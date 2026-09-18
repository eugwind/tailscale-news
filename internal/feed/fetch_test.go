package feed_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

// newTestFetcher wires a fetcher to srv with an optional fake clock.
func newTestFetcher(t *testing.T, now func() time.Time) *feed.Fetcher {
	t.Helper()

	return feed.NewFetcher(
		&http.Client{Timeout: 5 * time.Second},
		discardLogger(),
		feed.FetcherOptions{MaxConcurrency: 2, Now: now},
	)
}

func TestFetch_ParsesItems(t *testing.T) {
	t.Parallel()

	body := fixtureBytes(t, "blog.rss.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write(body)
	}))
	defer srv.Close()

	src := feed.Source{Name: "test", Category: feed.CategoryOfficial, URL: srv.URL}
	got, err := newTestFetcher(t, nil).Fetch(t.Context(), src)
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil", err)
	}
	if got.NotModified || got.Skipped {
		t.Errorf("NotModified/Skipped = %t/%t, want false/false", got.NotModified, got.Skipped)
	}
	if len(got.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3", len(got.Items))
	}
	if got.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero")
	}
}

func TestFetch_SendsPoliteHeaders(t *testing.T) {
	t.Parallel()

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write(fixtureBytes(t, "empty.rss.xml"))
	}))
	defer srv.Close()

	fetcher := feed.NewFetcher(srv.Client(), discardLogger(), feed.FetcherOptions{UserAgent: "tailscale-news/test"})
	if _, err := fetcher.Fetch(t.Context(), feed.Source{Name: "test", URL: srv.URL}); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if want := "tailscale-news/test"; got.Get("User-Agent") != want {
		t.Errorf("User-Agent = %q, want %q", got.Get("User-Agent"), want)
	}
	if accept := got.Get("Accept"); !strings.Contains(accept, "xml") {
		t.Errorf("Accept = %q, want it to mention xml", accept)
	}
}

func TestFetch_ConditionalRequest_ReturnsNotModified(t *testing.T) {
	t.Parallel()

	const etag = `"v1"`
	var requests int
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()

		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", "Wed, 09 Sep 2026 20:40:04 GMT")
		w.Write(fixtureBytes(t, "blog.rss.xml"))
	}))
	defer srv.Close()

	fetcher := newTestFetcher(t, nil)
	src := feed.Source{Name: "test", URL: srv.URL}

	first, err := fetcher.Fetch(t.Context(), src)
	if err != nil {
		t.Fatalf("first Fetch() error = %v", err)
	}
	if first.NotModified || len(first.Items) == 0 {
		t.Fatalf("first fetch: NotModified = %t, items = %d, want false and >0", first.NotModified, len(first.Items))
	}

	second, err := fetcher.Fetch(t.Context(), src)
	if err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if !second.NotModified {
		t.Error("second fetch: NotModified = false, want true (the ETag must be sent back)")
	}
	if len(second.Items) != 0 {
		t.Errorf("second fetch: len(Items) = %d, want 0", len(second.Items))
	}

	health := fetcher.Health([]feed.Source{src})
	if !health[0].Conditional {
		t.Error("Health.Conditional = false, want true once a validator is stored")
	}
}

func TestFetch_NoValidators_UsesContentHash(t *testing.T) {
	t.Parallel()

	body := fixtureBytes(t, "blog.rss.xml")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != "" {
			t.Errorf("If-None-Match = %q, want it unset when the source sends no validators", r.Header.Get("If-None-Match"))
		}
		w.Write(body)
	}))
	defer srv.Close()

	fetcher := newTestFetcher(t, nil)
	src := feed.Source{Name: "test", URL: srv.URL}

	if _, err := fetcher.Fetch(t.Context(), src); err != nil {
		t.Fatalf("first Fetch() error = %v", err)
	}

	second, err := fetcher.Fetch(t.Context(), src)
	if err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if !second.NotModified {
		t.Error("NotModified = false, want true — an identical body must be detected by hash")
	}

	health := fetcher.Health([]feed.Source{src})
	if health[0].Conditional {
		t.Error("Health.Conditional = true, want false for a source with no validators")
	}
	if health[0].LastItemCount != 3 {
		t.Errorf("LastItemCount = %d, want 3 preserved across an unchanged poll", health[0].LastItemCount)
	}
}

func TestFetch_ChangedBody_ReparsesItems(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	current := fixtureBytes(t, "empty.rss.xml")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Write(current)
	}))
	defer srv.Close()

	fetcher := newTestFetcher(t, nil)
	src := feed.Source{Name: "test", URL: srv.URL}

	first, err := fetcher.Fetch(t.Context(), src)
	if err != nil {
		t.Fatalf("first Fetch() error = %v", err)
	}
	if len(first.Items) != 0 {
		t.Fatalf("first fetch: len(Items) = %d, want 0", len(first.Items))
	}

	mu.Lock()
	current = fixtureBytes(t, "blog.rss.xml")
	mu.Unlock()

	second, err := fetcher.Fetch(t.Context(), src)
	if err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if second.NotModified {
		t.Error("NotModified = true, want false after the body changed")
	}
	if len(second.Items) != 3 {
		t.Errorf("len(Items) = %d, want 3", len(second.Items))
	}
}

func TestFetch_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{
			name:    "server error",
			handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			wantErr: feed.ErrBadStatus,
		},
		{
			name:    "not found",
			handler: func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantErr: feed.ErrBadStatus,
		},
		{
			name: "html instead of a feed",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("<html><body>nope</body></html>"))
			},
			wantErr: feed.ErrUnsupportedFormat,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			fetcher := newTestFetcher(t, nil)
			src := feed.Source{Name: "test", URL: srv.URL}

			if _, err := fetcher.Fetch(t.Context(), src); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Fetch() error = %v, want %v", err, tt.wantErr)
			}

			health := fetcher.Health([]feed.Source{src})
			if health[0].ConsecutiveFailures != 1 {
				t.Errorf("ConsecutiveFailures = %d, want 1", health[0].ConsecutiveFailures)
			}
			if health[0].LastError == "" {
				t.Error("LastError is empty, want the failure reason")
			}
		})
	}
}

func TestFetch_OversizedResponse_IsRejected(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("a", 4096)))
	}))
	defer srv.Close()

	fetcher := feed.NewFetcher(srv.Client(), discardLogger(), feed.FetcherOptions{MaxBytes: 1024})
	if _, err := fetcher.Fetch(t.Context(), feed.Source{Name: "test", URL: srv.URL}); !errors.Is(err, feed.ErrTooLarge) {
		t.Fatalf("Fetch() error = %v, want %v", err, feed.ErrTooLarge)
	}
}

func TestFetch_AfterFailure_SkipsUntilBackoffExpires(t *testing.T) {
	t.Parallel()

	var requests int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	clock := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	fetcher := newTestFetcher(t, func() time.Time { return clock })
	src := feed.Source{Name: "test", URL: srv.URL}

	if _, err := fetcher.Fetch(t.Context(), src); err == nil {
		t.Fatal("Fetch() error = nil, want a failure")
	}

	got, err := fetcher.Fetch(t.Context(), src)
	if err != nil {
		t.Fatalf("Fetch() during backoff error = %v, want nil", err)
	}
	if !got.Skipped {
		t.Error("Skipped = false, want true while inside the backoff window")
	}

	mu.Lock()
	afterSkip := requests
	mu.Unlock()
	if afterSkip != 1 {
		t.Errorf("server saw %d requests, want 1 — the skipped poll must not hit the network", afterSkip)
	}

	clock = clock.Add(maxTestBackoff)
	if _, err := fetcher.Fetch(t.Context(), src); err == nil {
		t.Fatal("Fetch() after backoff error = nil, want the failure to repeat")
	}

	mu.Lock()
	afterRetry := requests
	mu.Unlock()
	if afterRetry != 2 {
		t.Errorf("server saw %d requests, want 2 once the backoff expired", afterRetry)
	}
}

// maxTestBackoff exceeds the first backoff step, which is at most one minute.
const maxTestBackoff = 2 * time.Minute

func TestFetch_SuccessAfterFailure_ClearsBackoff(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	fail := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		shouldFail := fail
		mu.Unlock()

		if shouldFail {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write(fixtureBytes(t, "blog.rss.xml"))
	}))
	defer srv.Close()

	clock := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	fetcher := newTestFetcher(t, func() time.Time { return clock })
	src := feed.Source{Name: "test", URL: srv.URL}

	if _, err := fetcher.Fetch(t.Context(), src); err == nil {
		t.Fatal("Fetch() error = nil, want a failure")
	}

	mu.Lock()
	fail = false
	mu.Unlock()
	clock = clock.Add(maxTestBackoff)

	if _, err := fetcher.Fetch(t.Context(), src); err != nil {
		t.Fatalf("Fetch() error = %v, want nil", err)
	}

	health := fetcher.Health([]feed.Source{src})
	if health[0].ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0 after a success", health[0].ConsecutiveFailures)
	}
	if health[0].LastError != "" {
		t.Errorf("LastError = %q, want it cleared after a success", health[0].LastError)
	}
	if health[0].LastSuccess.IsZero() {
		t.Error("LastSuccess is zero after a successful poll")
	}
}

func TestFetch_ContextCancellation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	fetcher := newTestFetcher(t, nil)
	if _, err := fetcher.Fetch(ctx, feed.Source{Name: "test", URL: srv.URL}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Fetch() error = %v, want %v", err, context.Canceled)
	}
}

func TestFetch_BoundsConcurrency(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	active, peak := 0, 0
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		peak = max(peak, active)
		mu.Unlock()

		<-release

		mu.Lock()
		active--
		mu.Unlock()

		w.Write(fixtureBytes(t, "empty.rss.xml"))
	}))
	defer srv.Close()

	const limit = 2
	fetcher := feed.NewFetcher(srv.Client(), discardLogger(), feed.FetcherOptions{MaxConcurrency: limit})

	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			src := feed.Source{Name: "test", URL: srv.URL + "/?n=" + string(rune('a'+i))}
			fetcher.Fetch(t.Context(), src)
		}()
	}

	// Let the first batch reach the handler, then let everything drain.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if peak > limit {
		t.Errorf("peak concurrent requests = %d, want at most %d", peak, limit)
	}
}

func TestHealth_ReportsEverySourceEvenBeforePolling(t *testing.T) {
	t.Parallel()

	fetcher := newTestFetcher(t, nil)
	sources := []feed.Source{
		{Name: "zulu", URL: "https://example.test/z.xml"},
		{Name: "alpha", URL: "https://example.test/a.xml"},
	}

	health := fetcher.Health(sources)
	if len(health) != 2 {
		t.Fatalf("len(Health()) = %d, want 2", len(health))
	}
	if health[0].Source != "alpha" {
		t.Errorf("Health()[0].Source = %q, want %q (results must be sorted)", health[0].Source, "alpha")
	}
	if !health[0].LastAttempt.IsZero() || health[0].ConsecutiveFailures != 0 {
		t.Error("an unpolled source should report a zero attempt time and no failures")
	}
}
