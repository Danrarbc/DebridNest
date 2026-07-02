ALTER TABLE torrents ADD COLUMN library_pinned INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN library_favorite INTEGER NOT NULL DEFAULT 0;
ALTER TABLE torrents ADD COLUMN retention_exempt INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_torrents_library_completed
ON torrents(status, retention_exempt, ended_at DESC);
