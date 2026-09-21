package httpapi

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
	"github.com/eugwind/tailscale-news/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

var pageTemplates = template.Must(template.New("").Funcs(templateFuncs()).ParseFS(templateFS, "templates/*.html"))

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"isoDate":   func(t time.Time) string { return t.UTC().Format(time.RFC3339) },
		"shortDate": func(t time.Time) string { return t.UTC().Format("2 Jan 2006") },
		"age":       func(t time.Time) string { return humanAge(time.Since(t)) },
		"join":      func(values []string) string { return strings.Join(values, ", ") },
		"host":      hostOf,
		"truncate":  truncate,
	}
}

// humanAge renders a duration the way a reader thinks about it, not to the second.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	case d < 365*24*time.Hour:
		return plural(int(d.Hours()/24/30), "month") + " ago"
	default:
		return plural(int(d.Hours()/24/365), "year") + " ago"
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsed.Host, "www.")
}

// truncate cuts text at the last word boundary within limit runes.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:limit])
	if space := strings.LastIndex(cut, " "); space > limit/2 {
		cut = cut[:space]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

type categoryLink struct {
	Label  string
	Href   string
	Active bool
}

// themeCookie persists the reader's theme choice. The page has no JavaScript,
// so the server resolves the theme and the cookie need not be script-readable.
const themeCookie = "tsnews_theme"

// themes are the selectable colour schemes; "auto" follows the OS setting.
var themes = []struct {
	Label string
	Value string
}{
	{"Auto", "auto"},
	{"Light", "light"},
	{"Dark", "dark"},
}

func validTheme(value string) bool {
	for _, theme := range themes {
		if theme.Value == value {
			return true
		}
	}
	return false
}

// resolveTheme reads the theme from the query string, falling back to the
// cookie and then to "auto". A theme given in the query is also persisted.
func resolveTheme(w http.ResponseWriter, r *http.Request) string {
	if requested := r.URL.Query().Get("theme"); requested != "" {
		if !validTheme(requested) {
			requested = "auto"
		}
		http.SetCookie(w, &http.Cookie{
			Name:     themeCookie,
			Value:    requested,
			Path:     "/",
			MaxAge:   int((365 * 24 * time.Hour).Seconds()),
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		return requested
	}

	if cookie, err := r.Cookie(themeCookie); err == nil && validTheme(cookie.Value) {
		return cookie.Value
	}
	return "auto"
}

// linkWith rebuilds the current URL with one query parameter replaced, so the
// theme switcher keeps the active category and the category chips keep the theme.
func linkWith(r *http.Request, key, value string) string {
	query := r.URL.Query()
	if value == "" {
		query.Del(key)
	} else {
		query.Set(key, value)
	}
	if len(query) == 0 {
		return "/"
	}
	return "/?" + query.Encode()
}

type indexPage struct {
	Title           string
	Version         string
	Theme           string
	Query           string
	ClearSearchHref string
	Category        feed.Category
	Items           []store.Record
	Count           int
	Total           int
	SourceCount     int
	Categories      []categoryLink
	Themes          []categoryLink
}

// navCategories are the filter chips, ordered by editorial weight. A chip is
// rendered only when a registered source produces that category.
var navCategories = []struct {
	Label string
	Value feed.Category
}{
	{"All", ""},
	{"Security", feed.CategorySecurity},
	{"Releases", feed.CategoryReleaseNotes},
	{"Official", feed.CategoryOfficial},
	{"Dev", feed.CategoryDevelopment},
	{"Community", feed.CategoryCommunity},
	{"Third-party", feed.CategoryThirdParty},
}

func indexHandler(logger *slog.Logger, build BuildInfo, lister ItemLister, sources []feed.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, err := parseItemFilter(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		page := indexPage{
			Title:           "Tailscale News",
			Version:         build.Version,
			Theme:           resolveTheme(w, r),
			Query:           filter.Query,
			ClearSearchHref: linkWith(r, "q", ""),
			Category:        filter.Category,
			Items:           []store.Record{},
			SourceCount:     len(sources),
			Categories:      buildCategoryLinks(r, filter.Category, sources),
		}
		page.Themes = buildThemeLinks(r, page.Theme)
		if lister != nil {
			page.Items = lister.List(filter)
			page.Total = lister.Len()
		}
		page.Count = len(page.Items)

		// Render to a buffer so a template failure cannot emit a half-written page.
		var buf bytes.Buffer
		if err := pageTemplates.ExecuteTemplate(&buf, "index.html", page); err != nil {
			logger.ErrorContext(r.Context(), "render index", slog.Any("error", err))
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(buf.Bytes())
	}
}

// buildCategoryLinks offers "All" plus only those categories that a registered
// source can actually produce, so no chip leads to a permanently empty page.
func buildCategoryLinks(r *http.Request, active feed.Category, sources []feed.Source) []categoryLink {
	available := make(map[feed.Category]bool, len(sources))
	for _, src := range sources {
		available[src.Category] = true
	}

	links := make([]categoryLink, 0, len(navCategories))
	for _, category := range navCategories {
		if category.Value != "" && !available[category.Value] {
			continue
		}
		links = append(links, categoryLink{
			Label:  category.Label,
			Href:   linkWith(r, "category", string(category.Value)),
			Active: category.Value == active,
		})
	}
	return links
}

func buildThemeLinks(r *http.Request, active string) []categoryLink {
	links := make([]categoryLink, 0, len(themes))
	for _, theme := range themes {
		links = append(links, categoryLink{
			Label:  theme.Label,
			Href:   linkWith(r, "theme", theme.Value),
			Active: theme.Value == active,
		})
	}
	return links
}
