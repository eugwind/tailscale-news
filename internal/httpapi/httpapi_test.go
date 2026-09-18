package httpapi_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eugwind/tailscale-news/internal/httpapi"
)

func testHandler() http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewHandler(logger, httpapi.BuildInfo{Version: "v1.2.3", Commit: "abc1234"})
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
