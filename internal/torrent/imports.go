package torrent

import (
	"context"
	"time"

	"github.com/debridnest/debridnest/internal/storage"
)

func (m *Manager) CreateRSSImport(ctx context.Context, rec storage.RSSImportRecord) error {
	return m.db.CreateRSSImport(ctx, rec)
}

func (m *Manager) ListRSSImports(ctx context.Context) ([]storage.RSSImportRecord, error) {
	return m.db.ListRSSImports(ctx)
}

func (m *Manager) GetRSSImport(ctx context.Context, id string) (storage.RSSImportRecord, error) {
	return m.db.GetRSSImport(ctx, id)
}

func (m *Manager) DeleteRSSImport(ctx context.Context, id string) error {
	return m.db.DeleteRSSImport(ctx, id)
}

func (m *Manager) UpdateRSSImportRunResult(ctx context.Context, id string, ranAt time.Time, result string, added int, lastErr string) error {
	return m.db.UpdateRSSImportRunResult(ctx, id, ranAt, result, added, lastErr)
}
