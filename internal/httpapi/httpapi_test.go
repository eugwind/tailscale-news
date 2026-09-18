package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
	"github.com/eugwind/tailscale-news/internal/httpapi"
	"github.com/eugwind/tailscale-news/internal/store"
)

func testHandler() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewHandler(logger, httpapi.BuildInfo{Version: "v1.2.3", Commit: "abc1234"}, nil, nil, nil)
}

// recordingLister captures the filter it was given so tests can assert parsing.
type recordingLister struct {
	got     store.Filter
	records []store.Record
}

func (r *recordingLister) List(f store.Filter) []store.Record {
	r.got = f
	return r.records
}

func (r *recordingLister) Len() int { return len(r.records) }

// stubReporter returns canned health for the sources it is given.
type stubReporter struct{ calls int }

func (s *stubReporter) Health(sources []feed.Source) []feed.Health {
	s.calls++
	out := make([]feed.Health, 0, len(sources))
	for _, src := range sources {
		out = append(out, feed.Health{
			Source:        src.Name,
			Category:      src.Category,
			URL:           src.URL,
			LastItemCount: 7,
		})
	}
	return out
}

func TestHandler_Healthz_ReportsBuildInfo(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if got, want := res.Header.Get("Content-Type"), "application/json; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" || body.Version != "v1.2.3" || body.Commit != "abc1234" {
		t.Errorf("body = %+v, want status ok, version v1.2.3, commit abc1234", body)
	}
}

func TestHandler_UnroutedRequests_ReturnExpectedStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		target string
		want   int
	}{
		{"unknown path", http.MethodGet, "/does-not-exist", http.StatusNotFound},
		{"wrong method on healthz", http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, tt.target, nil)
			rec := httptest.NewRecorder()
			testHandler().ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestSources_ReportsHealthForEveryConfiguredSource(t *testing.T) {
	t.Parallel()

	sources := []feed.Source{
		{Name: "alpha", Category: feed.CategoryOfficial, URL: "https://example.test/a.xml"},
		{Name: "bravo", Category: feed.CategorySecurity, URL: "https://example.test/b.xml"},
	}
	reporter := &stubReporter{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.NewHandler(logger, httpapi.BuildInfo{}, reporter, sources, nil)

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Count   int `json:"count"`
		Sources []struct {
			Source        string `json:"source"`
			Category      string `json:"category"`
			LastItemCount int    `json:"last_item_count"`
		} `json:"sources"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Count != 2 || len(body.Sources) != 2 {
		t.Fatalf("count = %d, sources = %d, want 2 and 2", body.Count, len(body.Sources))
	}
	if body.Sources[0].Source != "alpha" || body.Sources[1].Category != "security" {
		t.Errorf("sources = %+v, want alpha first and bravo categorised as security", body.Sources)
	}
	if reporter.calls != 1 {
		t.Errorf("reporter called %d times, want 1", reporter.calls)
	}
}

func TestSources_WithoutReporter_ReturnsEmptyList(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"sources":[]`) {
		t.Errorf("body = %s, want an empty sources array rather than null", got)
	}
}

func TestItems_ReturnsStoredRecords(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	lister := &recordingLister{records: []store.Record{{
		Item: feed.Item{
			Source:    "Tailscale blog",
			Category:  feed.CategoryOfficial,
			Title:     "Subnet failover",
			URL:       "https://example.test/failover",
			Summary:   "A summary.",
			Published: published,
		},
		FirstSeen: published,
		Sources:   []string{"Tailscale blog", "Community forum"},
	}}}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.NewHandler(logger, httpapi.BuildInfo{}, nil, nil, lister)

	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Count int `json:"count"`
		Total int `json:"total"`
		Items []struct {
			Title     string   `json:"Title"`
			URL       string   `json:"URL"`
			Sources   []string `json:"sources"`
			FirstSeen string   `json:"first_seen"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body.Count != 1 || body.Total != 1 {
		t.Errorf("count/total = %d/%d, want 1/1", body.Count, body.Total)
	}
	if len(body.Items) != 1 || body.Items[0].Title != "Subnet failover" {
		t.Fatalf("items = %+v, want the stored story", body.Items)
	}
	if len(body.Items[0].Sources) != 2 {
		t.Errorf("sources = %v, want both recorded sources", body.Items[0].Sources)
	}
}

func TestItems_ParsesQueryParameters(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		query string
		want  store.Filter
	}{
		{
			name:  "defaults",
			query: "",
			want:  store.Filter{Limit: httpapi.DefaultItemLimit},
		},
		{
			name:  "category",
			query: "?category=security",
			want:  store.Filter{Category: feed.CategorySecurity, Limit: httpapi.DefaultItemLimit},
		},
		{
			name:  "source",
			query: "?source=Tailscale+blog",
			want:  store.Filter{Source: "Tailscale blog", Limit: httpapi.DefaultItemLimit},
		},
		{
			name:  "limit",
			query: "?limit=5",
			want:  store.Filter{Limit: 5},
		},
		{
			name:  "limit is capped",
			query: "?limit=100000",
			want:  store.Filter{Limit: httpapi.MaxItemLimit},
		},
		{
			name:  "since",
			query: "?since=2026-09-12T00:00:00Z",
			want:  store.Filter{Since: since, Limit: httpapi.DefaultItemLimit},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lister := &recordingLister{}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := httpapi.NewHandler(logger, httpapi.BuildInfo{}, nil, nil, lister)

			req := httptest.NewRequest(http.MethodGet, "/api/items"+tt.query, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if lister.got.Category != tt.want.Category || lister.got.Source != tt.want.Source ||
				lister.got.Limit != tt.want.Limit || !lister.got.Since.Equal(tt.want.Since) {
				t.Errorf("filter = %+v, want %+v", lister.got, tt.want)
			}
		})
	}
}

func TestItems_InvalidQuery_ReturnsBadRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
	}{
		{"unknown category", "?category=gossip"},
		{"non-numeric limit", "?limit=many"},
		{"zero limit", "?limit=0"},
		{"negative limit", "?limit=-5"},
		{"malformed since", "?since=yesterday"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			handler := httpapi.NewHandler(logger, httpapi.BuildInfo{}, nil, nil, &recordingLister{})

			req := httptest.NewRequest(http.MethodGet, "/api/items"+tt.query, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
			if !strings.Contains(rec.Body.String(), `"error"`) {
				t.Errorf("body = %s, want a JSON error message", rec.Body.String())
			}
		})
	}
}

func TestItems_WithoutStore_ReturnsEmptyList(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/api/items", nil)
	rec := httptest.NewRecorder()
	testHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); !strings.Contains(got, `"items":[]`) {
		t.Errorf("body = %s, want an empty items array rather than null", got)
	}
}

func TestNewServer_SetsAllTimeouts(t *testing.T) {
	t.Parallel()

	srv := httpapi.NewServer(":0", testHandler())

	if srv.Addr != ":0" {
		t.Errorf("Addr = %q, want \":0\"", srv.Addr)
	}
	timeouts := map[string]bool{
		"ReadHeaderTimeout": srv.ReadHeaderTimeout > 0,
		"ReadTimeout":       srv.ReadTimeout > 0,
		"WriteTimeout":      srv.WriteTimeout > 0,
		"IdleTimeout":       srv.IdleTimeout > 0,
	}
	for name, set := range timeouts {
		if !set {
			t.Errorf("%s is not set", name)
		}
	}
}
