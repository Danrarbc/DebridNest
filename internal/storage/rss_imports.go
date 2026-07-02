package storage

import (
	"context"
	"database/sql"
	"time"
)

type RSSImportRecord struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastRunAt  *time.Time `json:"lastRunAt,omitempty"`
	LastResult string     `json:"lastResult"`
	LastAdded  int        `json:"lastAdded"`
	LastError  string     `json:"lastError"`
}

func (db *DB) CreateRSSImport(ctx context.Context, rec RSSImportRecord) error {
	enabled := 0
	if rec.Enabled {
		enabled = 1
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO rss_imports (id, name, url, enabled, created_at, last_result, last_added, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.Name, rec.URL, enabled, rec.CreatedAt.UTC().Format(time.RFC3339Nano), rec.LastResult, rec.LastAdded, rec.LastError,
	)
	return err
}

func (db *DB) ListRSSImports(ctx context.Context) ([]RSSImportRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, url, enabled, created_at, last_run_at, last_result, last_added, last_error
		FROM rss_imports
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RSSImportRecord
	for rows.Next() {
		rec, err := scanRSSImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (db *DB) GetRSSImport(ctx context.Context, id string) (RSSImportRecord, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, name, url, enabled, created_at, last_run_at, last_result, last_added, last_error
		FROM rss_imports
		WHERE id = ?`, id)
	return scanRSSImport(row)
}

func (db *DB) DeleteRSSImport(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM rss_imports WHERE id = ?`, id)
	return err
}

func (db *DB) UpdateRSSImportRunResult(ctx context.Context, id string, ranAt time.Time, result string, added int, lastErr string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE rss_imports
		SET last_run_at = ?, last_result = ?, last_added = ?, last_error = ?
		WHERE id = ?`,
		ranAt.UTC().Format(time.RFC3339Nano), result, added, lastErr, id,
	)
	return err
}

func scanRSSImport(row interface {
	Scan(dest ...any) error
}) (RSSImportRecord, error) {
	var rec RSSImportRecord
	var enabled int
	var created string
	var lastRun sql.NullString
	if err := row.Scan(&rec.ID, &rec.Name, &rec.URL, &enabled, &created, &lastRun, &rec.LastResult, &rec.LastAdded, &rec.LastError); err != nil {
		return RSSImportRecord{}, err
	}
	rec.Enabled = enabled == 1
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if lastRun.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastRun.String)
		rec.LastRunAt = &t
	}
	return rec, nil
}
