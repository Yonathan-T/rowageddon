package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"rowageddon/internal/db"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
)

func main() {
	countFlag := flag.Int64("count", 10000, "number of rows to generate")
	stepFlag := flag.Int64("step", 2_000_000, "print progress every N rows")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "No .env file found: %v\n", err)
	}

	ctx := context.Background()

	host := os.Getenv("POSTGRES_HOST")
	port := os.Getenv("POSTGRES_PORT")
	pass := os.Getenv("POSTGRES_PASSWORD")
	dbname := os.Getenv("POSTGRES_DB")
	connString := fmt.Sprintf("host=%s port=%s user=postgres password='%s' dbname=%s sslmode=disable", host, port, pass, dbname)

	pool, err := db.Connect(ctx, connString, db.Config{MaxConns: 10})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	var startID int64
	err = pool.QueryRow(ctx, "SELECT COALESCE(MAX(id), 0) FROM hn_items").Scan(&startID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get MAX(id): %v\n", err)
		os.Exit(1)
	}
	if startID > 0 {
		fmt.Printf("Resuming from existing ID: %s\n", formatCommas(startID))
	}
	if startID == 0 {
		fmt.Println("Schema created.")
	}
	fmt.Println("Starting inserts...")

	generator := NewItemGenerator(*countFlag, 5000, *stepFlag, startID)
	columns := []string{"id", "parent_id", "created_at", "score", "deleted", "item_type", "author", "deleted_at"}

	startTime := time.Now()

	rowsInserted, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"hn_items"},
		columns,
		generator,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CopyFrom failed: %v\n", err)
		os.Exit(1)
	}

	elapsed := time.Since(startTime)
	mins := int(elapsed.Minutes())
	secs := int(elapsed.Seconds()) % 60
	millis := elapsed.Milliseconds() % 1000
	rate := float64(rowsInserted) / elapsed.Seconds()

	fmt.Printf("Insert Time: %d:%02d.%03d (m:ss.mmm) [%.0f rows/sec]\n", mins, secs, millis, rate)
	fmt.Println("All done!")

	var totalInDB int64
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM hn_items").Scan(&totalInDB)
	fmt.Printf("Total rows in database: %s\n", formatCommas(totalInDB))
}
