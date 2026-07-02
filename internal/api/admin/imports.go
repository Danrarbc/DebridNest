package admin

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/storage"
)

const maxRSSImportBytes = 5 << 20

type rssImportRunResult struct {
	ID        string    `json:"id"`
	CheckedAt time.Time `json:"checkedAt"`
	Found     int       `json:"found"`
	Added     int       `json:"added"`
	Errors    []string  `json:"errors"`
}

func (h *Handler) listRSSImports(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager not configured")
		return
	}
	items, err := h.manager.ListRSSImports(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []storage.RSSImportRecord{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) createRSSImport(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager not configured")
		return
	}

	var body struct {
		Name    string `json:"name"`
		URL     string `json:"url"`
		Enabled *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.URL = strings.TrimSpace(body.URL)
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	if err := validateRSSURL(body.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := newShareID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	rec := storage.RSSImportRecord{
		ID:        id,
		Name:      body.Name,
		URL:       body.URL,
		Enabled:   enabled,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.manager.CreateRSSImport(r.Context(), rec); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.LogActivity(r.Context(), ActionImportCreate, map[string]any{
		"importId": id,
		"name":     body.Name,
	})
	writeJSON(w, http.StatusCreated, rec)
}

func (h *Handler) deleteRSSImport(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager not configured")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.manager.DeleteRSSImport(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.LogActivity(r.Context(), ActionImportDelete, map[string]any{"importId": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) runRSSImport(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeError(w, http.StatusServiceUnavailable, "torrent manager not configured")
		return
	}
	id := chi.URLParam(r, "id")
	rec, err := h.manager.GetRSSImport(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rss import not found")
		return
	}
	if !rec.Enabled {
		writeError(w, http.StatusBadRequest, "rss import is disabled")
		return
	}

	magnets, err := fetchRSSMagnets(r.Context(), rec.URL)
	if err != nil {
		ranAt := time.Now().UTC()
		_ = h.manager.UpdateRSSImportRunResult(r.Context(), id, ranAt, "failed", 0, err.Error())
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	result := rssImportRunResult{
		ID:        id,
		CheckedAt: time.Now().UTC(),
		Found:     len(magnets),
		Errors:    []string{},
	}
	for _, magnet := range magnets {
		if _, err := h.manager.AddMagnet(r.Context(), magnet); err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		result.Added++
	}
	status := "ok"
	lastErr := ""
	if len(result.Errors) > 0 {
		status = "partial"
		lastErr = strings.Join(result.Errors, "; ")
	}
	if err := h.manager.UpdateRSSImportRunResult(r.Context(), id, result.CheckedAt, status, result.Added, lastErr); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.LogActivity(r.Context(), ActionImportRun, map[string]any{
		"importId": id,
		"found":    result.Found,
		"added":    result.Added,
		"errors":   len(result.Errors),
	})
	writeJSON(w, http.StatusOK, result)
}

func fetchRSSMagnets(ctx context.Context, feedURL string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/rss+xml, application/xml, text/xml;q=0.9, */*;q=0.5")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("rss feed returned %s", resp.Status)
	}
	return extractRSSMagnets(resp.Body)
}

func validateRSSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("valid rss url required")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("rss url must use http or https")
	}
	return nil
}

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
	Items   []rssItem  `xml:"item"`
	Entries []rssItem  `xml:"entry"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title      string         `xml:"title"`
	GUID       string         `xml:"guid"`
	ID         string         `xml:"id"`
	Enclosures []rssEnclosure `xml:"enclosure"`
	Attrs      []rssAttr      `xml:"attr"`
	Links      []rssLink      `xml:"link"`
}

type rssEnclosure struct {
	URL string `xml:"url,attr"`
}

type rssAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type rssLink struct {
	Href string `xml:"href,attr"`
	Text string `xml:",chardata"`
}

func extractRSSMagnets(body io.Reader) ([]string, error) {
	var feed rssFeed
	decoder := xml.NewDecoder(io.LimitReader(body, maxRSSImportBytes))
	if err := decoder.Decode(&feed); err != nil {
		return nil, err
	}

	items := append([]rssItem{}, feed.Channel.Items...)
	items = append(items, feed.Items...)
	items = append(items, feed.Entries...)

	seen := make(map[string]bool)
	var magnets []string
	addCandidate := func(candidate string) {
		magnet := normalizeMagnetCandidate(candidate)
		if magnet == "" || seen[magnet] {
			return
		}
		seen[magnet] = true
		magnets = append(magnets, magnet)
	}
	for _, item := range items {
		addCandidate(item.GUID)
		addCandidate(item.ID)
		for _, enclosure := range item.Enclosures {
			addCandidate(enclosure.URL)
		}
		for _, attr := range item.Attrs {
			if strings.EqualFold(attr.Name, "magneturl") || strings.EqualFold(attr.Name, "magnet") {
				addCandidate(attr.Value)
			}
		}
		for _, link := range item.Links {
			addCandidate(link.Href)
			addCandidate(link.Text)
		}
	}
	return magnets, nil
}

func normalizeMagnetCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}
	if decoded, err := url.QueryUnescape(candidate); err == nil {
		candidate = decoded
	}
	idx := strings.Index(strings.ToLower(candidate), "magnet:?")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(candidate[idx:])
}
