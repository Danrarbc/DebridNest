package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

type storageQuotaPolicy struct {
	Enabled       bool  `json:"enabled"`
	QuotaBytes    int64 `json:"quotaBytes"`
	QuotaGb       int64 `json:"quotaGb"`
	UsedBytes     int64 `json:"usedBytes"`
	ReservedBytes int64 `json:"reservedBytes"`
	ObjectCount   int   `json:"objectCount"`
	OffloadLocal  bool  `json:"offloadLocal"`
	EarlyOffload  bool  `json:"earlyOffload"`
}

type storageObjectEntry struct {
	Key          string     `json:"key"`
	Name         string     `json:"name"`
	Directory    bool       `json:"directory"`
	Bytes        int64      `json:"bytes"`
	LastModified *time.Time `json:"lastModified,omitempty"`
	TorrentID    string     `json:"torrentId,omitempty"`
	TorrentName  string     `json:"torrentName,omitempty"`
	FileID       int        `json:"fileId,omitempty"`
	FilePath     string     `json:"filePath,omitempty"`
	RemoteStored bool       `json:"remoteStored,omitempty"`
	KeepLocal    bool       `json:"keepLocal,omitempty"`
	LocalStored  bool       `json:"localStored,omitempty"`
}

type storageRecordEntry struct {
	TorrentID      string     `json:"torrentId"`
	TorrentName    string     `json:"torrentName"`
	InfoHash       string     `json:"hash"`
	Status         string     `json:"status"`
	FileID         int        `json:"fileId"`
	Path           string     `json:"path"`
	Bytes          int64      `json:"bytes"`
	ObjectKey      string     `json:"objectKey"`
	RemoteStored   bool       `json:"remoteStored"`
	KeepLocal      bool       `json:"keepLocal"`
	LocalStored    bool       `json:"localStored"`
	ObjectBytes    int64      `json:"objectBytes,omitempty"`
	ObjectModified *time.Time `json:"objectModified,omitempty"`
	ReconcileError string     `json:"error,omitempty"`
}

type storageReconcileResponse struct {
	Enabled        bool                 `json:"enabled"`
	Bucket         string               `json:"bucket"`
	Prefix         string               `json:"prefix"`
	CheckedAt      time.Time            `json:"checkedAt"`
	DBRemoteCount  int                  `json:"dbRemoteCount"`
	ObjectCount    int                  `json:"objectCount"`
	MatchedCount   int                  `json:"matchedCount"`
	Truncated      bool                 `json:"truncated"`
	MissingRemote  []storageRecordEntry `json:"missingRemote"`
	SizeMismatches []storageRecordEntry `json:"sizeMismatches"`
	OrphanedRemote []storageObjectEntry `json:"orphanedRemote"`
	UnmarkedRemote []storageRecordEntry `json:"unmarkedRemote"`
	Policy         storageQuotaPolicy   `json:"policy"`
}

func (h *Handler) listStorageObjects(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager not configured")
		return
	}

	cfg := h.storageConfig()
	policy, err := h.storagePolicy(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !cfg.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": false,
			"bucket":  cfg.Bucket,
			"prefix":  cleanObjectPrefix(r.URL.Query().Get("prefix")),
			"entries": []storageObjectEntry{},
			"policy":  policy,
		})
		return
	}
	if cfg.Bucket == "" {
		writeError(w, http.StatusBadRequest, "S3 bucket is required")
		return
	}

	store, err := objectstore.New(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := int32(boundedQueryInt(r, "limit", 100, 1, 1000))
	prefix := requestedObjectPrefix(cfg, r.URL.Query().Get("prefix"))
	result, err := store.List(r.Context(), prefix, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	records, err := h.manager.ObjectStorageRecords(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	byKey := recordsByObjectKey(store, records, true)

	entries := make([]storageObjectEntry, 0, len(result.Objects))
	for _, obj := range result.Objects {
		entry := storageObjectEntry{
			Key:       obj.Key,
			Name:      objectName(result.Prefix, obj.Key),
			Directory: obj.Directory,
			Bytes:     obj.Size,
		}
		if !obj.LastModified.IsZero() {
			mod := obj.LastModified
			entry.LastModified = &mod
		}
		if !obj.Directory {
			if rec, ok := byKey[obj.Key]; ok {
				annotateStorageObject(&entry, rec)
			}
		}
		entries = append(entries, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    true,
		"bucket":     cfg.Bucket,
		"prefix":     result.Prefix,
		"basePrefix": cfg.Prefix,
		"truncated":  result.Truncated,
		"nextToken":  result.NextToken,
		"entries":    entries,
		"policy":     policy,
	})
}

func (h *Handler) reconcileStorage(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager not configured")
		return
	}

	cfg := h.storageConfig()
	policy, err := h.storagePolicy(r.Context(), cfg)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := storageReconcileResponse{
		Enabled:   cfg.Enabled,
		Bucket:    cfg.Bucket,
		Prefix:    requestedObjectPrefix(cfg, r.URL.Query().Get("prefix")),
		CheckedAt: time.Now().UTC(),
		Policy:    policy,
	}
	if !cfg.Enabled {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if cfg.Bucket == "" {
		writeError(w, http.StatusBadRequest, "S3 bucket is required")
		return
	}

	store, err := objectstore.New(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	records, err := h.manager.ObjectStorageRecords(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	remoteByKey := recordsByObjectKey(store, records, true)
	expectedByKey := recordsByObjectKey(store, records, false)

	for _, rec := range remoteByKey {
		resp.DBRemoteCount++
		key := recordObjectKey(store, rec)
		size, modified, err := store.Head(r.Context(), key)
		entry := storageRecordPayload(rec, key)
		if err != nil {
			entry.ReconcileError = err.Error()
			resp.MissingRemote = append(resp.MissingRemote, entry)
			continue
		}
		entry.ObjectBytes = size
		if !modified.IsZero() {
			entry.ObjectModified = &modified
		}
		if size != rec.Bytes {
			resp.SizeMismatches = append(resp.SizeMismatches, entry)
			continue
		}
		resp.MatchedCount++
	}

	maxKeys := boundedQueryInt(r, "maxKeys", 10000, 1, 50000)
	objects, err := store.ListAll(r.Context(), resp.Prefix, maxKeys)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	resp.ObjectCount = len(objects.Objects)
	resp.Truncated = objects.Truncated
	for _, obj := range objects.Objects {
		if _, ok := remoteByKey[obj.Key]; ok {
			continue
		}
		if rec, ok := expectedByKey[obj.Key]; ok {
			entry := storageRecordPayload(rec, obj.Key)
			entry.ObjectBytes = obj.Size
			if !obj.LastModified.IsZero() {
				mod := obj.LastModified
				entry.ObjectModified = &mod
			}
			resp.UnmarkedRemote = append(resp.UnmarkedRemote, entry)
			continue
		}
		entry := storageObjectEntry{
			Key:   obj.Key,
			Name:  objectName(resp.Prefix, obj.Key),
			Bytes: obj.Size,
		}
		if !obj.LastModified.IsZero() {
			mod := obj.LastModified
			entry.LastModified = &mod
		}
		resp.OrphanedRemote = append(resp.OrphanedRemote, entry)
	}

	h.LogActivity(r.Context(), ActionStorageReconcile, map[string]any{
		"missingRemote":  len(resp.MissingRemote),
		"sizeMismatches": len(resp.SizeMismatches),
		"orphanedRemote": len(resp.OrphanedRemote),
		"unmarkedRemote": len(resp.UnmarkedRemote),
	})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) patchTorrentFileStorage(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager not configured")
		return
	}

	fileID, err := strconv.Atoi(chi.URLParam(r, "fileID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}

	var body struct {
		KeepLocal   *bool `json:"keepLocal"`
		OffloadOnly bool  `json:"offloadOnly"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.KeepLocal == nil && !body.OffloadOnly {
		writeError(w, http.StatusBadRequest, "keepLocal or offloadOnly required")
		return
	}

	torrentID := chi.URLParam(r, "id")
	file, err := h.manager.SetFileStoragePolicy(r.Context(), torrentID, fileID, body.KeepLocal, body.OffloadOnly)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, torrentmgr.ErrTorrentFileNotFound) || err.Error() == "unknown torrent" {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionStoragePolicy, map[string]any{
		"torrentId":   torrentID,
		"fileId":      fileID,
		"keepLocal":   file.KeepLocal,
		"offloadOnly": body.OffloadOnly,
	})
	writeJSON(w, http.StatusOK, torrentFilePayload(file))
}

func (h *Handler) storageConfig() objectstore.Config {
	if h.settings != nil {
		return h.settings.S3Config()
	}
	return objectstore.LoadFromEnv()
}

func (h *Handler) storagePolicy(ctx context.Context, cfg objectstore.Config) (storageQuotaPolicy, error) {
	policy := storageQuotaPolicy{
		Enabled:      cfg.Enabled,
		QuotaGb:      cfg.QuotaGB,
		OffloadLocal: cfg.OffloadLocal,
		EarlyOffload: cfg.EarlyOffload,
	}
	if h.manager == nil {
		return policy, nil
	}
	policy.QuotaBytes = h.manager.ObjectStorageQuotaBytes()
	usage, err := h.manager.ObjectStorageUsage(ctx)
	if err != nil {
		return policy, err
	}
	reserved, err := h.manager.ObjectStorageReservedUsage(ctx)
	if err != nil {
		return policy, err
	}
	policy.UsedBytes = usage.Bytes
	policy.ObjectCount = usage.Count
	policy.ReservedBytes = reserved.Bytes
	return policy, nil
}

func recordsByObjectKey(store *objectstore.Store, records []storage.ObjectStorageRecord, remoteOnly bool) map[string]storage.ObjectStorageRecord {
	out := make(map[string]storage.ObjectStorageRecord)
	for _, rec := range records {
		if remoteOnly && !rec.RemoteStored {
			continue
		}
		if !remoteOnly && !rec.Selected && !rec.RemoteStored {
			continue
		}
		key := recordObjectKey(store, rec)
		if key == "" {
			continue
		}
		out[key] = rec
	}
	return out
}

func recordObjectKey(store *objectstore.Store, rec storage.ObjectStorageRecord) string {
	if rec.ObjectKey != "" {
		return rec.ObjectKey
	}
	if store == nil || rec.InfoHash == "" || rec.Path == "" {
		return ""
	}
	return store.ObjectKey(rec.InfoHash, rec.Path)
}

func storageRecordPayload(rec storage.ObjectStorageRecord, key string) storageRecordEntry {
	return storageRecordEntry{
		TorrentID:      rec.TorrentID,
		TorrentName:    rec.TorrentName,
		InfoHash:       rec.InfoHash,
		Status:         rec.Status,
		FileID:         rec.FileID,
		Path:           rec.Path,
		Bytes:          rec.Bytes,
		ObjectKey:      key,
		RemoteStored:   rec.RemoteStored,
		KeepLocal:      rec.KeepLocal,
		LocalStored:    localStored(rec.DiskPath),
		ObjectBytes:    0,
		ObjectModified: nil,
		ReconcileError: "",
	}
}

func annotateStorageObject(entry *storageObjectEntry, rec storage.ObjectStorageRecord) {
	entry.TorrentID = rec.TorrentID
	entry.TorrentName = rec.TorrentName
	entry.FileID = rec.FileID
	entry.FilePath = rec.Path
	entry.RemoteStored = rec.RemoteStored
	entry.KeepLocal = rec.KeepLocal
	entry.LocalStored = localStored(rec.DiskPath)
}

func torrentFilePayload(f storage.TorrentFileRecord, linksVisible ...bool) map[string]any {
	local := localStored(f.DiskPath)
	state := "missing"
	switch {
	case local && f.RemoteStored:
		state = "local+remote"
	case f.RemoteStored:
		state = "remote-only"
	case local:
		state = "local-only"
	}
	policy := "default"
	if f.KeepLocal {
		policy = "keep-local"
	} else if f.RemoteStored && !local {
		policy = "offload-only"
	}
	payload := map[string]any{
		"id":              f.ID,
		"path":            f.Path,
		"bytes":           f.Bytes,
		"selected":        f.Selected,
		"downloadedBytes": f.DownloadedBytes,
		"streamableBytes": f.StreamableBytes,
		"objectKey":       f.ObjectKey,
		"remoteStored":    f.RemoteStored,
		"keepLocal":       f.KeepLocal,
		"localStored":     local,
		"storageState":    state,
		"storagePolicy":   policy,
	}
	if len(linksVisible) > 0 {
		payload["linksVisible"] = linksVisible[0]
	}
	return payload
}

func localStored(diskPath string) bool {
	if diskPath == "" {
		return false
	}
	st, err := os.Stat(diskPath)
	return err == nil && !st.IsDir()
}

func cleanObjectPrefix(prefix string) string {
	prefix = strings.ReplaceAll(prefix, "\\", "/")
	prefix = strings.TrimLeft(prefix, "/")
	for strings.Contains(prefix, "//") {
		prefix = strings.ReplaceAll(prefix, "//", "/")
	}
	return prefix
}

func requestedObjectPrefix(cfg objectstore.Config, raw string) string {
	raw = cleanObjectPrefix(raw)
	if raw != "" {
		return raw
	}
	base := cleanObjectPrefix(cfg.Prefix)
	if base == "" || strings.HasSuffix(base, "/") {
		return base
	}
	return base + "/"
}

func objectName(prefix, key string) string {
	name := strings.TrimPrefix(key, prefix)
	name = strings.Trim(name, "/")
	if name == "" {
		name = strings.Trim(key, "/")
	}
	if name == "" {
		return key
	}
	return path.Base(name)
}

func boundedQueryInt(r *http.Request, key string, fallback, min, max int) int {
	value := fallback
	if raw := r.URL.Query().Get(key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			value = n
		}
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
