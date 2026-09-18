// Package httpapi exposes the aggregator's HTTP surface.
//
// Handlers are deliberately thin: they parse the request, delegate to a service,
// and encode the response. No aggregation logic lives here.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// BuildInfo describes the running binary, reported by the health endpoint.
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// NewHandler builds the HTTP routes for the service.
func NewHandler(logger *slog.Logger, build BuildInfo) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz(logger, build))
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			logger.WarnContext(r.Context(), "encode health response", slog.Any("error", err))
		}
	}
}
