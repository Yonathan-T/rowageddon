package models

import "time"

type HNItem struct {
	ID        int64      `db:"id"`
	ParentID  *int64     `db:"parent_id"`
	CreatedAt time.Time  `db:"created_at"`
	Score     int32      `db:"score"`
	Deleted   bool       `db:"deleted"`
	DeletedAt *time.Time `db:"deleted_at"`
	ItemType  string     `db:"item_type"`
	Author    string     `db:"author"`
}
