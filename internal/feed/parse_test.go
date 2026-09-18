package feed_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
)

func fixture(t *testing.T, name string) *strings.Reader {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.NewReader(string(raw))
}

var blogSource = feed.Source{
	Name:     "Example blog",
	Category: feed.CategoryOfficial,
	URL:      "https://example.test/blog/index.xml",
}

var releasesSource = feed.Source{
	Name:     "Example releases",
	Category: feed.CategoryReleaseNotes,
	URL:      "https://example.test/example/example/releases.atom",
}

func TestParse_RSS_NormalisesEveryField(t *testing.T) {
	t.Parallel()

	items, err := feed.Parse(fixture(t, "blog.rss.xml"), blogSource)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if len(items) != 3 {
		t.Fatalf("Parse() returned %d items, want 3 (the link-less entry is skipped)", len(items))
	}

	tests := []struct {
		name          string
		index         int
		wantTitle     string
		wantURL       string
		wantSummary   string
		wantGUID      string
		wantPublished time.Time
	}{
		{
			name:          "tracking parameters are stripped",
			index:         0,
			wantTitle:     "Subnet routers now support automatic failover",
			wantURL:       "https://example.test/blog/subnet-router-failover",
			wantSummary:   "Two routers advertising the same route now fail over automatically.",
			wantGUID:      "blog-2026-0912-failover",
			wantPublished: time.Date(2026, 9, 12, 14, 30, 0, 0, time.UTC),
		},
		{
			name:          "relative link is resolved and CDATA unwrapped",
			index:         1,
			wantTitle:     "ACLs: a shorter grammar",
			wantURL:       "https://example.test/blog/acl-grammar",
			wantSummary:   "The policy file gains a compact form.",
			wantGUID:      "blog-2026-0903-acl",
			wantPublished: time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC),
		},
		{
			name:          "case and default port canonicalised, fragment kept",
			index:         2,
			wantTitle:     "Undated field note",
			wantURL:       "https://example.test/blog/field-note#section-2",
			wantSummary:   "No publication date is supplied by this entry.",
			wantGUID:      "",
			wantPublished: time.Time{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := items[tt.index]

			if got.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", got.Title, tt.wantTitle)
			}
			if got.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tt.wantURL)
			}
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if got.GUID != tt.wantGUID {
				t.Errorf("GUID = %q, want %q", got.GUID, tt.wantGUID)
			}
			if !got.Published.Equal(tt.wantPublished) {
				t.Errorf("Published = %s, want %s", got.Published, tt.wantPublished)
			}
			if got.Published.Location() != time.UTC {
				t.Errorf("Published location = %s, want UTC", got.Published.Location())
			}
			if got.Source != blogSource.Name || got.Category != blogSource.Category {
				t.Errorf("Source/Category = %q/%q, want %q/%q",
					got.Source, got.Category, blogSource.Name, blogSource.Category)
			}
			if got.HasDate() != !tt.wantPublished.IsZero() {
				t.Errorf("HasDate() = %t, want %t", got.HasDate(), !tt.wantPublished.IsZero())
			}
		})
	}
}

func TestParse_Atom_PrefersAlternateLink(t *testing.T) {
	t.Parallel()

	items, err := feed.Parse(fixture(t, "releases.atom.xml"), releasesSource)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if len(items) != 2 {
		t.Fatalf("Parse() returned %d items, want 2 (the link-less entry is skipped)", len(items))
	}

	first := items[0]
	if want := "https://example.test/example/example/releases/tag/v1.84.0"; first.URL != want {
		t.Errorf("URL = %q, want %q", first.URL, want)
	}
	if want := time.Date(2026, 9, 15, 9, 45, 0, 0, time.UTC); !first.Published.Equal(want) {
		t.Errorf("Published = %s, want %s (published wins over updated)", first.Published, want)
	}
	if want := "Changes Faster DERP handshakes"; first.Summary != want {
		t.Errorf("Summary = %q, want %q", first.Summary, want)
	}

	second := items[1]
	if want := "https://example.test/example/example/releases/tag/v1.83.2"; second.URL != want {
		t.Errorf("URL = %q, want %q (self link must not win)", second.URL, want)
	}
	if want := "v1.83.2 — security fix"; second.Title != want {
		t.Errorf("Title = %q, want %q", second.Title, want)
	}
	if want := time.Date(2026, 8, 28, 17, 12, 0, 0, time.UTC); !second.Published.Equal(want) {
		t.Errorf("Published = %s, want %s (falls back to updated)", second.Published, want)
	}
}

func TestParse_AnchorBasedFeed_KeepsEntriesDistinct(t *testing.T) {
	t.Parallel()

	src := feed.Source{
		Name:     "Example changelog",
		Category: feed.CategoryReleaseNotes,
		URL:      "https://example.test/changelog/index.xml",
	}

	items, err := feed.Parse(fixture(t, "changelog.rss.xml"), src)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if len(items) != 3 {
		t.Fatalf("Parse() returned %d items, want 3", len(items))
	}

	urls := make(map[string]bool, len(items))
	keys := make(map[string]bool, len(items))
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		urls[item.URL] = true
		keys[item.DedupKey()] = true
		ids[item.ID()] = true
	}

	if len(urls) != len(items) {
		t.Errorf("got %d distinct URLs for %d items — the fragment that identifies each entry was dropped", len(urls), len(items))
	}
	if len(keys) != len(items) {
		t.Errorf("got %d distinct dedup keys for %d items, want one per entry", len(keys), len(items))
	}
	if len(ids) != len(items) {
		t.Errorf("got %d distinct IDs for %d items, want one per entry", len(ids), len(items))
	}

	if want := "https://example.test/changelog#2026-09-17-service"; items[0].URL != want {
		t.Errorf("URL = %q, want %q", items[0].URL, want)
	}
}

func TestParse_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fixture   string
		wantItems int
		wantErr   error
	}{
		{"empty feed is not an error", "empty.rss.xml", 0, nil},
		{"malformed xml", "malformed.xml", 0, nil},
		{"html page is unsupported", "notafeed.html", 0, feed.ErrUnsupportedFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			items, err := feed.Parse(fixture(t, tt.fixture), blogSource)

			switch {
			case tt.wantErr != nil:
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Parse() error = %v, want %v", err, tt.wantErr)
				}
			case tt.name == "malformed xml":
				if err == nil {
					t.Fatal("Parse() error = nil, want a parse error")
				}
			default:
				if err != nil {
					t.Fatalf("Parse() error = %v, want nil", err)
				}
			}

			if len(items) != tt.wantItems {
				t.Errorf("Parse() returned %d items, want %d", len(items), tt.wantItems)
			}
		})
	}
}

func TestParse_EmptyInput_ReturnsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	_, err := feed.Parse(strings.NewReader(""), blogSource)
	if !errors.Is(err, feed.ErrUnsupportedFormat) {
		t.Fatalf("Parse() error = %v, want %v", err, feed.ErrUnsupportedFormat)
	}
}

func TestParse_IsDeterministic(t *testing.T) {
	t.Parallel()

	first, err := feed.Parse(fixture(t, "blog.rss.xml"), blogSource)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	second, err := feed.Parse(fixture(t, "blog.rss.xml"), blogSource)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for i := range first {
		if first[i].ID() != second[i].ID() {
			t.Errorf("item %d: ID() is not stable across parses", i)
		}
		if first[i].DedupKey() != second[i].DedupKey() {
			t.Errorf("item %d: DedupKey() is not stable across parses", i)
		}
	}
}
