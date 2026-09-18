// Package aggregate schedules source polling and, later, de-duplicates and
// ranks the results.
package aggregate

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
)

// Poller fetches a single source. It is satisfied by *feed.Fetcher.
type Poller interface {
	Fetch(ctx context.Context, src feed.Source) (feed.Result, error)
}

// Sink receives the result of every successful poll that produced items.
type Sink func(ctx context.Context, result feed.Result)

// Scheduler polls each source on its own interval until its context is
// cancelled. A failing source never stops the others.
type Scheduler struct {
	poller          Poller
	sources         []feed.Source
	defaultInterval time.Duration
	logger          *slog.Logger
	sink            Sink

	// jitter returns the delay before a source's first poll. It is replaced in
	// tests to make start-up deterministic.
	jitter func(interval time.Duration) time.Duration
}

// NewScheduler returns a Scheduler for sources. Sources with a zero
// PollInterval use defaultInterval.
func NewScheduler(poller Poller, sources []feed.Source, defaultInterval time.Duration, logger *slog.Logger, sink Sink) *Scheduler {
	if defaultInterval <= 0 {
		defaultInterval = 15 * time.Minute
	}
	if sink == nil {
		sink = func(context.Context, feed.Result) {}
	}

	return &Scheduler{
		poller:          poller,
		sources:         sources,
		defaultInterval: defaultInterval,
		logger:          logger,
		sink:            sink,
		jitter:          defaultJitter,
	}
}

// defaultJitter spreads the first poll of each source over a tenth of its
// interval, capped at a second, so start-up does not fire every request at once.
func defaultJitter(interval time.Duration) time.Duration {
	window := min(interval/10, time.Second)
	if window <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(window)))
}

// Run polls every source until ctx is cancelled, then waits for in-flight polls
// to finish. It returns nil on clean shutdown.
func (s *Scheduler) Run(ctx context.Context) error {
	if len(s.sources) == 0 {
		s.logger.WarnContext(ctx, "no sources registered, scheduler idle")
		<-ctx.Done()
		return nil
	}

	s.logger.InfoContext(ctx, "scheduler starting", slog.Int("sources", len(s.sources)))

	var wg sync.WaitGroup
	for _, src := range s.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.runSource(ctx, src)
		}()
	}
	wg.Wait()

	s.logger.Info("scheduler stopped")
	return nil
}

func (s *Scheduler) runSource(ctx context.Context, src feed.Source) {
	interval := src.PollInterval
	if interval <= 0 {
		interval = s.defaultInterval
	}

	start := time.NewTimer(s.jitter(interval))
	defer start.Stop()

	select {
	case <-ctx.Done():
		return
	case <-start.C:
	}

	s.pollOnce(ctx, src)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollOnce(ctx, src)
		}
	}
}

func (s *Scheduler) pollOnce(ctx context.Context, src feed.Source) {
	result, err := s.poller.Fetch(ctx, src)
	if err != nil {
		// Cancellation is a normal shutdown, not a source failure. Other
		// failures are already logged with their backoff by the fetcher.
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.logger.DebugContext(ctx, "poll returned an error",
				slog.String("source", src.Name), slog.Any("error", err))
		}
		return
	}

	switch {
	case result.Skipped:
		s.logger.DebugContext(ctx, "poll skipped, source in backoff", slog.String("source", src.Name))
	case result.NotModified:
		s.logger.DebugContext(ctx, "source unchanged", slog.String("source", src.Name))
	default:
		s.logger.InfoContext(ctx, "source polled",
			slog.String("source", src.Name),
			slog.String("category", string(src.Category)),
			slog.Int("items", len(result.Items)),
		)
		s.sink(ctx, result)
	}
}
