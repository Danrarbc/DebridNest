ALTER TABLE users ADD COLUMN quota_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN rate_limit_mbps REAL NOT NULL DEFAULT 0;

ALTER TABLE torrents ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE torrents ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';

ALTER TABLE downloads ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN owner_name TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_torrents_owner_id ON torrents(owner_id);
CREATE INDEX IF NOT EXISTS idx_downloads_owner_id ON downloads(owner_id);

CREATE TABLE IF NOT EXISTS share_links (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL UNIQUE,
    owner_id TEXT NOT NULL,
    owner_name TEXT NOT NULL,
    torrent_id TEXT NOT NULL,
    file_id INTEGER NOT NULL,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    filesize INTEGER NOT NULL DEFAULT 0,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    last_accessed_at TEXT,
    revoked_at TEXT,
    FOREIGN KEY (torrent_id) REFERENCES torrents(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_share_links_owner_id ON share_links(owner_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_share_links_expires_at ON share_links(expires_at);
