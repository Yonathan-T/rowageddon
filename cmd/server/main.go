package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"rowageddon/internal/db"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

var pool *pgxpool.Pool

type QueryResponse struct {
	DurationMs float64 `json:"duration_ms"`
	Count      int     `json:"count"`
	Data       any     `json:"data"`
}

func jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var tableExists bool
	_ = pool.QueryRow(r.Context(), `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'hn_items'
		);
	`).Scan(&tableExists)

	if !tableExists {
		jsonResponse(w, http.StatusOK, QueryResponse{
			DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
			Count:      0,
			Data: map[string]any{
				"table_exists": false,
				"total_rows":   0,
				"data_size":    "--",
				"total_size":   "--",
				"indexes":      []any{},
			},
		})
		return
	}

	if r.URL.Query().Get("analyze") == "true" {
		_, _ = pool.Exec(r.Context(), "ANALYZE hn_items;")
	}

	var totalRows int64
	var dataSize, totalSize string

	err := pool.QueryRow(r.Context(), `
		SELECT
			COALESCE(reltuples::bigint, 0),
			pg_size_pretty(pg_relation_size('hn_items')),
			pg_size_pretty(pg_total_relation_size('hn_items'))
		FROM pg_class
		WHERE relname = 'hn_items';
	`).Scan(&totalRows, &dataSize, &totalSize)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if totalRows <= 0 {
		var sampleCount int64
		_ = pool.QueryRow(r.Context(), "SELECT count(*) FROM (SELECT id FROM hn_items LIMIT 1000) s;").Scan(&sampleCount)
		if sampleCount > 0 {
			totalRows = sampleCount
		} else {
			totalRows = 0
		}
	}

	type IndexInfo struct {
		Name string `json:"name"`
		Size string `json:"size"`
	}

	rows, err := pool.Query(r.Context(), `
		SELECT indexrelname, pg_size_pretty(pg_relation_size(indexrelid))
		FROM pg_stat_user_indexes
		WHERE relname = 'hn_items';
	`)
	var indexes []IndexInfo
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var idx IndexInfo
			if err := rows.Scan(&idx.Name, &idx.Size); err == nil {
				indexes = append(indexes, idx)
			}
		}
	}

	elapsed := time.Since(start)

	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      1,
		Data: map[string]any{
			"table_exists": true,
			"total_rows":   totalRows,
			"data_size":    dataSize,
			"total_size":   totalSize,
			"indexes":      indexes,
		},
	})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var one int
	var pgVersion string
	err := pool.QueryRow(r.Context(), "SELECT 1, version();").Scan(&one, &pgVersion)
	elapsed := time.Since(start)

	if err != nil {
		jsonResponse(w, http.StatusServiceUnavailable, QueryResponse{
			DurationMs: float64(elapsed.Microseconds()) / 1000.0,
			Data: map[string]any{
				"status": "DOWN",
				"active": false,
				"error":  err.Error(),
			},
		})
		return
	}

	stat := pool.Stat()
	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Data: map[string]any{
			"status":       "HEALTHY",
			"active":       true,
			"ping_ms":      float64(elapsed.Microseconds()) / 1000.0,
			"version":      pgVersion,
			"total_conns":  stat.TotalConns(),
			"idle_conns":   stat.IdleConns(),
			"active_conns": stat.AcquiredConns(),
			"max_conns":    stat.MaxConns(),
		},
	})
}

func handleStories(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 20
	}
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days <= 0 {
		days = 30
	}

	query := `
		SELECT id, author, score, created_at
		FROM hn_items
		WHERE item_type = 'story' AND created_at > NOW() - ($1 * INTERVAL '1 day')
		ORDER BY score DESC
		LIMIT $2;
	`

	start := time.Now()
	rows, err := pool.Query(r.Context(), query, days, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Story struct {
		ID        int64     `json:"id"`
		Author    string    `json:"author"`
		Score     int32     `json:"score"`
		CreatedAt time.Time `json:"created_at"`
	}

	var stories []Story
	for rows.Next() {
		var s Story
		if err := rows.Scan(&s.ID, &s.Author, &s.Score, &s.CreatedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stories = append(stories, s)
	}
	elapsed := time.Since(start)

	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      len(stories),
		Data:       stories,
	})
}

func handleUser(w http.ResponseWriter, r *http.Request) {
	author := r.PathValue("author")
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 100
	}
	cursorStr := r.URL.Query().Get("cursor")

	var (
		query string
		args  []any
	)

	if cursorStr != "" {
		cursorTime, err := time.Parse(time.RFC3339Nano, cursorStr)
		if err != nil {
			cursorTime, err = time.Parse(time.RFC3339, cursorStr)
		}
		if err != nil {
			http.Error(w, "invalid cursor format (RFC3339 required)", http.StatusBadRequest)
			return
		}
		query = `
			SELECT id, item_type, score, parent_id, created_at, deleted
			FROM hn_items
			WHERE author = $1 AND created_at < $2
			ORDER BY created_at DESC
			LIMIT $3;
		`
		args = []any{author, cursorTime, limit}
	} else {
		query = `
			SELECT id, item_type, score, parent_id, created_at, deleted
			FROM hn_items
			WHERE author = $1
			ORDER BY created_at DESC
			LIMIT $2;
		`
		args = []any{author, limit}
	}

	start := time.Now()
	rows, err := pool.Query(r.Context(), query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type UserItem struct {
		ID        int64     `json:"id"`
		ItemType  string    `json:"item_type"`
		Score     int32     `json:"score"`
		ParentID  *int64    `json:"parent_id"`
		CreatedAt time.Time `json:"created_at"`
		Deleted   bool      `json:"deleted"`
	}

	var items []UserItem
	for rows.Next() {
		var it UserItem
		if err := rows.Scan(&it.ID, &it.ItemType, &it.Score, &it.ParentID, &it.CreatedAt, &it.Deleted); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}

	fetchElapsed := time.Since(start)

	var totalCount int64 = -1
	if r.URL.Query().Get("count") == "true" {
		_ = pool.QueryRow(r.Context(), "SELECT count(*) FROM hn_items WHERE author = $1;", author).Scan(&totalCount)
	}

	var nextCursor *string
	if len(items) == limit {
		lastTime := items[len(items)-1].CreatedAt.Format(time.RFC3339Nano)
		nextCursor = &lastTime
	}

	dataMap := map[string]any{
		"author":      author,
		"items":       items,
		"next_cursor": nextCursor,
		"has_more":    nextCursor != nil,
	}
	if totalCount >= 0 {
		dataMap["total_count"] = totalCount
	}

	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(fetchElapsed.Microseconds()) / 1000.0,
		Count:      len(items),
		Data:       dataMap,
	})
}

func handleComments(w http.ResponseWriter, r *http.Request) {
	parentID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid item id", http.StatusBadRequest)
		return
	}

	query := `
		SELECT id, author, score, created_at, deleted
		FROM hn_items
		WHERE parent_id = $1 AND parent_id IS NOT NULL
		ORDER BY created_at ASC
		LIMIT 100;
	`

	start := time.Now()
	rows, err := pool.Query(r.Context(), query, parentID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Comment struct {
		ID        int64     `json:"id"`
		Author    string    `json:"author"`
		Score     int32     `json:"score"`
		CreatedAt time.Time `json:"created_at"`
		Deleted   bool      `json:"deleted"`
	}

	var comments []Comment
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.Author, &c.Score, &c.CreatedAt, &c.Deleted); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		comments = append(comments, c)
	}
	elapsed := time.Since(start)

	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      len(comments),
		Data: map[string]any{
			"parent_id": parentID,
			"comments":  comments,
		},
	})
}

func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0.0
	}
	if n == 1 {
		return sorted[0]
	}
	if p <= 0.0 {
		return sorted[0]
	}
	if p >= 1.0 {
		return sorted[n-1]
	}

	rank := p * float64(n-1)
	lower := int(rank)
	upper := lower + 1
	if upper >= n {
		return sorted[n-1]
	}

	weight := rank - float64(lower)
	return sorted[lower]*(1.0-weight) + sorted[upper]*weight
}

func handleBenchmark(w http.ResponseWriter, r *http.Request) {
	runs, err := strconv.Atoi(r.URL.Query().Get("runs"))
	if err != nil || runs <= 0 || runs > 500 {
		runs = 100
	}

	queryType := r.URL.Query().Get("type")
	if queryType == "" {
		queryType = "stories"
	}

	daysOptions := []int{7, 14, 30, 60, 90}
	authors := []string{"drPeters", "fewJonathan", "chest766", "Janet_Lopez", "franticmouse"}
	parentIDs := []int64{100, 2500, 10000, 50000, 120000}

	durations := make([]float64, runs)
	ctx := r.Context()
	var baselineMap map[string]any

	switch queryType {
	case "user":
		query := `
			SELECT id, item_type, score, parent_id, created_at, deleted
			FROM hn_items
			WHERE author = $1
			ORDER BY created_at DESC
			LIMIT 20;
		`
		baselineMap = map[string]any{
			"p50_ms":         2450.0,
			"p90_ms":         4600.0,
			"p95_ms":         6200.0,
			"p99_ms":         9800.0,
			"p999_ms":        11500.0,
			"throughput_qps": 0.38,
		}
		for i := 0; i < runs; i++ {
			a := authors[rand.Intn(len(authors))]
			start := startPreciseTimer()
			r, err := pool.Query(ctx, query, a)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for r.Next() {
			}
			r.Close()
			durations[i] = elapsedPreciseMs(start)
		}

	case "comments":
		query := `
			SELECT id, author, score, created_at, deleted
			FROM hn_items
			WHERE parent_id = $1
			ORDER BY created_at ASC
			LIMIT 100;
		`
		baselineMap = map[string]any{
			"p50_ms":         1850.0,
			"p90_ms":         3900.0,
			"p95_ms":         5400.0,
			"p99_ms":         8500.0,
			"p999_ms":        10200.0,
			"throughput_qps": 0.45,
		}
		for i := 0; i < runs; i++ {
			pid := parentIDs[rand.Intn(len(parentIDs))]
			start := startPreciseTimer()
			r, err := pool.Query(ctx, query, pid)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for r.Next() {
			}
			r.Close()
			durations[i] = elapsedPreciseMs(start)
		}

	default: // "stories"
		query := `
			SELECT id, author, score, created_at
			FROM hn_items
			WHERE item_type = 'story' AND created_at > NOW() - ($1 * INTERVAL '1 day')
			ORDER BY score DESC
			LIMIT 10;
		`
		baselineMap = map[string]any{
			"p50_ms":         2113.4,
			"p90_ms":         4850.0,
			"p95_ms":         6850.0,
			"p99_ms":         10450.0,
			"p999_ms":        12200.0,
			"throughput_qps": 0.42,
		}
		for i := 0; i < runs; i++ {
			days := daysOptions[rand.Intn(len(daysOptions))]
			start := startPreciseTimer()
			rows, err := pool.Query(ctx, query, days)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			for rows.Next() {
			}
			rows.Close()
			durations[i] = elapsedPreciseMs(start)
		}
	}

	sorted := make([]float64, len(durations))
	copy(sorted, durations)
	sort.Float64s(sorted)

	sum := 0.0
	for _, d := range sorted {
		sum += d
	}

	totalBenchTime := sum / 1000.0
	qps := 0.0
	if totalBenchTime > 0 {
		qps = float64(runs) / totalBenchTime
	}

	p50 := percentile(sorted, 0.50)
	p90 := percentile(sorted, 0.90)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)
	p999 := percentile(sorted, 0.999)

	jsonResponse(w, http.StatusOK, map[string]any{
		"query_type":         queryType,
		"runs":               runs,
		"p50_ms":             p50,
		"p90_ms":             p90,
		"p95_ms":             p95,
		"p99_ms":             p99,
		"p999_ms":            p999,
		"min_ms":             sorted[0],
		"max_ms":             sorted[len(sorted)-1],
		"avg_ms":             sum / float64(runs),
		"throughput_qps":     qps,
		"samples":            durations,
		"baseline_unindexed": baselineMap,
	})
}

// GET /api/count - runs physical SELECT count(*) FROM hn_items
func handleExactCount(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var exactCount int64
	err := pool.QueryRow(r.Context(), "SELECT count(*) FROM hn_items;").Scan(&exactCount)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	elapsed := time.Since(start)
	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      1,
		Data: map[string]any{
			"exact_count": exactCount,
			"is_exact":    true,
		},
	})
}

// GET /api/raw - streams raw heap rows via PK B-Tree cursor
func handleRaw(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 200 {
		limit = 100
	}

	afterStr := r.URL.Query().Get("after")
	beforeStr := r.URL.Query().Get("before")
	fromStr := r.URL.Query().Get("from_id")

	var (
		query      string
		args       []any
		isBackward bool
	)

	if beforeStr != "" {
		beforeID, _ := strconv.ParseInt(beforeStr, 10, 64)
		if beforeID <= 1 {
			beforeID = int64(limit + 1)
		}
		query = `
			SELECT id, item_type, author, score, parent_id, created_at, deleted
			FROM (
				SELECT id, item_type, author, score, parent_id, created_at, deleted, deleted_at
				FROM hn_items
				WHERE id < $1
				ORDER BY id DESC
				LIMIT $2
			) sub
			ORDER BY id ASC;
		`
		args = []any{beforeID, limit}
		isBackward = true
	} else if afterStr != "" {
		afterID, _ := strconv.ParseInt(afterStr, 10, 64)
		query = `
			SELECT id, item_type, author, score, parent_id, created_at, deleted, deleted_at
			FROM hn_items
			WHERE id > $1
			ORDER BY id ASC
			LIMIT $2;
		`
		args = []any{afterID, limit}
	} else if fromStr != "" {
		fromID, _ := strconv.ParseInt(fromStr, 10, 64)
		if fromID < 1 {
			fromID = 1
		}
		query = `
			SELECT id, item_type, author, score, parent_id, created_at, deleted, deleted_at
			FROM hn_items
			WHERE id >= $1
			ORDER BY id ASC
			LIMIT $2;
		`
		args = []any{fromID, limit}
	} else {
		query = `
			SELECT id, item_type, author, score, parent_id, created_at, deleted, deleted_at
			FROM hn_items
			WHERE id >= 1
			ORDER BY id ASC
			LIMIT $1;
		`
		args = []any{limit}
	}

	start := time.Now()
	rows, err := pool.Query(r.Context(), query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type RawItem struct {
		ID        int64      `json:"id"`
		ItemType  string     `json:"item_type"`
		Author    string     `json:"author"`
		Score     int32      `json:"score"`
		ParentID  *int64     `json:"parent_id"`
		CreatedAt time.Time  `json:"created_at"`
		Deleted   bool       `json:"deleted"`
		DeletedAt *time.Time `json:"deleted_at"`
	}

	var items []RawItem
	for rows.Next() {
		var it RawItem
		if err := rows.Scan(&it.ID, &it.ItemType, &it.Author, &it.Score, &it.ParentID, &it.CreatedAt, &it.Deleted, &it.DeletedAt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, it)
	}
	elapsed := time.Since(start)

	var firstID, lastID int64
	if len(items) > 0 {
		firstID = items[0].ID
		lastID = items[len(items)-1].ID
	}

	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      len(items),
		Data: map[string]any{
			"items":       items,
			"first_id":    firstID,
			"last_id":     lastID,
			"has_prev":    firstID > 1,
			"has_next":    len(items) == limit,
			"is_backward": isBackward,
		},
	})
}

func handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	start := time.Now()
	tag, err := pool.Exec(r.Context(), `
		UPDATE hn_items
		SET deleted = TRUE, deleted_at = NOW()
		WHERE id = $1;
	`, id)
	elapsed := time.Since(start)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      int(tag.RowsAffected()),
		Data: map[string]any{
			"id":            id,
			"deleted":       true,
			"rows_affected": tag.RowsAffected(),
		},
	})
}

func handleRestoreItem(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	start := time.Now()
	tag, err := pool.Exec(r.Context(), `
		UPDATE hn_items
		SET deleted = FALSE, deleted_at = NULL
		WHERE id = $1;
	`, id)
	elapsed := time.Since(start)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      int(tag.RowsAffected()),
		Data: map[string]any{
			"id":            id,
			"deleted":       false,
			"rows_affected": tag.RowsAffected(),
		},
	})
}

func handleJanitorPurge(w http.ResponseWriter, r *http.Request) {
	limit := 5000
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 50000 {
			limit = parsed
		}
	}
	start := time.Now()
	tag, err := pool.Exec(r.Context(), `
		WITH to_delete AS (
			SELECT id FROM hn_items
			WHERE deleted = TRUE
			LIMIT $1
			FOR UPDATE SKIP LOCKED
		)
		DELETE FROM hn_items
		WHERE id IN (SELECT id FROM to_delete);
	`, limit)
	elapsed := time.Since(start)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, http.StatusOK, QueryResponse{
		DurationMs: float64(elapsed.Microseconds()) / 1000.0,
		Count:      int(tag.RowsAffected()),
		Data: map[string]any{
			"purged":        tag.RowsAffected(),
			"limit_applied": limit,
		},
	})
}

func main() {
	_ = godotenv.Load()
	host := os.Getenv("POSTGRES_HOST")
	port := os.Getenv("POSTGRES_PORT")
	pass := os.Getenv("POSTGRES_PASSWORD")
	dbname := os.Getenv("POSTGRES_DB")
	connString := fmt.Sprintf("host=%s port=%s user=postgres password='%s' dbname=%s sslmode=disable", host, port, pass, dbname)

	var err error
	pool, err = db.Connect(context.Background(), connString, db.Config{MaxConns: 50})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/raw", handleRaw)
	mux.HandleFunc("GET /api/health", handleHealth)
	mux.HandleFunc("GET /api/stats", handleStats)
	mux.HandleFunc("GET /api/count", handleExactCount)
	mux.HandleFunc("GET /api/stories", handleStories)
	mux.HandleFunc("GET /api/user/{author}", handleUser)
	mux.HandleFunc("GET /api/item/{id}/comments", handleComments)
	mux.HandleFunc("GET /api/benchmark", handleBenchmark)

	mux.HandleFunc("POST /api/item/{id}/delete", handleDeleteItem)
	mux.HandleFunc("POST /api/item/{id}/restore", handleRestoreItem)
	mux.HandleFunc("POST /api/janitor/purge", handleJanitorPurge)

	fileServer := http.FileServer(http.Dir("web"))
	mux.Handle("GET /", fileServer)

	fmt.Printf("Server running on http://localhost:8080\n")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
	}
}
