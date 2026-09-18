package aggregate

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakePoller records every call and returns a scripted result.
type fakePoller struct {
	calls  chan feed.Source
	mu     sync.Mutex
	result feed.Result
	err    error
}

func newFakePoller(buffer int) *fakePoller {
	return &fakePoller{calls: make(chan feed.Source, buffer)}
}

func (f *fakePoller) Fetch(ctx context.Context, src feed.Source) (feed.Result, error) {
	f.mu.Lock()
	result, err := f.result, f.err
	f.mu.Unlock()

	select {
	case f.calls <- src:
	default:
	}

	if err != nil {
		return feed.Result{Source: src}, err
	}
	result.Source = src
	return result, nil
}

func (f *fakePoller) setOutcome(result feed.Result, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result, f.err = result, err
}

// newTestScheduler builds a scheduler with jitter disabled so the first poll is
// immediate and the test does not depend on wall-clock luck.
func newTestScheduler(poller Poller, sources []feed.Source, sink Sink) *Scheduler {
	s := NewScheduler(poller, sources, time.Hour, testLogger(), sink)
	s.jitter = func(time.Duration) time.Duration { return 0 }
	return s
}

func waitForCalls(t *testing.T, calls <-chan feed.Source, n int) []feed.Source {
	t.Helper()

	got := make([]feed.Source, 0, n)
	deadline := time.After(2 * time.Second)
	for len(got) < n {
		select {
		case src := <-calls:
			got = append(got, src)
		case <-deadline:
			t.Fatalf("timed out waiting for %d polls, got %d", n, len(got))
		}
	}
	return got
}

func TestScheduler_PollsEverySourceOnStartup(t *testing.T) {
	t.Parallel()

	poller := newFakePoller(16)
	poller.setOutcome(feed.Result{Items: []feed.Item{{Title: "one"}}}, nil)

	sources := []feed.Source{
		{Name: "alpha", URL: "https://example.test/a.xml", PollInterval: time.Hour},
		{Name: "bravo", URL: "https://example.test/b.xml", PollInterval: time.Hour},
		{Name: "charlie", URL: "https://example.test/c.xml", PollInterval: time.Hour},
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- newTestScheduler(poller, sources, nil).Run(ctx) }()

	seen := make(map[string]bool)
	for _, src := range waitForCalls(t, poller.calls, len(sources)) {
		seen[src.Name] = true
	}
	for _, src := range sources {
		if !seen[src.Name] {
			t.Errorf("source %q was never polled", src.Name)
		}
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestScheduler_RepollsOnInterval(t *testing.T) {
	t.Parallel()

	poller := newFakePoller(16)
	sources := []feed.Source{{Name: "alpha", URL: "https://example.test/a.xml", PollInterval: 10 * time.Millisecond}}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- newTestScheduler(poller, sources, nil).Run(ctx) }()

	waitForCalls(t, poller.calls, 3)

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestScheduler_DeliversItemsToSink(t *testing.T) {
	t.Parallel()

	poller := newFakePoller(4)
	poller.setOutcome(feed.Result{Items: []feed.Item{{Title: "one"}, {Title: "two"}}}, nil)

	delivered := make(chan feed.Result, 4)
	sources := []feed.Source{{Name: "alpha", URL: "https://example.test/a.xml", PollInterval: time.Hour}}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	sink := func(_ context.Context, result feed.Result) { delivered <- result }
	done := make(chan error, 1)
	go func() { done <- newTestScheduler(poller, sources, sink).Run(ctx) }()

	select {
	case result := <-delivered:
		if len(result.Items) != 2 {
			t.Errorf("sink received %d items, want 2", len(result.Items))
		}
		if result.Source.Name != "alpha" {
			t.Errorf("sink received source %q, want %q", result.Source.Name, "alpha")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sink was never called")
	}

	cancel()
	<-done
}

func TestScheduler_SinkSkippedForUnproductivePolls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result feed.Result
		err    error
	}{
		{name: "not modified", result: feed.Result{NotModified: true}},
		{name: "skipped by backoff", result: feed.Result{Skipped: true}},
		{name: "fetch failed", err: errors.New("boom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			poller := newFakePoller(8)
			poller.setOutcome(tt.result, tt.err)

			var sinkCalls int
			var mu sync.Mutex
			sink := func(context.Context, feed.Result) {
				mu.Lock()
				sinkCalls++
				mu.Unlock()
			}

			sources := []feed.Source{{Name: "alpha", URL: "https://example.test/a.xml", PollInterval: 5 * time.Millisecond}}

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- newTestScheduler(poller, sources, sink).Run(ctx) }()

			waitForCalls(t, poller.calls, 2)
			cancel()
			<-done

			mu.Lock()
			defer mu.Unlock()
			if sinkCalls != 0 {
				t.Errorf("sink called %d times, want 0", sinkCalls)
			}
		})
	}
}

func TestScheduler_FailingSourceDoesNotStopOthers(t *testing.T) {
	t.Parallel()

	failing := newFakePoller(8)
	failing.setOutcome(feed.Result{}, errors.New("always broken"))

	sources := []feed.Source{
		{Name: "broken", URL: "https://example.test/broken.xml", PollInterval: 5 * time.Millisecond},
		{Name: "healthy", URL: "https://example.test/ok.xml", PollInterval: 5 * time.Millisecond},
	}

	// One poller serves both sources; the error applies to every call, which is
	// the harshest case: the healthy source must still be polled repeatedly.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- newTestScheduler(failing, sources, nil).Run(ctx) }()

	counts := make(map[string]int)
	for _, src := range waitForCalls(t, failing.calls, 6) {
		counts[src.Name]++
	}

	if counts["healthy"] == 0 {
		t.Error("the healthy source was never polled while another source was failing")
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run() error = %v, want nil", err)
	}
}

func TestScheduler_NoSources_IdlesUntilCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- newTestScheduler(newFakePoller(1), nil, nil).Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() error = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after cancellation")
	}
}

func TestScheduler_ZeroPollInterval_UsesDefault(t *testing.T) {
	t.Parallel()

	poller := newFakePoller(8)
	sources := []feed.Source{{Name: "alpha", URL: "https://example.test/a.xml"}}

	scheduler := NewScheduler(poller, sources, 5*time.Millisecond, testLogger(), nil)
	scheduler.jitter = func(time.Duration) time.Duration { return 0 }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()

	waitForCalls(t, poller.calls, 2)

	cancel()
	<-done
}
