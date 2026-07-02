CREATE TABLE IF NOT EXISTS rss_imports (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    last_run_at TEXT,
    last_result TEXT NOT NULL DEFAULT '',
    last_added INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_rss_imports_created_at ON rss_imports(created_at DESC);
