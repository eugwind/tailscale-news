package feed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Fetch errors.
var (
	// ErrTooLarge means the response exceeded the fetcher's size limit.
	ErrTooLarge = errors.New("response too large")
	// ErrBadStatus means the source answered with a non-2xx, non-304 status.
	ErrBadStatus = errors.New("unexpected http status")
)

// Fetcher defaults.
const (
	DefaultMaxBytes    = 4 << 20 // the changelog feed alone is ~375 KB
	DefaultUserAgent   = "tailscale-news/dev"
	minBackoff         = 1 * time.Minute
	maxBackoff         = 6 * time.Hour
	maxBackoffAttempts = 12 // caps the shift so the doubling cannot overflow
)

// Result reports the outcome of polling one source.
type Result struct {
	Source Source
	Items  []Item
	// NotModified is true when the source reported or proved no change, in
	// which case Items is empty.
	NotModified bool
	// Skipped is true when the source was still inside its backoff window and
	// no request was made.
	Skipped   bool
	FetchedAt time.Time
}

// Health summarises a source's recent polling history.
type Health struct {
	Source              string    `json:"source"`
	Category            Category  `json:"category"`
	URL                 string    `json:"url"`
	LastAttempt         time.Time `json:"last_attempt,omitzero"`
	LastSuccess         time.Time `json:"last_success,omitzero"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LastError           string    `json:"last_error,omitempty"`
	LastItemCount       int       `json:"last_item_count"`
	Conditional         bool      `json:"supports_conditional_requests"`
}

type sourceState struct {
	etag         string
	lastModified string
	contentHash  [sha256.Size]byte
	hasHash      bool

	lastAttempt time.Time
	lastSuccess time.Time
	failures    int
	lastErr     string
	itemCount   int
	nextAttempt time.Time
}

// Fetcher polls sources over HTTP and parses their responses.
//
// It is safe for concurrent use. Parallelism is bounded by the semaphore given
// to [NewFetcher], so a caller may run one goroutine per source without
// hammering the network.
type Fetcher struct {
	client    *http.Client
	logger    *slog.Logger
	userAgent string
	maxBytes  int64
	sem       chan struct{}
	now       func() time.Time

	mu    sync.Mutex
	state map[string]*sourceState
}

// FetcherOptions configures a [Fetcher]. The zero value is usable: sensible
// defaults are applied for every unset field.
type FetcherOptions struct {
	UserAgent      string
	MaxBytes       int64
	MaxConcurrency int
	// Now overrides the clock. Tests use it to exercise backoff without waiting.
	Now func() time.Time
}

// NewFetcher returns a Fetcher that polls with client and logs through logger.
func NewFetcher(client *http.Client, logger *slog.Logger, opts FetcherOptions) *Fetcher {
	if opts.MaxConcurrency < 1 {
		opts.MaxConcurrency = 1
	}
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultUserAgent
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	return &Fetcher{
		client:    client,
		logger:    logger,
		userAgent: opts.UserAgent,
		maxBytes:  opts.MaxBytes,
		sem:       make(chan struct{}, opts.MaxConcurrency),
		now:       opts.Now,
		state:     make(map[string]*sourceState),
	}
}

// NewHTTPClient returns a client suitable for polling untrusted feeds: a hard
// timeout, a redirect cap, and a refusal to follow a redirect that downgrades
// from https or points at a non-http scheme.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("stopped after 5 redirects")
			}
			if req.URL.Scheme != "https" && via[0].URL.Scheme == "https" {
				return fmt.Errorf("refusing redirect from https to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// Fetch polls one source. A source inside its backoff window is skipped without
// a request. Callers get [Result.NotModified] when the source is unchanged,
// which is detected from a 304 response or, for sources that send no validators,
// from a hash of the response body.
func (f *Fetcher) Fetch(ctx context.Context, src Source) (Result, error) {
	select {
	case f.sem <- struct{}{}:
		defer func() { <-f.sem }()
	case <-ctx.Done():
		return Result{Source: src}, ctx.Err()
	}

	now := f.now()

	f.mu.Lock()
	state := f.state[src.URL]
	if state == nil {
		state = &sourceState{}
		f.state[src.URL] = state
	}
	if !state.nextAttempt.IsZero() && now.Before(state.nextAttempt) {
		f.mu.Unlock()
		return Result{Source: src, Skipped: true, FetchedAt: now}, nil
	}
	etag, lastModified := state.etag, state.lastModified
	f.mu.Unlock()

	body, header, notModified, err := f.request(ctx, src, etag, lastModified)
	if err != nil {
		f.recordFailure(src, err)
		return Result{Source: src, FetchedAt: now}, err
	}

	if notModified {
		f.recordSuccess(src, "", "", nil, -1)
		return Result{Source: src, NotModified: true, FetchedAt: f.now()}, nil
	}

	sum := sha256.Sum256(body)

	f.mu.Lock()
	unchanged := state.hasHash && state.contentHash == sum
	f.mu.Unlock()

	if unchanged {
		f.recordSuccess(src, header.Get("ETag"), header.Get("Last-Modified"), &sum, -1)
		return Result{Source: src, NotModified: true, FetchedAt: f.now()}, nil
	}

	items, err := Parse(bytes.NewReader(body), src)
	if err != nil {
		f.recordFailure(src, err)
		return Result{Source: src, FetchedAt: f.now()}, err
	}

	f.recordSuccess(src, header.Get("ETag"), header.Get("Last-Modified"), &sum, len(items))
	return Result{Source: src, Items: items, FetchedAt: f.now()}, nil
}

func (f *Fetcher) request(ctx context.Context, src Source, etag, lastModified string) ([]byte, http.Header, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("build request for %s: %w", src.Name, err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/xml;q=0.9, text/xml;q=0.8")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, nil, false, fmt.Errorf("fetch %s: %w", src.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, resp.Header, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, resp.Header, false, fmt.Errorf("fetch %s: %d %s: %w",
			src.Name, resp.StatusCode, http.StatusText(resp.StatusCode), ErrBadStatus)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, resp.Header, false, fmt.Errorf("read %s: %w", src.Name, err)
	}
	if int64(len(body)) > f.maxBytes {
		return nil, resp.Header, false, fmt.Errorf("read %s: over %d bytes: %w", src.Name, f.maxBytes, ErrTooLarge)
	}

	return body, resp.Header, false, nil
}

// recordSuccess clears the backoff and stores fresh validators. An itemCount of
// -1 means "unchanged", leaving the previous count in place.
func (f *Fetcher) recordSuccess(src Source, etag, lastModified string, sum *[sha256.Size]byte, itemCount int) {
	now := f.now()

	f.mu.Lock()
	defer f.mu.Unlock()

	state := f.state[src.URL]
	state.lastAttempt = now
	state.lastSuccess = now
	state.failures = 0
	state.lastErr = ""
	state.nextAttempt = time.Time{}
	if etag != "" {
		state.etag = etag
	}
	if lastModified != "" {
		state.lastModified = lastModified
	}
	if sum != nil {
		state.contentHash = *sum
		state.hasHash = true
	}
	if itemCount >= 0 {
		state.itemCount = itemCount
	}
}

func (f *Fetcher) recordFailure(src Source, cause error) {
	now := f.now()

	f.mu.Lock()
	state := f.state[src.URL]
	state.lastAttempt = now
	state.failures++
	state.lastErr = cause.Error()
	state.nextAttempt = now.Add(backoffFor(state.failures))
	failures, retryAt := state.failures, state.nextAttempt
	f.mu.Unlock()

	f.logger.Warn("source poll failed",
		slog.String("source", src.Name),
		slog.String("url", src.URL),
		slog.Int("consecutive_failures", failures),
		slog.Time("retry_after", retryAt),
		slog.Any("error", cause),
	)
}

// backoffFor returns a capped exponential delay with jitter. Jitter keeps
// several failing sources from retrying in lockstep.
func backoffFor(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	shift := min(failures-1, maxBackoffAttempts)
	delay := time.Duration(float64(minBackoff) * math.Pow(2, float64(shift)))
	if delay > maxBackoff || delay <= 0 {
		delay = maxBackoff
	}
	jitter := time.Duration(rand.Int64N(int64(delay) / 4))
	return delay - delay/8 + jitter
}

// Health returns a snapshot of every polled source, ordered by name.
func (f *Fetcher) Health(sources []Source) []Health {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]Health, 0, len(sources))
	for _, src := range sources {
		health := Health{Source: src.Name, Category: src.Category, URL: src.URL}
		if state := f.state[src.URL]; state != nil {
			health.LastAttempt = state.lastAttempt
			health.LastSuccess = state.lastSuccess
			health.ConsecutiveFailures = state.failures
			health.LastError = state.lastErr
			health.LastItemCount = state.itemCount
			health.Conditional = state.etag != "" || state.lastModified != ""
		}
		out = append(out, health)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Source < out[j].Source })
	return out
}
