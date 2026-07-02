package storage

import (
	"context"
	"database/sql"
	"time"
)

type ShareLinkRecord struct {
	ID             string     `json:"id"`
	TokenHash      string     `json:"-"`
	OwnerID        string     `json:"ownerId"`
	OwnerName      string     `json:"ownerName"`
	TorrentID      string     `json:"torrentId"`
	FileID         int        `json:"fileId"`
	Filename       string     `json:"filename"`
	MimeType       string     `json:"mimeType"`
	Filesize       int64      `json:"filesize"`
	ExpiresAt      time.Time  `json:"expiresAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	LastAccessedAt *time.Time `json:"lastAccessedAt,omitempty"`
	RevokedAt      *time.Time `json:"revokedAt,omitempty"`
}

func (db *DB) CreateShareLink(ctx context.Context, rec ShareLinkRecord) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO share_links (
			id, token_hash, owner_id, owner_name, torrent_id, file_id, filename, mime_type,
			filesize, expires_at, created_at, last_accessed_at, revoked_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		rec.ID, rec.TokenHash, rec.OwnerID, rec.OwnerName, rec.TorrentID, rec.FileID,
		rec.Filename, rec.MimeType, rec.Filesize, rec.ExpiresAt.UTC().Format(time.RFC3339Nano),
		rec.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (db *DB) GetShareLinkByTokenHash(ctx context.Context, tokenHash string) (ShareLinkRecord, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, token_hash, owner_id, owner_name, torrent_id, file_id, filename, mime_type,
			filesize, expires_at, created_at, last_accessed_at, revoked_at
		FROM share_links
		WHERE token_hash = ?`, tokenHash)
	return scanShareLink(row.Scan)
}

func (db *DB) GetShareLinkByID(ctx context.Context, id string) (ShareLinkRecord, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, token_hash, owner_id, owner_name, torrent_id, file_id, filename, mime_type,
			filesize, expires_at, created_at, last_accessed_at, revoked_at
		FROM share_links
		WHERE id = ?`, id)
	return scanShareLink(row.Scan)
}

func (db *DB) ListShareLinks(ctx context.Context, ownerID string, includeAll bool, limit int) ([]ShareLinkRecord, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, token_hash, owner_id, owner_name, torrent_id, file_id, filename, mime_type,
			filesize, expires_at, created_at, last_accessed_at, revoked_at
		FROM share_links`
	var args []any
	if !includeAll {
		query += ` WHERE owner_id = ?`
		args = append(args, ownerID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ShareLinkRecord
	for rows.Next() {
		rec, err := scanShareLink(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (db *DB) MarkShareLinkAccessed(ctx context.Context, id string, at time.Time) error {
	_, err := db.ExecContext(ctx, `
		UPDATE share_links SET last_accessed_at = ? WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), id,
	)
	return err
}

func (db *DB) RevokeShareLink(ctx context.Context, id string, at time.Time) error {
	res, err := db.ExecContext(ctx, `
		UPDATE share_links
		SET revoked_at = COALESCE(revoked_at, ?)
		WHERE id = ?`,
		at.UTC().Format(time.RFC3339Nano), id,
	)
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type shareScanFn func(dest ...any) error

func scanShareLink(scan shareScanFn) (ShareLinkRecord, error) {
	var rec ShareLinkRecord
	var expiresAt, createdAt string
	var lastAccessed, revoked sql.NullString
	if err := scan(
		&rec.ID, &rec.TokenHash, &rec.OwnerID, &rec.OwnerName, &rec.TorrentID, &rec.FileID,
		&rec.Filename, &rec.MimeType, &rec.Filesize, &expiresAt, &createdAt, &lastAccessed, &revoked,
	); err != nil {
		return ShareLinkRecord{}, err
	}
	rec.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if lastAccessed.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastAccessed.String)
		rec.LastAccessedAt = &t
	}
	if revoked.Valid {
		t, _ := time.Parse(time.RFC3339Nano, revoked.String)
		rec.RevokedAt = &t
	}
	return rec, nil
}
