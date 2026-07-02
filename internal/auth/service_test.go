package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/storage"
)

func openTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestLegacyValidateToken(t *testing.T) {
	db := openTestDB(t)
	svc, err := auth.New(db, false, "secret-token")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	u, ok := svc.ValidateToken(context.Background(), "Bearer secret-token")
	if !ok {
		t.Fatal("expected valid legacy token")
	}
	if !u.Admin || u.Name != "owner" {
		t.Fatalf("user = %+v, want admin owner", u)
	}

	if _, ok := svc.ValidateToken(context.Background(), "Bearer wrong"); ok {
		t.Fatal("expected invalid token")
	}
}

func TestMultiUserBootstrapAndValidate(t *testing.T) {
	db := openTestDB(t)
	const legacy = "bootstrap-token"
	svc, err := auth.New(db, true, legacy)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	u, ok := svc.ValidateToken(context.Background(), "Bearer "+legacy)
	if !ok {
		t.Fatal("expected bootstrapped owner token")
	}
	if u.Name != "owner" || !u.Admin {
		t.Fatalf("user = %+v", u)
	}

	users, err := svc.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 || users[0].Name != "owner" {
		t.Fatalf("users = %+v", users)
	}
}

func TestCreateDeleteRotateUser(t *testing.T) {
	db := openTestDB(t)
	svc, err := auth.New(db, true, "owner-token")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx := context.Background()
	rec, token, err := svc.CreateUser(ctx, "reader", "user")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if token == "" || rec.Name != "reader" {
		t.Fatalf("rec = %+v token empty = %v", rec, token == "")
	}

	u, ok := svc.ValidateToken(ctx, "Bearer "+token)
	if !ok || u.Name != "reader" || u.Admin {
		t.Fatalf("validated = %+v ok=%v", u, ok)
	}

	newToken, err := svc.RotateToken(ctx, rec.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newToken == token {
		t.Fatal("expected new token after rotate")
	}
	if _, ok := svc.ValidateToken(ctx, "Bearer "+token); ok {
		t.Fatal("old token should be invalid")
	}
	if _, ok := svc.ValidateToken(ctx, "Bearer "+newToken); !ok {
		t.Fatal("new token should be valid")
	}

	if err := svc.DeleteUser(ctx, rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := svc.ValidateToken(ctx, "Bearer "+newToken); ok {
		t.Fatal("deleted user token should be invalid")
	}
}

func TestUserLimitsAndUsage(t *testing.T) {
	db := openTestDB(t)
	svc, err := auth.New(db, true, "owner-token")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	ctx := context.Background()
	rec, token, err := svc.CreateUser(ctx, "metered", "user", auth.UserOptions{
		QuotaBytes:    1024,
		RateLimitMbps: 1.5,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if rec.QuotaBytes != 1024 || rec.RateLimitMbps != 1.5 {
		t.Fatalf("created limits = %+v", rec)
	}

	u, ok := svc.ValidateToken(ctx, "Bearer "+token)
	if !ok {
		t.Fatal("token did not validate")
	}
	if u.QuotaBytes != 1024 || u.RateLimitMbps != 1.5 {
		t.Fatalf("validated limits = %+v", u)
	}

	if err := db.CreateTorrent(ctx, storage.TorrentRecord{
		ID:        "USAGE001",
		InfoHash:  "9999999999999999999999999999999999999999",
		Status:    "downloaded",
		AddedAt:   time.Now().UTC(),
		OwnerID:   rec.ID,
		OwnerName: rec.Name,
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/selected.mkv", Bytes: 600, Selected: true},
			{ID: 2, Path: "/skipped.mkv", Bytes: 700, Selected: false},
		},
	}); err != nil {
		t.Fatalf("create torrent: %v", err)
	}

	got, err := svc.GetUser(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if got.UsedBytes != 600 {
		t.Fatalf("used bytes = %d, want 600", got.UsedBytes)
	}

	updated, err := svc.UpdateUserLimits(ctx, rec.ID, auth.UserOptions{
		QuotaBytes:    2048,
		RateLimitMbps: 0.75,
	})
	if err != nil {
		t.Fatalf("update limits: %v", err)
	}
	if updated.QuotaBytes != 2048 || updated.RateLimitMbps != 0.75 || updated.UsedBytes != 600 {
		t.Fatalf("updated user = %+v", updated)
	}
}

func TestDeleteLastAdmin(t *testing.T) {
	db := openTestDB(t)
	svc, err := auth.New(db, true, "owner-token")
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	ctx := context.Background()

	users, err := svc.ListUsers(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("list users: %v len=%d", err, len(users))
	}

	if err := svc.DeleteUser(ctx, users[0].ID); !errors.Is(err, auth.ErrLastAdmin) {
		t.Fatalf("delete last admin err = %v, want ErrLastAdmin", err)
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	a := auth.HashToken("abc")
	b := auth.HashToken("abc")
	if a != b || len(a) != 64 {
		t.Fatalf("hash = %q len=%d", a, len(a))
	}
}
