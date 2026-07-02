package admin

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/auth"
)

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "bad_token")
		return
	}
	resp := map[string]any{
		"id":            user.ID,
		"name":          user.Name,
		"role":          user.Role,
		"admin":         user.Admin,
		"quotaBytes":    user.QuotaBytes,
		"rateLimitMbps": user.RateLimitMbps,
	}
	if h.auth != nil && h.auth.MultiUserEnabled() && user.ID != "" {
		if rec, err := h.auth.GetUser(r.Context(), user.ID); err == nil {
			resp["usedBytes"] = rec.UsedBytes
			resp["quotaBytes"] = rec.QuotaBytes
			resp["rateLimitMbps"] = rec.RateLimitMbps
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	users, err := h.auth.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []auth.UserRecord{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var body struct {
		Name          string  `json:"name"`
		Role          string  `json:"role"`
		QuotaBytes    int64   `json:"quotaBytes"`
		QuotaGB       float64 `json:"quotaGb"`
		RateLimitMbps float64 `json:"rateLimitMbps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Role == "" {
		body.Role = "user"
	}

	opts, err := userOptionsFromValues(body.QuotaBytes, body.QuotaGB, body.RateLimitMbps)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rec, token, err := h.auth.CreateUser(r.Context(), body.Name, body.Role, opts)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserExists) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionUserCreate, map[string]any{
		"userId": rec.ID,
		"name":   rec.Name,
		"role":   rec.Role,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":    rec.ID,
		"name":  rec.Name,
		"role":  rec.Role,
		"token": token,
	})
}

func (h *Handler) patchUser(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id := chi.URLParam(r, "id")
	current, err := h.auth.GetUser(r.Context(), id)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	var body struct {
		QuotaBytes    *int64   `json:"quotaBytes"`
		QuotaGB       *float64 `json:"quotaGb"`
		RateLimitMbps *float64 `json:"rateLimitMbps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	quotaBytes := current.QuotaBytes
	if body.QuotaBytes != nil {
		quotaBytes = *body.QuotaBytes
	}
	if body.QuotaGB != nil {
		quotaBytes = gbToBytes(*body.QuotaGB)
	}
	rateLimit := current.RateLimitMbps
	if body.RateLimitMbps != nil {
		rateLimit = *body.RateLimitMbps
	}

	rec, err := h.auth.UpdateUserLimits(r.Context(), id, auth.UserOptions{
		QuotaBytes:    quotaBytes,
		RateLimitMbps: rateLimit,
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionUserUpdate, map[string]any{
		"userId":        rec.ID,
		"quotaBytes":    rec.QuotaBytes,
		"rateLimitMbps": rec.RateLimitMbps,
	})
	writeJSON(w, http.StatusOK, rec)
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id := chi.URLParam(r, "id")
	if user, ok := auth.UserFromContext(r.Context()); ok && user.ID == id {
		writeError(w, http.StatusBadRequest, auth.ErrSelfDelete.Error())
		return
	}
	if err := h.auth.DeleteUser(r.Context(), id); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, auth.ErrLastAdmin) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionUserDelete, map[string]any{"userId": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) rotateUserToken(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id := chi.URLParam(r, "id")
	token, err := h.auth.RotateToken(r.Context(), id)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionUserRotateToken, map[string]any{"userId": id})

	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}

func userOptionsFromValues(quotaBytes int64, quotaGB float64, rateLimitMbps float64) (auth.UserOptions, error) {
	if quotaGB != 0 {
		quotaBytes = gbToBytes(quotaGB)
	}
	opts := auth.UserOptions{
		QuotaBytes:    quotaBytes,
		RateLimitMbps: rateLimitMbps,
	}
	if opts.QuotaBytes < 0 {
		return auth.UserOptions{}, errors.New("quota bytes must be non-negative")
	}
	if opts.RateLimitMbps < 0 {
		return auth.UserOptions{}, errors.New("rate limit must be non-negative")
	}
	return opts, nil
}

func gbToBytes(gb float64) int64 {
	if gb <= 0 {
		return 0
	}
	bytes := gb * 1024 * 1024 * 1024
	if bytes > float64(math.MaxInt64) {
		return math.MaxInt64
	}
	return int64(bytes)
}
