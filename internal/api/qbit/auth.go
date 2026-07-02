package qbit

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/debridnest/debridnest/internal/auth"
)

const sessionCookie = "SID"

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]session
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]session)}
}

type session struct {
	expires time.Time
	user    auth.User
}

func (s *sessionStore) create(user auth.User) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sid := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[sid] = session{expires: time.Now().Add(24 * time.Hour), user: user}
	s.mu.Unlock()
	return sid, nil
}

func (s *sessionStore) valid(sid string) (auth.User, bool) {
	if sid == "" {
		return auth.User{}, false
	}
	s.mu.RLock()
	sess, ok := s.sessions[sid]
	s.mu.RUnlock()
	if !ok {
		return auth.User{}, false
	}
	if time.Now().After(sess.expires) {
		s.mu.Lock()
		delete(s.sessions, sid)
		s.mu.Unlock()
		return auth.User{}, false
	}
	return sess.user, true
}

func (s *sessionStore) delete(sid string) {
	if sid == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, sid)
	s.mu.Unlock()
}

func (h *Handler) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err == nil {
			if user, ok := h.sessions.valid(cookie.Value); ok {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
				return
			}
		}
		if user, password, ok := r.BasicAuth(); ok {
			authUser, authenticated := h.authenticateUser(r.Context(), user, password)
			if authenticated {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), authUser)))
				return
			}
		}
		if h.auth != nil {
			if user, ok := h.auth.ValidateToken(r.Context(), r.Header.Get("Authorization")); ok {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
				return
			}
		}
		w.WriteHeader(http.StatusForbidden)
	})
}

func legacyAdminUser() auth.User {
	return auth.User{ID: "legacy", Name: "owner", Role: "admin", Admin: true}
}

func setSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
