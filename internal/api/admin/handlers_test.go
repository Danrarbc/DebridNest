package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/activity"
	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
)

func testConfig() config.Config {
	return config.Config{
		APIToken:          "test-token",
		PublicURL:         "http://127.0.0.1:8080",
		Listen:            ":8080",
		TorrentPort:       "0",
		LinkSecret:        "test-token",
		LinkTTL:           12 * time.Hour,
		AutoSelectAfter:   5 * time.Second,
		RetentionDays:     30,
		WebDAVEnabled:     true,
		MetricsEnabled:    true,
		TranscodeEnabled:  false,
		SeedAfterComplete: false,
		QBitUser:          "debridnest",
		MinStreamMB:       8,
		StreamReadaheadMB: 32,
		SeekReadaheadMB:   64,
		SeekPreRollMB:     8,
	}
}

func authHeader(token string) string {
	return "Bearer " + token
}

func newTestServices(t *testing.T, cfg config.Config) (*auth.Service, *settings.Store, *activity.Service, *storage.DB) {
	t.Helper()
	cfg.DataDir = t.TempDir()

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage open: %v", err)
	}

	authSvc, err := auth.New(db, cfg.MultiUserEnabled, cfg.APIToken)
	if err != nil {
		_ = db.Close()
		t.Fatalf("auth: %v", err)
	}

	settingsStore, err := settings.NewStore(db, cfg)
	if err != nil {
		_ = db.Close()
		t.Fatalf("settings: %v", err)
	}

	activitySvc := activity.New(db)
	t.Cleanup(func() { _ = db.Close() })
	return authSvc, settingsStore, activitySvc, db
}

func newTestHandler(t *testing.T) (*Handler, *torrent.Manager, *storage.DB) {
	t.Helper()
	cfg := testConfig()
	authSvc, settingsStore, activitySvc, db := newTestServices(t, cfg)

	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer, settingsStore, objectstore.Config{})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	t.Cleanup(func() { _ = manager.Close() })
	return NewHandler(cfg, manager, nil, activitySvc, settingsStore, authSvc), manager, db
}

func newMultiUserTestHandler(t *testing.T) (*Handler, *auth.Service, *torrent.Manager, *storage.DB) {
	t.Helper()
	cfg := testConfig()
	cfg.MultiUserEnabled = true
	authSvc, settingsStore, activitySvc, db := newTestServices(t, cfg)

	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer, settingsStore, objectstore.Config{})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	t.Cleanup(func() { _ = manager.Close() })
	return NewHandler(cfg, manager, nil, activitySvc, settingsStore, authSvc), authSvc, manager, db
}

func serveWithToken(t *testing.T, h *Handler, token, method, path string, body []byte, contentTypes ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", authHeader(token))
	if len(contentTypes) > 0 {
		req.Header.Set("Content-Type", contentTypes[0])
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

func serve(t *testing.T, h *Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	return serveWithToken(t, h, h.cfg.APIToken, method, path, body)
}

func createUserToken(t *testing.T, authSvc *auth.Service, name string) string {
	t.Helper()
	_, token, err := authSvc.CreateUser(context.Background(), name, "user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return token
}

func multipartTorrentBody(t *testing.T, payload []byte) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("torrent", "sample.torrent")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func TestSystemEndpoint(t *testing.T) {
	cfg := testConfig()
	authSvc, settingsStore, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)

	rec := serve(t, h, http.MethodGet, "/system", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["version"] != version {
		t.Fatalf("version = %v, want %q", resp["version"], version)
	}
	if resp["qbitEnabled"] != true {
		t.Fatalf("qbitEnabled = %v, want true", resp["qbitEnabled"])
	}
	if resp["listen"] != ":8080" {
		t.Fatalf("listen = %v, want :8080", resp["listen"])
	}
	if _, ok := resp["uptime"]; !ok {
		t.Fatal("expected uptime field")
	}
	if _, ok := resp["startedAt"]; !ok {
		t.Fatal("expected startedAt field")
	}
}

func TestMeEndpoint(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := serve(t, h, http.MethodGet, "/me", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "owner" || resp["admin"] != true {
		t.Fatalf("me = %+v", resp)
	}
}

func TestAddMagnet(t *testing.T) {
	h, _, _ := newTestHandler(t)

	body := []byte(`{"magnet":"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test"}`)
	rec := serve(t, h, http.MethodPost, "/torrents/add", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] == "" {
		t.Fatal("expected torrent id")
	}
	if resp["status"] != "magnet_conversion" {
		t.Fatalf("status = %v, want magnet_conversion", resp["status"])
	}
}

func TestRSSImportRunAddsMagnets(t *testing.T) {
	h, _, db := newTestHandler(t)
	infoHash := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss><channel><item><title>Example</title><link>magnet:?xt=urn:btih:` + infoHash + `&amp;dn=Example</link></item></channel></rss>`))
	}))
	t.Cleanup(feed.Close)

	createBody := []byte(`{"name":"Latest movies","url":"` + feed.URL + `"}`)
	createRec := serve(t, h, http.MethodPost, "/imports/rss", createBody)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created storage.RSSImportRecord
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected import id")
	}

	runRec := serve(t, h, http.MethodPost, "/imports/rss/"+created.ID+"/run", nil)
	if runRec.Code != http.StatusOK {
		t.Fatalf("run status = %d, body = %s", runRec.Code, runRec.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(runRec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if result["found"] != float64(1) || result["added"] != float64(1) {
		t.Fatalf("result = %+v, want found/add 1", result)
	}
	if _, err := db.GetTorrentByHash(context.Background(), infoHash); err != nil {
		t.Fatalf("expected torrent from import: %v", err)
	}
}

func TestNonAdminReadStatusAndAddCapabilities(t *testing.T) {
	h, authSvc, _, db := newMultiUserTestHandler(t)
	token := createUserToken(t, authSvc, "reader")
	ctx := context.Background()

	if err := db.CreateTorrent(ctx, storage.TorrentRecord{
		ID:       "READ001",
		InfoHash: "0123456789abcdef0123456789abcdef01234567",
		Name:     "readable",
		Status:   "magnet_conversion",
		AddedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed torrent: %v", err)
	}

	for _, tc := range []struct {
		name string
		path string
	}{
		{"me", "/me"},
		{"system", "/system"},
		{"stats", "/stats"},
		{"config", "/config"},
		{"settings", "/settings"},
		{"torrents", "/torrents"},
		{"torrent detail", "/torrents/READ001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveWithToken(t, h, token, http.MethodGet, tc.path, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
		})
	}

	addBody := []byte(`{"magnet":"magnet:?xt=urn:btih:1111111111111111111111111111111111111111&dn=user-add"}`)
	addRec := serveWithToken(t, h, token, http.MethodPost, "/torrents/add", addBody)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("add status = %d, body = %s", addRec.Code, addRec.Body.String())
	}

	uploadBody, uploadType := multipartTorrentBody(t, []byte("not a torrent"))
	uploadRec := serveWithToken(t, h, token, http.MethodPost, "/torrents/upload", uploadBody, uploadType)
	if uploadRec.Code == http.StatusForbidden {
		t.Fatalf("upload unexpectedly forbidden: body = %s", uploadRec.Body.String())
	}
	if uploadRec.Code != http.StatusBadRequest {
		t.Fatalf("upload status = %d, want 400 from torrent validation; body = %s", uploadRec.Code, uploadRec.Body.String())
	}
}

func TestNonAdminDestructiveCapabilitiesForbidden(t *testing.T) {
	h, authSvc, _, db := newMultiUserTestHandler(t)
	token := createUserToken(t, authSvc, "limited")
	ctx := context.Background()
	now := time.Now().UTC()

	for _, rec := range []storage.TorrentRecord{
		{ID: "DELETE01", InfoHash: "delete01", Name: "delete", Status: "magnet_conversion", AddedAt: now},
		{ID: "BATCH001", InfoHash: "batch001", Name: "batch", Status: "magnet_conversion", AddedAt: now},
		{ID: "RETRY001", InfoHash: "retry001", Name: "retry", Status: "error", AddedAt: now},
		{ID: "LIBSTATE", InfoHash: "libstate", Name: "library", Status: "downloaded", AddedAt: now, EndedAt: &now},
	} {
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("seed torrent %s: %v", rec.ID, err)
		}
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{"delete", http.MethodDelete, "/torrents/DELETE01", nil},
		{"batch delete", http.MethodPost, "/torrents/batch-delete", []byte(`{"ids":["BATCH001"]}`)},
		{"retry", http.MethodPost, "/torrents/RETRY001/retry", nil},
		{"library state", http.MethodPatch, "/library/LIBSTATE", []byte(`{"favorite":true}`)},
		{"maintenance cleanup", http.MethodPost, "/maintenance/cleanup", nil},
		{"purge", http.MethodPost, "/torrents/purge", []byte(`{"filter":"completed"}`)},
		{"storage objects", http.MethodGet, "/storage/objects", nil},
		{"storage reconcile", http.MethodGet, "/storage/reconcile", nil},
		{"file storage policy", http.MethodPatch, "/torrents/DELETE01/files/1/storage", []byte(`{"keepLocal":true}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveWithToken(t, h, token, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body = %s", rec.Code, rec.Body.String())
			}
		})
	}

	for _, id := range []string{"DELETE01", "BATCH001", "RETRY001"} {
		rec, err := db.GetTorrent(ctx, id)
		if err != nil {
			t.Fatalf("torrent %s was removed or hidden: %v", id, err)
		}
		if id == "RETRY001" && rec.Status != "error" {
			t.Fatalf("retry status = %q, want error", rec.Status)
		}
	}
}

func TestLibraryListSearchSortAndPatchState(t *testing.T) {
	h, _, db := newTestHandler(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	endedA := now.Add(-2 * time.Hour)
	endedB := now.Add(-1 * time.Hour)

	seed := []storage.TorrentRecord{
		{
			ID:              "LIB00001",
			InfoHash:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Name:            "Zeta Show",
			Status:          "downloaded",
			Bytes:           700,
			OriginalBytes:   700,
			AddedAt:         now.Add(-24 * time.Hour),
			EndedAt:         &endedA,
			LibraryFavorite: true,
			Files: []storage.TorrentFileRecord{
				{ID: 1, TorrentID: "LIB00001", Path: "/Season 1/Episode One.mkv", Bytes: 700, Selected: true, DownloadedBytes: 700, StreamableBytes: 700},
			},
		},
		{
			ID:            "LIB00002",
			InfoHash:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Name:          "Alpha Movie",
			Status:        "downloaded",
			Bytes:         300,
			OriginalBytes: 300,
			AddedAt:       now.Add(-23 * time.Hour),
			EndedAt:       &endedB,
			Files: []storage.TorrentFileRecord{
				{ID: 1, TorrentID: "LIB00002", Path: "/Alpha Movie.mp4", Bytes: 300, Selected: true, DownloadedBytes: 300, StreamableBytes: 300},
			},
		},
		{
			ID:       "ACTIVE01",
			InfoHash: "cccccccccccccccccccccccccccccccccccccccc",
			Name:     "Still Downloading",
			Status:   "downloading",
			AddedAt:  now,
		},
	}
	for _, rec := range seed {
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create torrent %s: %v", rec.ID, err)
		}
	}

	searchRec := serve(t, h, http.MethodGet, "/library?q=episode&sort=name", nil)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", searchRec.Code, searchRec.Body.String())
	}
	var searchResp []map[string]any
	if err := json.Unmarshal(searchRec.Body.Bytes(), &searchResp); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchResp) != 1 || searchResp[0]["id"] != "LIB00001" {
		t.Fatalf("search response = %+v, want LIB00001 only", searchResp)
	}
	if searchResp[0]["fileCount"] != float64(1) || searchResp[0]["selectedFileCount"] != float64(1) {
		t.Fatalf("file counts = %+v", searchResp[0])
	}

	favoritesRec := serve(t, h, http.MethodGet, "/library?filter=favorites", nil)
	if favoritesRec.Code != http.StatusOK {
		t.Fatalf("favorites status = %d, body = %s", favoritesRec.Code, favoritesRec.Body.String())
	}
	var favoritesResp []map[string]any
	if err := json.Unmarshal(favoritesRec.Body.Bytes(), &favoritesResp); err != nil {
		t.Fatalf("decode favorites: %v", err)
	}
	if len(favoritesResp) != 1 || favoritesResp[0]["id"] != "LIB00001" {
		t.Fatalf("favorites response = %+v, want LIB00001 only", favoritesResp)
	}

	patchRec := serve(t, h, http.MethodPatch, "/library/LIB00002", []byte(`{"pinned":true,"favorite":true,"retentionExempt":true}`))
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}
	var patchResp map[string]any
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patchResp); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patchResp["pinned"] != true || patchResp["favorite"] != true || patchResp["retentionExempt"] != true {
		t.Fatalf("patch state = %+v", patchResp)
	}
	if files, ok := patchResp["files"].([]any); !ok || len(files) != 1 {
		t.Fatalf("patch files = %+v, want one file", patchResp["files"])
	}

	updated, err := db.GetTorrent(ctx, "LIB00002")
	if err != nil {
		t.Fatalf("get updated torrent: %v", err)
	}
	if !updated.LibraryPinned || !updated.LibraryFavorite || !updated.RetentionExempt {
		t.Fatalf("stored state = %+v", updated)
	}

	protectedRec := serve(t, h, http.MethodGet, "/library?filter=protected", nil)
	if protectedRec.Code != http.StatusOK {
		t.Fatalf("protected status = %d, body = %s", protectedRec.Code, protectedRec.Body.String())
	}
	var protectedResp []map[string]any
	if err := json.Unmarshal(protectedRec.Body.Bytes(), &protectedResp); err != nil {
		t.Fatalf("decode protected: %v", err)
	}
	if len(protectedResp) != 1 || protectedResp[0]["id"] != "LIB00002" {
		t.Fatalf("protected response = %+v, want LIB00002 only", protectedResp)
	}
}

func TestAdminDestructiveCapabilitiesStillWork(t *testing.T) {
	h, _, db := newTestHandler(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, rec := range []storage.TorrentRecord{
		{ID: "ADMINDEL", InfoHash: "admindel", Name: "delete", Status: "magnet_conversion", AddedAt: now},
		{ID: "ADMINRTY", InfoHash: "adminrty", Name: "retry", Status: "error", AddedAt: now},
	} {
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("seed torrent %s: %v", rec.ID, err)
		}
	}

	deleteRec := serve(t, h, http.MethodDelete, "/torrents/ADMINDEL", nil)
	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := db.GetTorrent(ctx, "ADMINDEL"); err == nil {
		t.Fatal("deleted torrent still exists")
	}

	retryRec := serve(t, h, http.MethodPost, "/torrents/ADMINRTY/retry", nil)
	if retryRec.Code != http.StatusNoContent {
		t.Fatalf("retry status = %d, body = %s", retryRec.Code, retryRec.Body.String())
	}
	retried, err := db.GetTorrent(ctx, "ADMINRTY")
	if err != nil {
		t.Fatalf("get retried torrent: %v", err)
	}
	if retried.Status != "queued" {
		t.Fatalf("retry status = %q, want queued", retried.Status)
	}

	cleanupRec := serve(t, h, http.MethodPost, "/maintenance/cleanup", nil)
	if cleanupRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("cleanup status = %d, want handler-level 503; body = %s", cleanupRec.Code, cleanupRec.Body.String())
	}
}

func TestBatchDeleteTorrents(t *testing.T) {
	h, manager, db := newTestHandler(t)
	ctx := context.Background()

	ids := []string{"BATCH001", "BATCH002", "BATCH003"}
	for _, id := range ids {
		rec := storage.TorrentRecord{
			ID:       id,
			Name:     id,
			InfoHash: id,
			Status:   "magnet_conversion",
			AddedAt:  time.Now().UTC(),
		}
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create torrent %s: %v", id, err)
		}
	}

	payload, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := serve(t, h, http.MethodPost, "/torrents/batch-delete", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(resp["deleted"].(float64)) != 3 {
		t.Fatalf("deleted = %v, want 3", resp["deleted"])
	}

	items, err := manager.List(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("remaining torrents = %d, want 0", len(items))
	}
}

func TestPurgeCompleted(t *testing.T) {
	h, manager, db := newTestHandler(t)
	ctx := context.Background()

	seed := []struct {
		id     string
		status string
	}{
		{"DOWN0001", "downloaded"},
		{"ERR00001", "error"},
		{"DOWN0002", "downloading"},
	}
	for _, item := range seed {
		rec := storage.TorrentRecord{
			ID:      item.id,
			Name:    item.status,
			Status:  item.status,
			AddedAt: time.Now().UTC(),
		}
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create torrent %s: %v", item.id, err)
		}
	}

	rec := serve(t, h, http.MethodPost, "/torrents/purge", []byte(`{"filter":"completed"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(resp["deleted"].(float64)) != 1 {
		t.Fatalf("deleted = %v, want 1", resp["deleted"])
	}

	items, err := manager.List(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("remaining torrents = %d, want 2", len(items))
	}
}

func TestPurgeUnknownFilter(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := serve(t, h, http.MethodPost, "/torrents/purge", []byte(`{"filter":"unknown"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUnauthorized(t *testing.T) {
	cfg := testConfig()
	authSvc, settingsStore, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)

	req := httptest.NewRequest(http.MethodGet, "/system", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMultiUserCRUD(t *testing.T) {
	cfg := testConfig()
	cfg.MultiUserEnabled = true
	authSvc, settingsStore, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)

	createReq := httptest.NewRequest(http.MethodPost, "/users/", bytes.NewReader([]byte(`{"name":"alice","role":"user"}`)))
	createReq.Header.Set("Authorization", authHeader(cfg.APIToken))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	token, _ := created["token"].(string)
	userID, _ := created["id"].(string)
	if token == "" || userID == "" {
		t.Fatalf("create response = %+v", created)
	}

	listRec := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/users/", nil)
	listReq.Header.Set("Authorization", authHeader(cfg.APIToken))
	h.Routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/me", nil)
	meReq.Header.Set("Authorization", authHeader(token))
	meRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("alice me status = %d", meRec.Code)
	}

	purgeReq := httptest.NewRequest(http.MethodPost, "/torrents/purge", bytes.NewReader([]byte(`{"filter":"completed"}`)))
	purgeReq.Header.Set("Authorization", authHeader(token))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(purgeRec, purgeReq)
	if purgeRec.Code != http.StatusForbidden {
		t.Fatalf("non-admin purge status = %d, want 403", purgeRec.Code)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/users/"+userID, nil)
	delReq.Header.Set("Authorization", authHeader(cfg.APIToken))
	delRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", delRec.Code, delRec.Body.String())
	}
}

func TestPatchUserLimitsAndListUsage(t *testing.T) {
	cfg := testConfig()
	cfg.MultiUserEnabled = true
	authSvc, settingsStore, activitySvc, db := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)
	ctx := context.Background()

	createRec := serveWithToken(t, h, cfg.APIToken, http.MethodPost, "/users/", []byte(`{"name":"limited","role":"user","quotaGb":1,"rateLimitMbps":2}`))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	userID, _ := created["id"].(string)
	if userID == "" {
		t.Fatalf("created response = %+v", created)
	}

	if err := db.CreateTorrent(ctx, storage.TorrentRecord{
		ID:        "LIMITUSE",
		InfoHash:  "1212121212121212121212121212121212121212",
		Status:    "downloaded",
		AddedAt:   time.Now().UTC(),
		OwnerID:   userID,
		OwnerName: "limited",
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/picked.mkv", Bytes: 512, Selected: true},
			{ID: 2, Path: "/ignored.mkv", Bytes: 1024, Selected: false},
		},
	}); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	patchRec := serveWithToken(t, h, cfg.APIToken, http.MethodPatch, "/users/"+userID, []byte(`{"quotaBytes":2048,"rateLimitMbps":3}`))
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}
	var patched map[string]any
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if patched["token"] != nil {
		t.Fatalf("patch leaked token: %+v", patched)
	}
	if patched["quotaBytes"] != float64(2048) || patched["rateLimitMbps"] != float64(3) || patched["usedBytes"] != float64(512) {
		t.Fatalf("patched limits = %+v", patched)
	}

	listRec := serveWithToken(t, h, cfg.APIToken, http.MethodGet, "/users/", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "token") {
		t.Fatalf("list leaked token-bearing field: %s", listRec.Body.String())
	}
}

func TestUserQuotaEnforcedOnSelection(t *testing.T) {
	h, authSvc, manager, db := newMultiUserTestHandler(t)
	ctx := context.Background()
	rec, token, err := authSvc.CreateUser(ctx, "quota-user", "user", auth.UserOptions{QuotaBytes: 100})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, ok := authSvc.ValidateToken(ctx, authHeader(token))
	if !ok {
		t.Fatal("user token did not validate")
	}

	if err := db.CreateTorrent(ctx, storage.TorrentRecord{
		ID:        "QUOTA001",
		InfoHash:  "3434343434343434343434343434343434343434",
		Status:    "waiting_files_selection",
		AddedAt:   time.Now().UTC(),
		OwnerID:   rec.ID,
		OwnerName: rec.Name,
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/too-large.mkv", Bytes: 150},
		},
	}); err != nil {
		t.Fatalf("seed torrent: %v", err)
	}

	err = manager.SelectFiles(auth.WithUser(ctx, user), "QUOTA001", "1")
	if !errors.Is(err, torrent.ErrUserQuotaExceeded) {
		t.Fatalf("select err = %v, want quota exceeded", err)
	}
	_ = h
}

func TestShareLinksAreOwnedAndServeFile(t *testing.T) {
	h, authSvc, manager, db := newMultiUserTestHandler(t)
	ctx := context.Background()
	owner, ownerToken, err := authSvc.CreateUser(ctx, "owner-user", "user")
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	_, otherToken, err := authSvc.CreateUser(ctx, "other-user", "user")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}

	now := time.Now().UTC()
	infoHash := "5656565656565656565656565656565656565656"
	diskPath := filepath.Join(manager.FilesDir(), infoHash, "movie.mkv")
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(diskPath, []byte("share-body"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := db.CreateTorrent(ctx, storage.TorrentRecord{
		ID:        "SHARE001",
		InfoHash:  infoHash,
		Name:      "share movie",
		Status:    "downloaded",
		AddedAt:   now,
		EndedAt:   &now,
		OwnerID:   owner.ID,
		OwnerName: owner.Name,
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/movie.mkv", Bytes: int64(len("share-body")), Selected: true, DownloadedBytes: int64(len("share-body")), StreamableBytes: int64(len("share-body")), DiskPath: diskPath},
		},
	}); err != nil {
		t.Fatalf("seed torrent: %v", err)
	}

	body := []byte(`{"torrentId":"SHARE001","fileId":1,"expiresInHours":1}`)
	otherRec := serveWithToken(t, h, otherToken, http.MethodPost, "/share-links", body)
	if otherRec.Code != http.StatusNotFound {
		t.Fatalf("other create status = %d, body = %s", otherRec.Code, otherRec.Body.String())
	}

	createRec := serveWithToken(t, h, ownerToken, http.MethodPost, "/share-links", body)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	shareURL, _ := created["url"].(string)
	shareID, _ := created["id"].(string)
	if shareURL == "" || shareID == "" {
		t.Fatalf("share response = %+v", created)
	}

	listRec := serveWithToken(t, h, ownerToken, http.MethodGet, "/share-links", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRec.Code, listRec.Body.String())
	}
	parsed, err := url.Parse(shareURL)
	if err != nil {
		t.Fatalf("parse share url: %v", err)
	}
	if strings.Contains(listRec.Body.String(), filepath.Base(parsed.Path)) {
		t.Fatalf("share list leaked raw token: %s", listRec.Body.String())
	}

	public := chi.NewRouter()
	public.Handle("/share/{token}", http.HandlerFunc(h.ServeShareLink))
	req := httptest.NewRequest(http.MethodGet, parsed.Path, nil)
	recorder := httptest.NewRecorder()
	public.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("public status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "share-body" {
		t.Fatalf("public body = %q", recorder.Body.String())
	}

	revokeRec := serveWithToken(t, h, ownerToken, http.MethodDelete, "/share-links/"+shareID, nil)
	if revokeRec.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, body = %s", revokeRec.Code, revokeRec.Body.String())
	}
	afterRevoke := httptest.NewRecorder()
	public.ServeHTTP(afterRevoke, httptest.NewRequest(http.MethodGet, parsed.Path, nil))
	if afterRevoke.Code != http.StatusGone {
		t.Fatalf("revoked status = %d, body = %s", afterRevoke.Code, afterRevoke.Body.String())
	}
}

func TestGetSettingsRedaction(t *testing.T) {
	cfg := testConfig()
	cfg.MultiUserEnabled = true
	authSvc, settingsStore, activitySvc, db := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)
	ctx := context.Background()

	_, err := settingsStore.Patch(ctx, map[string]any{
		"webhookDiscordUrl":  "https://discord.example/secret-hook",
		"webhookGotifyUrl":   "https://gotify.example/message?token=abc",
		"webhookGotifyToken": "super-secret",
		"webhookNtfyTopic":   "my-private-topic",
	})
	if err != nil {
		t.Fatalf("patch settings: %v", err)
	}

	_, token, err := authSvc.CreateUser(ctx, "reader", "user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	adminReq := httptest.NewRequest(http.MethodGet, "/settings", nil)
	adminReq.Header.Set("Authorization", authHeader(cfg.APIToken))
	adminRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusOK {
		t.Fatalf("admin settings status = %d", adminRec.Code)
	}
	var adminResp map[string]any
	if err := json.Unmarshal(adminRec.Body.Bytes(), &adminResp); err != nil {
		t.Fatalf("decode admin: %v", err)
	}
	if adminResp["webhookGotifyToken"] != "super-secret" {
		t.Fatalf("admin token = %v", adminResp["webhookGotifyToken"])
	}

	userReq := httptest.NewRequest(http.MethodGet, "/settings", nil)
	userReq.Header.Set("Authorization", authHeader(token))
	userRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(userRec, userReq)
	if userRec.Code != http.StatusOK {
		t.Fatalf("user settings status = %d", userRec.Code)
	}
	var userResp map[string]any
	if err := json.Unmarshal(userRec.Body.Bytes(), &userResp); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	if userResp["webhookGotifyToken"] != "" {
		t.Fatalf("non-admin got token = %v", userResp["webhookGotifyToken"])
	}
	if userResp["webhookDiscordUrl"] != "(configured)" {
		t.Fatalf("non-admin discord url = %v", userResp["webhookDiscordUrl"])
	}
	_ = db
}

func TestPatchSettingsNotificationFields(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := serve(t, h, http.MethodPatch, "/settings", []byte(`{
		"webhookDiscordUrl":"https://discord.example/hook",
		"notifyOnDownloadComplete":true,
		"notifyOnTorrentProblem":false,
		"notifyOnQuotaWarning":true
	}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["webhookDiscordUrl"] != "https://discord.example/hook" {
		t.Fatalf("webhookDiscordUrl = %v", resp["webhookDiscordUrl"])
	}
	if resp["notifyOnDownloadComplete"] != true {
		t.Fatalf("notifyOnDownloadComplete = %v", resp["notifyOnDownloadComplete"])
	}
	if resp["notifyOnTorrentProblem"] != false {
		t.Fatalf("notifyOnTorrentProblem = %v", resp["notifyOnTorrentProblem"])
	}
	if resp["notifyOnQuotaWarning"] != true {
		t.Fatalf("notifyOnQuotaWarning = %v", resp["notifyOnQuotaWarning"])
	}
}

func TestS3QuotaSettingsConfigAndStats(t *testing.T) {
	h, _, db := newTestHandler(t)
	ctx := context.Background()

	patchRec := serve(t, h, http.MethodPatch, "/settings", []byte(`{"s3Enabled":true,"s3QuotaGb":9}`))
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}
	var settingsResp map[string]any
	if err := json.Unmarshal(patchRec.Body.Bytes(), &settingsResp); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settingsResp["s3QuotaGb"] != float64(9) {
		t.Fatalf("settings s3QuotaGb = %v", settingsResp["s3QuotaGb"])
	}

	rec := storage.TorrentRecord{
		ID:       "S3API",
		InfoHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Status:   "downloaded",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{ID: 1, TorrentID: "S3API", Path: "/remote.mkv", Bytes: 2048, Selected: true, ObjectKey: "remote/movie.mkv", RemoteStored: true},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}

	configRec := serve(t, h, http.MethodGet, "/config", nil)
	if configRec.Code != http.StatusOK {
		t.Fatalf("config status = %d", configRec.Code)
	}
	var configResp map[string]any
	if err := json.Unmarshal(configRec.Body.Bytes(), &configResp); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if configResp["s3QuotaGb"] != float64(9) || configResp["s3Enabled"] != true {
		t.Fatalf("config S3 fields = %+v", configResp)
	}

	statsRec := serve(t, h, http.MethodGet, "/stats", nil)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("stats status = %d", statsRec.Code)
	}
	var statsResp map[string]any
	if err := json.Unmarshal(statsRec.Body.Bytes(), &statsResp); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if statsResp["s3Used"] != float64(2048) || statsResp["s3ObjectCount"] != float64(1) {
		t.Fatalf("stats S3 usage = %+v", statsResp)
	}
	if statsResp["s3QuotaGb"] != float64(9) || statsResp["s3Quota"] != float64(9*1024*1024*1024) {
		t.Fatalf("stats S3 quota = %+v", statsResp)
	}
}

func TestDeleteSelfRejected(t *testing.T) {
	cfg := testConfig()
	cfg.MultiUserEnabled = true
	authSvc, settingsStore, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)
	ctx := context.Background()

	_, adminToken, err := authSvc.CreateUser(ctx, "admin2", "admin")
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	users, err := authSvc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var selfID string
	for _, u := range users {
		if u.Name == "admin2" {
			selfID = u.ID
			break
		}
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/users/"+selfID, nil)
	delReq.Header.Set("Authorization", authHeader(adminToken))
	delRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusBadRequest {
		t.Fatalf("self-delete status = %d, body = %s", delRec.Code, delRec.Body.String())
	}
}

func TestS3TestEndpointDisabled(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "")
	h, _, _ := newTestHandler(t)

	rec := serve(t, h, http.MethodPost, "/settings/s3-test", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body = %s", rec.Code, rec.Body.String())
	}
}

func TestS3TestEndpointEnabledNoBucket(t *testing.T) {
	h, _, _ := newTestHandler(t)

	patchRec := serve(t, h, http.MethodPatch, "/settings", []byte(`{"s3Enabled":true}`))
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}

	rec := serve(t, h, http.MethodPost, "/settings/s3-test", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestS3TestEndpointNilSettings(t *testing.T) {
	cfg := testConfig()
	authSvc, _, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, nil, authSvc)

	req := httptest.NewRequest(http.MethodPost, "/settings/s3-test", nil)
	req.Header.Set("Authorization", authHeader(cfg.APIToken))
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestStorageObjectsDisabled(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := serve(t, h, http.MethodGet, "/storage/objects", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["enabled"] != false {
		t.Fatalf("enabled = %v, want false", resp["enabled"])
	}
	entries, ok := resp["entries"].([]any)
	if !ok || len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty array", resp["entries"])
	}
}

func TestPatchTorrentFileStorageKeepLocal(t *testing.T) {
	h, _, db := newTestHandler(t)
	ctx := context.Background()
	rec := storage.TorrentRecord{
		ID:       "STOREPATCH",
		InfoHash: "8888888888888888888888888888888888888888",
		Status:   "downloaded",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{ID: 1, TorrentID: "STOREPATCH", Path: "/movie.mkv", Bytes: 100, Selected: true},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}

	patchRec := serve(t, h, http.MethodPatch, "/torrents/STOREPATCH/files/1/storage", []byte(`{"keepLocal":true}`))
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(patchRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["keepLocal"] != true || resp["storagePolicy"] != "keep-local" {
		t.Fatalf("storage response = %+v", resp)
	}
	got, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get torrent: %v", err)
	}
	if !got.Files[0].KeepLocal {
		t.Fatal("keep_local was not persisted")
	}
}

func TestStorageReconcileComparesDatabaseAndBucket(t *testing.T) {
	s3 := fakeS3Server(t)
	defer s3.Close()

	h, _, db := newTestHandler(t)
	ctx := context.Background()
	patchRec := serve(t, h, http.MethodPatch, "/settings", []byte(`{
		"s3Enabled": true,
		"s3Endpoint": "`+s3.URL+`",
		"s3Bucket": "bucket",
		"s3AccessKey": "access",
		"s3SecretKey": "secret",
		"s3ForcePathStyle": true,
		"s3Prefix": "prefix"
	}`))
	if patchRec.Code != http.StatusOK {
		t.Fatalf("settings patch status = %d, body = %s", patchRec.Code, patchRec.Body.String())
	}

	for _, rec := range []storage.TorrentRecord{
		{
			ID:       "RECONOK",
			InfoHash: "hash",
			Status:   "downloaded",
			AddedAt:  time.Now().UTC(),
			Files: []storage.TorrentFileRecord{
				{ID: 1, TorrentID: "RECONOK", Path: "/ok.mkv", Bytes: 100, Selected: true, ObjectKey: "prefix/hash/ok.mkv", RemoteStored: true},
			},
		},
		{
			ID:       "RECONMISS",
			InfoHash: "hash",
			Status:   "downloaded",
			AddedAt:  time.Now().UTC(),
			Files: []storage.TorrentFileRecord{
				{ID: 1, TorrentID: "RECONMISS", Path: "/missing.mkv", Bytes: 100, Selected: true, ObjectKey: "prefix/hash/missing.mkv", RemoteStored: true},
			},
		},
		{
			ID:       "RECONSIZE",
			InfoHash: "hash",
			Status:   "downloaded",
			AddedAt:  time.Now().UTC(),
			Files: []storage.TorrentFileRecord{
				{ID: 1, TorrentID: "RECONSIZE", Path: "/size.mkv", Bytes: 50, Selected: true, ObjectKey: "prefix/hash/size.mkv", RemoteStored: true},
			},
		},
		{
			ID:       "RECONUNMARKED",
			InfoHash: "hash",
			Status:   "downloaded",
			AddedAt:  time.Now().UTC(),
			Files: []storage.TorrentFileRecord{
				{ID: 1, TorrentID: "RECONUNMARKED", Path: "/unmarked.mkv", Bytes: 70, Selected: true},
			},
		},
	} {
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create %s: %v", rec.ID, err)
		}
	}

	reconcileRec := serve(t, h, http.MethodGet, "/storage/reconcile", nil)
	if reconcileRec.Code != http.StatusOK {
		t.Fatalf("reconcile status = %d, body = %s", reconcileRec.Code, reconcileRec.Body.String())
	}
	var resp struct {
		DBRemoteCount  int   `json:"dbRemoteCount"`
		ObjectCount    int   `json:"objectCount"`
		MatchedCount   int   `json:"matchedCount"`
		MissingRemote  []any `json:"missingRemote"`
		SizeMismatches []any `json:"sizeMismatches"`
		OrphanedRemote []any `json:"orphanedRemote"`
		UnmarkedRemote []any `json:"unmarkedRemote"`
	}
	if err := json.Unmarshal(reconcileRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode reconcile: %v", err)
	}
	if resp.DBRemoteCount != 3 || resp.ObjectCount != 4 || resp.MatchedCount != 1 {
		t.Fatalf("summary = %+v", resp)
	}
	if len(resp.MissingRemote) != 1 || len(resp.SizeMismatches) != 1 || len(resp.OrphanedRemote) != 1 || len(resp.UnmarkedRemote) != 1 {
		t.Fatalf("findings = missing:%d mismatched:%d orphan:%d unmarked:%d", len(resp.MissingRemote), len(resp.SizeMismatches), len(resp.OrphanedRemote), len(resp.UnmarkedRemote))
	}
}

func fakeS3Server(t *testing.T) *httptest.Server {
	t.Helper()
	objects := map[string]int64{
		"prefix/hash/ok.mkv":       100,
		"prefix/hash/size.mkv":     999,
		"prefix/hash/orphan.mkv":   20,
		"prefix/hash/unmarked.mkv": 70,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		key = strings.TrimPrefix(key, "/bucket/")
		if r.Method == http.MethodHead {
			size, ok := objects[key]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			w.Header().Set("Last-Modified", "Thu, 02 Jul 2026 12:00:00 GMT")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">
  <Name>bucket</Name>
  <Prefix>prefix/</Prefix>
  <KeyCount>4</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents><Key>prefix/hash/ok.mkv</Key><LastModified>2026-07-02T12:00:00.000Z</LastModified><Size>100</Size></Contents>
  <Contents><Key>prefix/hash/size.mkv</Key><LastModified>2026-07-02T12:00:00.000Z</LastModified><Size>999</Size></Contents>
  <Contents><Key>prefix/hash/orphan.mkv</Key><LastModified>2026-07-02T12:00:00.000Z</LastModified><Size>20</Size></Contents>
  <Contents><Key>prefix/hash/unmarked.mkv</Key><LastModified>2026-07-02T12:00:00.000Z</LastModified><Size>70</Size></Contents>
</ListBucketResult>`))
			return
		}
		http.NotFound(w, r)
	}))
}
