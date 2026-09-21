// Package httpapi exposes the aggregator's HTTP surface.
//
// Handlers are deliberately thin: they parse the request, delegate to a service,
// and encode the response. No aggregation logic lives here.
package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
	"github.com/eugwind/tailscale-news/internal/store"
)

// DefaultItemLimit caps /api/items when the caller does not ask for a limit.
const DefaultItemLimit = 50

// MaxItemLimit caps how many items one request may ask for.
const MaxItemLimit = 500

// BuildInfo describes the running binary, reported by the health endpoint.
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// SourceReporter reports the polling health of every configured source.
type SourceReporter interface {
	Health(sources []feed.Source) []feed.Health
}

// ItemLister returns stored stories matching a filter.
type ItemLister interface {
	List(f store.Filter) []store.Record
	Len() int
}

// NewHandler builds the HTTP routes for the service. reporter and lister may be
// nil, in which case their endpoints report empty results.
func NewHandler(logger *slog.Logger, build BuildInfo, reporter SourceReporter, sources []feed.Source, lister ItemLister) http.Handler {
	mux := http.NewServeMux()
	// "/{$}" matches only the root, so unknown paths still fall through to 404.
	mux.HandleFunc("GET /{$}", indexHandler(logger, build, lister, sources))
	mux.HandleFunc("GET /healthz", healthz(logger, build))
	mux.HandleFunc("GET /sources", sourcesHandler(logger, reporter, sources))
	mux.HandleFunc("GET /api/items", itemsHandler(logger, lister))
	return mux
}

// NewServer returns an http.Server with every timeout set explicitly.
func NewServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func healthz(logger *slog.Logger, build BuildInfo) http.HandlerFunc {
	body := struct {
		Status string `json:"status"`
		BuildInfo
	}{Status: "ok", BuildInfo: build}

	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, logger, body)
	}
}

func sourcesHandler(logger *slog.Logger, reporter SourceReporter, sources []feed.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		health := []feed.Health{}
		if reporter != nil {
			health = reporter.Health(sources)
		}
		writeJSON(w, r, logger, struct {
			Count   int           `json:"count"`
			Sources []feed.Health `json:"sources"`
		}{Count: len(health), Sources: health})
	}
}

func itemsHandler(logger *slog.Logger, lister ItemLister) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if lister == nil {
			writeJSON(w, r, logger, itemsResponse{Items: []store.Record{}})
			return
		}

		filter, err := parseItemFilter(r)
		if err != nil {
			writeError(w, r, logger, http.StatusBadRequest, err.Error())
			return
		}

		records := lister.List(filter)
		writeJSON(w, r, logger, itemsResponse{
			Count: len(records),
			Total: lister.Len(),
			Items: records,
		})
	}
}

type itemsResponse struct {
	Count int            `json:"count"`
	Total int            `json:"total"`
	Items []store.Record `json:"items"`
}

// parseItemFilter reads the query parameters of /api/items. Unknown categories
// are rejected rather than silently returning nothing.
func parseItemFilter(r *http.Request) (store.Filter, error) {
	query := r.URL.Query()
	filter := store.Filter{
		Source: query.Get("source"),
		Limit:  DefaultItemLimit,
	}

	if raw := query.Get("category"); raw != "" {
		category := feed.Category(raw)
		if !feed.ValidCategory(category) {
			return store.Filter{}, fmt.Errorf("unknown category %q", raw)
		}
		filter.Category = category
	}

	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return store.Filter{}, fmt.Errorf("limit %q must be a positive integer", raw)
		}
		filter.Limit = min(limit, MaxItemLimit)
	}

	if raw := query.Get("since"); raw != "" {
		since, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return store.Filter{}, fmt.Errorf("since %q must be an RFC3339 timestamp", raw)
		}
		filter.Since = since
	}

	filter.Query = strings.TrimSpace(query.Get("q"))

	return filter, nil
}

func writeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: message}); err != nil {
		logger.WarnContext(r.Context(), "encode error response",
			slog.String("path", r.URL.Path), slog.Any("error", err))
	}
}

func writeJSON(w http.ResponseWriter, r *http.Request, logger *slog.Logger, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.WarnContext(r.Context(), "encode response",
			slog.String("path", r.URL.Path), slog.Any("error", err))
	}
}
