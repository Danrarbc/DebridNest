package torrent

import (
	"context"
	"fmt"
	"os"

	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/storage"
)

func (m *Manager) maybeOffloadCompletedFiles(ctx context.Context, rec *storage.TorrentRecord) {
	store, err := m.objectStoreForSettings()
	if err != nil || store == nil || !store.Enabled() || !store.EarlyOffload() {
		return
	}
	m.offloadFiles(ctx, store, rec, false)
}

func (m *Manager) offloadTorrent(ctx context.Context, torrentID string) {
	store, err := m.objectStoreForSettings()
	if err != nil || store == nil || !store.Enabled() {
		return
	}

	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil || !IsCompletedStatus(rec.Status) {
		return
	}

	m.offloadFiles(ctx, store, rec, store.OffloadLocal())
}

func (m *Manager) offloadFiles(ctx context.Context, store *objectstore.Store, rec *storage.TorrentRecord, removeLocal bool) {
	var updated []storage.TorrentFileRecord
	var used int64
	quota := m.objectStorageQuotaBytes()
	if quota > 0 {
		m.objectQuotaMu.Lock()
		defer m.objectQuotaMu.Unlock()
		usage, err := m.db.ObjectStorageUsage(ctx)
		if err != nil {
			return
		}
		used = usage.Bytes
	}
	for i := range rec.Files {
		f := rec.Files[i]
		if f.RemoteStored {
			if removeLocal && !f.KeepLocal {
				removeLocalOffloadedFile(f)
			}
			continue
		}
		if !offloadCandidate(f) {
			continue
		}
		if quota > 0 && used+f.Bytes > quota {
			continue
		}
		if changed, out := m.offloadOneFile(ctx, store, rec, f); changed {
			nextFiles := replaceTorrentFile(rec.Files, out)
			if err := m.db.UpdateTorrentFiles(ctx, rec.ID, nextFiles); err != nil {
				_ = store.Delete(ctx, out.ObjectKey)
				continue
			}
			rec.Files = nextFiles
			if removeLocal && !out.KeepLocal {
				removeLocalOffloadedFile(out)
			}
			updated = append(updated, out)
			used += f.Bytes
		}
	}
	if len(updated) == 0 {
		return
	}

	m.invalidateDiskUsed()
}

func replaceTorrentFile(files []storage.TorrentFileRecord, updated storage.TorrentFileRecord) []storage.TorrentFileRecord {
	out := append([]storage.TorrentFileRecord(nil), files...)
	for i := range out {
		if out[i].ID == updated.ID {
			out[i] = updated
			return out
		}
	}
	return out
}

func offloadCandidate(f storage.TorrentFileRecord) bool {
	return f.Selected && !f.RemoteStored && f.Bytes > 0 && f.DownloadedBytes >= f.Bytes
}

func (m *Manager) offloadOneFile(ctx context.Context, store *objectstore.Store, rec *storage.TorrentRecord, f storage.TorrentFileRecord) (bool, storage.TorrentFileRecord) {
	changed, out, err := m.offloadOneFileStrict(ctx, store, rec, f)
	if err != nil {
		return false, f
	}
	return changed, out
}

func (m *Manager) offloadOneFileStrict(ctx context.Context, store *objectstore.Store, rec *storage.TorrentRecord, f storage.TorrentFileRecord) (bool, storage.TorrentFileRecord, error) {
	if !offloadCandidate(f) {
		return false, f, nil
	}
	if store == nil || !store.Enabled() {
		return false, f, fmt.Errorf("object storage disabled")
	}
	if f.DiskPath == "" {
		return false, f, fmt.Errorf("local file unavailable")
	}
	if _, err := os.Stat(f.DiskPath); err != nil {
		return false, f, err
	}

	key := store.ObjectKey(rec.InfoHash, f.Path)
	if err := store.Upload(ctx, f.DiskPath, key); err != nil {
		return false, f, err
	}

	f.ObjectKey = key
	f.RemoteStored = true
	return true, f, nil
}

func removeLocalOffloadedFile(f storage.TorrentFileRecord) {
	if f.DiskPath == "" {
		return
	}
	_ = os.Remove(f.DiskPath)
}
