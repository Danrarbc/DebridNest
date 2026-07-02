package storage

import (
	"context"
	"database/sql"
)

type UserLimits struct {
	QuotaBytes    int64
	RateLimitMbps float64
}

func (db *DB) GetUserLimits(ctx context.Context, id string) (UserLimits, error) {
	var limits UserLimits
	err := db.QueryRowContext(ctx, `
		SELECT quota_bytes, rate_limit_mbps
		FROM users
		WHERE id = ? AND disabled = 0`, id).Scan(&limits.QuotaBytes, &limits.RateLimitMbps)
	if err != nil {
		return UserLimits{}, err
	}
	return limits, nil
}

func (db *DB) UserSelectedBytes(ctx context.Context, ownerID, excludeTorrentID string) (int64, error) {
	if ownerID == "" {
		return 0, nil
	}
	var used sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT SUM(tf.bytes)
		FROM torrents t
		JOIN torrent_files tf ON tf.torrent_id = t.id
		WHERE t.owner_id = ? AND t.id <> ? AND tf.selected = 1`,
		ownerID, excludeTorrentID,
	).Scan(&used)
	if err != nil {
		return 0, err
	}
	if !used.Valid {
		return 0, nil
	}
	return used.Int64, nil
}
