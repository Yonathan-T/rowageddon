CREATE TABLE hn_items (
    id BIGSERIAL PRIMARY KEY,
    parent_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL,
    score INT NOT NULL DEFAULT 0,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at TIMESTAMPTZ DEFAULT NULL,
    item_type VARCHAR(16) NOT NULL,
    author VARCHAR(32) NOT NULL
);

-- Note: Ingest your raw data first before creating indexes!
-- Read the writeup (web/blog.html) for why building indexes early ruins ingestion speed.

-- CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hn_author ON hn_items (author, created_at DESC);
-- CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hn_comments_parent ON hn_items (parent_id, created_at) WHERE parent_id IS NOT NULL;
-- CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_stories_top_scoring ON hn_items (score DESC, created_at DESC) WHERE item_type = 'story';
-- CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hn_active_stories ON hn_items (score DESC, created_at DESC) WHERE item_type = 'story' AND deleted_at IS NULL;
