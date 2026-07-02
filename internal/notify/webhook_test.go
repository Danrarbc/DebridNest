package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type staticSettings struct {
	settings Settings
}

func (s staticSettings) NotifySettings() Settings {
	return s.settings
}

func TestDiscordWebhookNotifications(t *testing.T) {
	requests := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content type = %q, want application/json", got)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		requests <- payload["content"]
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service := New(staticSettings{settings: Settings{
		DiscordWebhookURL:        server.URL,
		NotifyOnDownloadComplete: true,
		NotifyOnTorrentProblem:   true,
		NotifyOnQuotaWarning:     true,
	}})

	service.NotifyDownloadComplete("movie.mkv")
	service.NotifyTorrentProblem("stalled magnet", "magnet_error")
	service.NotifyQuotaWarning(95, 100)

	contents := collectWebhookContents(t, requests, 3)
	assertAnyContentContains(t, contents, "**Download complete**", "Torrent finished: movie.mkv")
	assertAnyContentContains(t, contents, "**Torrent failed or stuck**", "stalled magnet", "magnet_error")
	assertAnyContentContains(t, contents, "**Disk quota warning**", "95%")
}

func TestDisabledEventsDoNotPost(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	service := New(staticSettings{settings: Settings{
		DiscordWebhookURL: server.URL,
	}})
	service.NotifyDownloadComplete("movie.mkv")
	service.NotifyTorrentProblem("stalled magnet", "magnet_error")
	service.NotifyQuotaWarning(95, 100)

	select {
	case got := <-requests:
		t.Fatalf("unexpected webhook request to %s", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func collectWebhookContents(t *testing.T, requests <-chan string, want int) []string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	contents := make([]string, 0, want)
	for len(contents) < want {
		select {
		case content := <-requests:
			contents = append(contents, content)
		case <-deadline:
			t.Fatalf("received %d webhook request(s), want %d", len(contents), want)
		}
	}
	return contents
}

func assertAnyContentContains(t *testing.T, contents []string, parts ...string) {
	t.Helper()
	for _, content := range contents {
		matched := true
		for _, part := range parts {
			if !strings.Contains(content, part) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("no webhook content contained %v; got %#v", parts, contents)
}
