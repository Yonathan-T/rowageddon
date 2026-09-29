package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/brianvoe/gofakeit/v7"
)

type ItemGenerator struct {
	totalRows    int64
	rowsInserted int64
	currentRowId int64
	step         int64
	users        []string
	now          time.Time
	rowValues    []any
}

func NewItemGenerator(totalRows int64, userPoolSize int, step int64, startID int64) *ItemGenerator {
	users := make([]string, userPoolSize)
	for i := range userPoolSize {
		users[i] = gofakeit.Username()
	}

	if step <= 0 {
		step = 1_000_000
		if totalRows < 1_000_000 {
			step = totalRows / 10
			if step == 0 {
				step = 1000
			}
		}
	}

	return &ItemGenerator{
		totalRows:    totalRows,
		rowsInserted: 0,
		currentRowId: startID,
		step:         step,
		users:        users,
		now:          time.Now(),
	}
}

func (g *ItemGenerator) Next() bool {
	if g.rowsInserted >= g.totalRows {
		return false
	}
	g.rowsInserted++
	g.currentRowId++

	id := g.currentRowId

	isStory := rand.Float32() < 0.15
	itemType := "comment"
	var parentID *int64 = nil

	if isStory {
		itemType = "story"
	} else if g.currentRowId > 1 {
		p := rand.Int63n(g.currentRowId-1) + 1
		parentID = &p
	}

	userIndex := int(float64(len(g.users)) * (1.0 - (rand.Float64() * rand.Float64())))
	if userIndex >= len(g.users) {
		userIndex = len(g.users) - 1
	}
	author := g.users[userIndex]

	randomMinutesAgo := rand.Intn(365 * 2 * 24 * 60)
	createdAt := g.now.Add(-time.Duration(randomMinutesAgo) * time.Minute)

	score := int32(0)
	if isStory {
		score = int32(rand.Intn(500))
	} else if rand.Float32() < 0.3 {
		score = int32(rand.Intn(10))
	}

	deleted := rand.Float32() < 0.03
	var deletedAt *time.Time = nil
	if deleted {
		// Random realistic deletion time between createdAt and now
		delta := g.now.Sub(createdAt)
		if delta > 0 {
			delTime := createdAt.Add(time.Duration(rand.Int63n(int64(delta))))
			deletedAt = &delTime
		} else {
			deletedAt = &createdAt
		}
	}

	g.rowValues = []any{
		id,
		parentID,
		createdAt,
		score,
		deleted,
		itemType,
		author,
		deletedAt,
	}

	if g.step > 0 && g.currentRowId%g.step == 0 {
		fmt.Printf("Inserted %s rows\n", formatCommas(g.currentRowId))
	}

	return true
}

func (g *ItemGenerator) Values() ([]any, error) {
	return g.rowValues, nil
}

func (g *ItemGenerator) Err() error {
	return nil
}

func formatCommas(n int64) string {
	in := strconv.FormatInt(n, 10)
	numOfDigits := len(in)
	if numOfDigits <= 3 {
		return in
	}
	var out []byte
	rem := numOfDigits % 3
	if rem > 0 {
		out = append(out, in[:rem]...)
		if numOfDigits > rem {
			out = append(out, ',')
		}
	}
	for i := rem; i < numOfDigits; i += 3 {
		out = append(out, in[i:i+3]...)
		if i+3 < numOfDigits {
			out = append(out, ',')
		}
	}
	return string(out)
}
