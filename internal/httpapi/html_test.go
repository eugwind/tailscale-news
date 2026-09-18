package httpapi_test

import (
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

func htmlHandler(lister httpapi.ItemLister, sources []feed.Source) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return httpapi.NewHandler(logger, httpapi.BuildInfo{Version: "v1.2.3"}, nil, sources, lister)
}

func record(source string, category feed.Category, title, url string, published time.Time, alsoIn ...string) store.Record {
	return store.Record{
		Item: feed.Item{
			Source:    source,
			Category:  category,
			Title:     title,
			URL:       url,
			Summary:   "A short summary of the story.",
			Published: published,
		},
		FirstSeen: published,
		Sources:   append([]string{source}, alsoIn...),
	}
}

func get(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestIndex_RendersStories(t *testing.T) {
	t.Parallel()

	published := time.Now().Add(-48 * time.Hour)
	lister := &recordingLister{records: []store.Record{
		record("Tailscale blog", feed.CategoryOfficial, "Subnet failover", "https://tailscale.com/blog/failover", published),
		record("Tailscale security bulletins", feed.CategorySecurity, "TS-2026-011", "https://tailscale.com/security-bulletins#ts-2026-011", published),
	}}

	rec := get(t, htmlHandler(lister, make([]feed.Source, 5)), "/")
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/html; charset=utf-8"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	for _, want := range []string{
		"Subnet failover",
		"TS-2026-011",
		"https://tailscale.com/blog/failover",
		"Tailscale security bulletins",
		"2 days ago",
		"tailscale.com",
		`rel="noopener noreferrer external"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page is missing %q", want)
		}
	}
}

func TestIndex_EscapesUntrustedContent(t *testing.T) {
	t.Parallel()

	lister := &recordingLister{records: []store.Record{
		record("Evil feed", feed.CategoryCommunity,
			`<script>alert("xss")</script>`,
			"https://example.test/story?a=1&b=2",
			time.Now()),
	}}

	rec := get(t, htmlHandler(lister, nil), "/")
	body := rec.Body.String()

	if strings.Contains(body, "<script>alert") {
		t.Error("an unescaped <script> tag from feed content reached the page")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("the malicious title was not HTML-escaped")
	}
	if !strings.Contains(body, "a=1&amp;b=2") {
		t.Error("the URL query was not escaped in the href")
	}
}

func TestIndex_RejectsJavascriptURL(t *testing.T) {
	t.Parallel()

	lister := &recordingLister{records: []store.Record{
		record("Evil feed", feed.CategoryCommunity, "Click me", "javascript:alert(1)", time.Now()),
	}}

	body := get(t, htmlHandler(lister, nil), "/").Body.String()
	if strings.Contains(body, `href="javascript:`) {
		t.Error("a javascript: URL was rendered as a live href")
	}
}

// liveSources mirrors the registered source categories used by the nav chips.
var liveSources = []feed.Source{
	{Name: "blog", Category: feed.CategoryOfficial, URL: "https://example.test/a.xml"},
	{Name: "changelog", Category: feed.CategoryReleaseNotes, URL: "https://example.test/b.xml"},
	{Name: "bulletins", Category: feed.CategorySecurity, URL: "https://example.test/c.xml"},
	{Name: "dev", Category: feed.CategoryDevelopment, URL: "https://example.test/d.xml"},
}

func TestIndex_CategoryNavigation(t *testing.T) {
	t.Parallel()

	lister := &recordingLister{}
	handler := htmlHandler(lister, liveSources)

	rec := get(t, handler, "/?category=security")
	body := rec.Body.String()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if lister.got.Category != feed.CategorySecurity {
		t.Errorf("filter category = %q, want %q", lister.got.Category, feed.CategorySecurity)
	}
	if !strings.Contains(body, `href="/?category=security" class="active"`) {
		t.Error("the active category chip is not marked active")
	}
	if !strings.Contains(body, `href="/?category=release-notes"`) {
		t.Error("the releases filter link is missing")
	}
}

func TestIndex_OnlyOffersCategoriesThatHaveASource(t *testing.T) {
	t.Parallel()

	body := get(t, htmlHandler(&recordingLister{}, liveSources), "/").Body.String()

	for _, want := range []string{"Security", "Releases", "Official", "Dev"} {
		if !strings.Contains(body, ">"+want+"<") {
			t.Errorf("chip %q is missing even though a source produces it", want)
		}
	}
	for _, unwanted := range []string{"Community", "Third-party"} {
		if strings.Contains(body, ">"+unwanted+"<") {
			t.Errorf("chip %q is offered but no registered source produces that category", unwanted)
		}
	}
}

func TestIndex_EmptyStore_ShowsPlaceholder(t *testing.T) {
	t.Parallel()

	body := get(t, htmlHandler(&recordingLister{}, nil), "/").Body.String()
	if !strings.Contains(body, "No stories yet") {
		t.Error("an empty store should render a placeholder, not a blank list")
	}
}

// emptyFilterLister reports a populated store whose filter matches nothing.
type emptyFilterLister struct{ total int }

func (e *emptyFilterLister) List(store.Filter) []store.Record { return nil }
func (e *emptyFilterLister) Len() int                         { return e.total }

func TestIndex_EmptyFilterOverFullStore_OffersAnEscape(t *testing.T) {
	t.Parallel()

	body := get(t, htmlHandler(&emptyFilterLister{total: 398}, liveSources), "/?category=security").Body.String()

	if strings.Contains(body, "No stories yet") {
		t.Error(`a populated store must not claim "No stories yet" — that blames the scheduler for a filter miss`)
	}
	if !strings.Contains(body, "No stories match this filter") {
		t.Error("an empty filter result should say the filter matched nothing")
	}
	if !strings.Contains(body, `<a href="/">Show all 398</a>`) {
		t.Error("an empty filter result should link back to the full list")
	}
}

func TestIndex_WithoutStore_StillRenders(t *testing.T) {
	t.Parallel()

	rec := get(t, htmlHandler(nil, nil), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Tailscale News") {
		t.Error("the page did not render without a store")
	}
}

func TestIndex_InvalidQuery_ReturnsBadRequest(t *testing.T) {
	t.Parallel()

	rec := get(t, htmlHandler(&recordingLister{}, nil), "/?category=gossip")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestIndex_UnknownPath_IsNotSwallowedByRoot(t *testing.T) {
	t.Parallel()

	rec := get(t, htmlHandler(&recordingLister{}, nil), "/not-a-page")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d — \"/{$}\" must match only the root", rec.Code, http.StatusNotFound)
	}
}

func TestIndex_Theme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    string
		cookie    *http.Cookie
		want      string
		wantSaved string
	}{
		{
			name:   "defaults to auto",
			target: "/",
			want:   "auto",
		},
		{
			name:      "query selects dark and persists it",
			target:    "/?theme=dark",
			want:      "dark",
			wantSaved: "dark",
		},
		{
			name:      "query selects light and persists it",
			target:    "/?theme=light",
			want:      "light",
			wantSaved: "light",
		},
		{
			name:   "cookie is honoured without a query",
			target: "/",
			cookie: &http.Cookie{Name: "tsnews_theme", Value: "dark"},
			want:   "dark",
		},
		{
			name:      "query overrides the cookie",
			target:    "/?theme=light",
			cookie:    &http.Cookie{Name: "tsnews_theme", Value: "dark"},
			want:      "light",
			wantSaved: "light",
		},
		{
			name:      "unknown theme falls back to auto",
			target:    "/?theme=neon",
			want:      "auto",
			wantSaved: "auto",
		},
		{
			name:   "tampered cookie falls back to auto",
			target: "/",
			cookie: &http.Cookie{Name: "tsnews_theme", Value: "'; DROP TABLE"},
			want:   "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			rec := httptest.NewRecorder()
			htmlHandler(&recordingLister{}, nil).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if want := `<html lang="en" data-theme="` + tt.want + `">`; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("page is missing %q", want)
			}

			saved := ""
			for _, c := range rec.Result().Cookies() {
				if c.Name == "tsnews_theme" {
					saved = c.Value
				}
			}
			if saved != tt.wantSaved {
				t.Errorf("persisted theme = %q, want %q", saved, tt.wantSaved)
			}
		})
	}
}

func TestIndex_ThemeCookie_IsHardened(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?theme=dark", nil)
	rec := httptest.NewRecorder()
	htmlHandler(&recordingLister{}, nil).ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies, want 1", len(cookies))
	}

	cookie := cookies[0]
	if !cookie.HttpOnly {
		t.Error("theme cookie is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want \"/\"", cookie.Path)
	}
	if cookie.MaxAge <= 0 {
		t.Errorf("MaxAge = %d, want a positive lifetime", cookie.MaxAge)
	}
}

func TestIndex_ThemeAndCategory_PreserveEachOther(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/?category=security&theme=dark", nil)
	rec := httptest.NewRecorder()
	htmlHandler(&recordingLister{}, liveSources).ServeHTTP(rec, req)
	body := rec.Body.String()

	// A theme link must keep the active category, and vice versa.
	if !strings.Contains(body, `href="/?category=security&amp;theme=light"`) {
		t.Error("the theme switcher dropped the active category filter")
	}
	if !strings.Contains(body, `href="/?category=official&amp;theme=dark"`) {
		t.Error("the category chips dropped the selected theme")
	}
}

func TestIndex_UndatedStory_ShowsFirstSeen(t *testing.T) {
	t.Parallel()

	undated := store.Record{
		Item:      feed.Item{Source: "Learn", Category: feed.CategoryOfficial, Title: "Undated", URL: "https://example.test/x"},
		FirstSeen: time.Now().Add(-3 * time.Hour),
		Sources:   []string{"Learn"},
	}

	body := get(t, htmlHandler(&recordingLister{records: []store.Record{undated}}, nil), "/").Body.String()
	if !strings.Contains(body, "first seen 3 hours ago") {
		t.Error("an undated story should fall back to its first-seen time")
	}
}

func TestIndex_MultiSourceStory_ListsOtherSources(t *testing.T) {
	t.Parallel()

	shared := record("Tailscale blog", feed.CategoryOfficial, "Shared story",
		"https://example.test/shared", time.Now(), "Community forum")

	body := get(t, htmlHandler(&recordingLister{records: []store.Record{shared}}, nil), "/").Body.String()
	if !strings.Contains(body, "Also carried by:") || !strings.Contains(body, "Community forum") {
		t.Error("a story carried by several sources should name them")
	}
}
