package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

const (
	defaultShareTTL = 24 * time.Hour
	maxShareTTL     = 30 * 24 * time.Hour
)

func (h *Handler) listShareLinks(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "bad_token")
		return
	}
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager unavailable")
		return
	}

	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	includeAll := user.Admin && r.URL.Query().Get("all") == "1"
	items, err := h.manager.ListShareLinks(r.Context(), user.ID, includeAll, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []storage.ShareLinkRecord{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) createShareLink(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "bad_token")
		return
	}
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager unavailable")
		return
	}

	var body struct {
		TorrentID      string  `json:"torrentId"`
		FileID         int     `json:"fileId"`
		ExpiresInHours float64 `json:"expiresInHours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.TorrentID == "" || body.FileID <= 0 {
		writeError(w, http.StatusBadRequest, "torrentId and fileId are required")
		return
	}

	rec, err := h.manager.Get(r.Context(), body.TorrentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "torrent not found")
		return
	}
	file, err := shareableFile(rec, body.FileID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !torrentmgr.NewLifecycle(h.cfg.MinStreamBytes()).FileLinksVisible(rec, *file) {
		writeError(w, http.StatusBadRequest, torrentmgr.ErrStreamNotReady.Error())
		return
	}

	token, err := auth.GenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := newShareID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(shareTTL(body.ExpiresInHours))
	ownerID, ownerName := rec.OwnerID, rec.OwnerName
	if ownerID == "" {
		ownerID, ownerName = user.ID, user.Name
	}
	share := storage.ShareLinkRecord{
		ID:        id,
		TokenHash: auth.HashToken(token),
		OwnerID:   ownerID,
		OwnerName: ownerName,
		TorrentID: rec.ID,
		FileID:    file.ID,
		Filename:  filepath.Base(file.Path),
		MimeType:  links.MimeType(file.Path),
		Filesize:  file.Bytes,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	if err := h.manager.CreateShareLink(r.Context(), share); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionShareCreate, map[string]any{
		"shareId":   id,
		"torrentId": rec.ID,
		"fileId":    file.ID,
		"expiresAt": expiresAt,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        id,
		"url":       fmt.Sprintf("%s/share/%s", h.cfg.PublicURL, token),
		"expiresAt": expiresAt,
		"filename":  share.Filename,
		"filesize":  share.Filesize,
	})
}

func (h *Handler) revokeShareLink(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "bad_token")
		return
	}
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager unavailable")
		return
	}

	id := chi.URLParam(r, "id")
	share, err := h.manager.GetShareLinkByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "share link not found")
		return
	}
	if !user.Admin && share.OwnerID != user.ID {
		writeError(w, http.StatusNotFound, "share link not found")
		return
	}
	if err := h.manager.RevokeShareLink(r.Context(), id, time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.LogActivity(r.Context(), ActionShareRevoke, map[string]any{"shareId": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ServeShareLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager unavailable")
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusNotFound, "share link not found")
		return
	}
	share, err := h.manager.GetShareLinkByTokenHash(r.Context(), auth.HashToken(token))
	if err != nil {
		writeError(w, http.StatusNotFound, "share link not found")
		return
	}
	if share.RevokedAt != nil || time.Now().After(share.ExpiresAt) {
		writeError(w, http.StatusGone, "share link expired")
		return
	}

	rec, err := h.manager.Get(r.Context(), share.TorrentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	file, err := shareableFile(rec, share.FileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	byteRange := torrentmgr.ParseRange(r.Header.Get("Range"), file.Bytes)
	reader, modTime, _, err := h.manager.OpenServingReader(r.Context(), rec.ID, file.ID, torrentmgr.StreamOptions{
		StartOffset:   byteRange.Start,
		RequestLength: byteRange.Length,
	})
	if err != nil {
		if errors.Is(err, torrentmgr.ErrStreamNotReady) {
			w.Header().Set("Retry-After", "2")
			writeError(w, http.StatusServiceUnavailable, "stream not ready yet")
			return
		}
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	defer reader.Close()

	_ = h.manager.MarkShareLinkAccessed(r.Context(), share.ID, time.Now().UTC())

	filename := share.Filename
	if filename == "" {
		filename = filepath.Base(file.Path)
	}
	mimeType := share.MimeType
	if mimeType == "" {
		mimeType = links.MimeType(filename)
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(filename))
	w.Header().Set("Accept-Ranges", "bytes")

	limited := reader
	if limiter := h.shareDownloadRateLimiter(r.Context(), share.OwnerID); limiter != nil {
		limited = limiter.ReadSeekCloser(reader)
	}
	http.ServeContent(w, r, filename, modTime, limited)
}

func (h *Handler) shareDownloadRateLimiter(ctx context.Context, ownerID string) *links.RateLimiter {
	limit := h.effectiveRateLimitMbps()
	if h.auth != nil && ownerID != "" {
		if user, err := h.auth.GetUser(ctx, ownerID); err == nil && user.RateLimitMbps > 0 {
			if limit > 0 && limit < user.RateLimitMbps {
				return links.NewRateLimiter(limit)
			}
			return links.NewRateLimiter(user.RateLimitMbps)
		}
	}
	return links.NewRateLimiter(limit)
}

func shareableFile(rec *storage.TorrentRecord, fileID int) (*storage.TorrentFileRecord, error) {
	for i := range rec.Files {
		if rec.Files[i].ID != fileID {
			continue
		}
		file := &rec.Files[i]
		if !file.Selected {
			return nil, errors.New("file is not selected")
		}
		return file, nil
	}
	return nil, errors.New("file not found")
}

func shareTTL(hours float64) time.Duration {
	if hours <= 0 {
		return defaultShareTTL
	}
	ttl := time.Duration(hours * float64(time.Hour))
	if ttl > maxShareTTL {
		return maxShareTTL
	}
	if ttl < time.Minute {
		return time.Minute
	}
	return ttl
}

func newShareID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
