package feed_test

import (
	"errors"
	"testing"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
)

func TestCanonicalURL(t *testing.T) {
	t.Parallel()

	const base = "https://example.test/blog/index.xml"

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"already canonical", "https://example.test/blog/post", "https://example.test/blog/post"},
		{"uppercase scheme and host", "HTTPS://EXAMPLE.TEST/Blog/Post", "https://example.test/Blog/Post"},
		{"default https port", "https://example.test:443/blog/post", "https://example.test/blog/post"},
		{"default http port", "http://example.test:80/blog/post", "http://example.test/blog/post"},
		{"non-default port kept", "https://example.test:8443/blog/post", "https://example.test:8443/blog/post"},
		{"fragment kept", "https://example.test/blog/post#intro", "https://example.test/blog/post#intro"},
		{"trailing slash removed", "https://example.test/blog/post/", "https://example.test/blog/post"},
		{
			name: "anchor entry keeps its fragment",
			raw:  "https://example.test/changelog/#2026-09-17-service",
			want: "https://example.test/changelog#2026-09-17-service",
		},
		{"root slash kept", "https://example.test/", "https://example.test/"},
		{"relative path resolved", "/blog/post", "https://example.test/blog/post"},
		{"relative sibling resolved", "post", "https://example.test/blog/post"},
		{
			name: "utm parameters stripped",
			raw:  "https://example.test/p?utm_source=rss&utm_campaign=x",
			want: "https://example.test/p",
		},
		{
			name: "known trackers stripped",
			raw:  "https://example.test/p?fbclid=a&gclid=b&ref=c&ref_src=d&mc_cid=e",
			want: "https://example.test/p",
		},
		{
			name: "meaningful query kept and sorted",
			raw:  "https://example.test/search?q=tailscale&page=2",
			want: "https://example.test/search?page=2&q=tailscale",
		},
		{
			name: "mixed query keeps only meaningful parameters",
			raw:  "https://example.test/search?utm_source=rss&q=acl",
			want: "https://example.test/search?q=acl",
		},
		{"whitespace trimmed", "  https://example.test/p  ", "https://example.test/p"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := feed.CanonicalURL(base, tt.raw)
			if err != nil {
				t.Fatalf("CanonicalURL(%q) error = %v, want nil", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("CanonicalURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCanonicalURL_Unusable_ReturnsErrNoURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		base string
		raw  string
	}{
		{"empty", "https://example.test/blog/", ""},
		{"whitespace only", "https://example.test/blog/", "   "},
		{"relative with no base", "", "/blog/post"},
		{"javascript scheme", "https://example.test/blog/", "javascript:alert(1)"},
		{"data scheme", "https://example.test/blog/", "data:text/html,<script>x</script>"},
		{"mailto scheme", "https://example.test/blog/", "mailto:someone@example.test"},
		{"file scheme", "https://example.test/blog/", "file:///etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := feed.CanonicalURL(tt.base, tt.raw)
			if !errors.Is(err, feed.ErrNoURL) {
				t.Fatalf("CanonicalURL(%q) error = %v, want %v", tt.raw, err, feed.ErrNoURL)
			}
			if got != "" {
				t.Errorf("CanonicalURL(%q) = %q, want empty string on error", tt.raw, got)
			}
		})
	}
}

func TestItem_DedupKey_CollapsesTheSameStory(t *testing.T) {
	t.Parallel()

	official := feed.Item{
		Source:   "Example blog",
		Category: feed.CategoryOfficial,
		Title:    "Subnet routers now support automatic failover",
		URL:      "https://example.test/blog/failover",
	}

	tests := []struct {
		name  string
		other feed.Item
		equal bool
	}{
		{
			name: "same story from a community source",
			other: feed.Item{
				Source:   "Community forum",
				Category: feed.CategoryCommunity,
				Title:    "Subnet routers now support automatic failover",
				URL:      "https://example.test/blog/failover",
			},
			equal: true,
		},
		{
			name: "title differing only by case and punctuation",
			other: feed.Item{
				Source: "Mirror",
				Title:  "  SUBNET ROUTERS NOW SUPPORT AUTOMATIC FAILOVER!  ",
				URL:    "https://example.test/blog/failover",
			},
			equal: true,
		},
		{
			name: "title with markup and entities",
			other: feed.Item{
				Source: "Mirror",
				Title:  "<b>Subnet routers</b> now support&#32;automatic failover",
				URL:    "https://example.test/blog/failover",
			},
			equal: true,
		},
		{
			name: "different article on the same site",
			other: feed.Item{
				Source: "Example blog",
				Title:  "Subnet routers now support automatic failover",
				URL:    "https://example.test/blog/other",
			},
			equal: false,
		},
		{
			name: "same url, genuinely different story",
			other: feed.Item{
				Source: "Example blog",
				Title:  "ACLs get a shorter grammar",
				URL:    "https://example.test/blog/failover",
			},
			equal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := official.DedupKey() == tt.other.DedupKey()
			if got != tt.equal {
				t.Errorf("DedupKey() equality = %t, want %t", got, tt.equal)
			}
		})
	}
}

func TestItem_ID_IsPerSourceAndStable(t *testing.T) {
	t.Parallel()

	base := feed.Item{
		Source:    "Example blog",
		Title:     "A post",
		URL:       "https://example.test/blog/post",
		GUID:      "blog-123",
		Published: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
	}

	t.Run("stable across repeated calls", func(t *testing.T) {
		t.Parallel()
		if base.ID() != base.ID() {
			t.Error("ID() is not stable")
		}
	})

	t.Run("unaffected by volatile fields", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Summary = "an edited summary"
		other.Published = base.Published.Add(time.Hour)
		if other.ID() != base.ID() {
			t.Error("ID() changed when only summary and date changed")
		}
	})

	t.Run("differs per source", func(t *testing.T) {
		t.Parallel()
		other := base
		other.Source = "Mirror"
		if other.ID() == base.ID() {
			t.Error("ID() is identical across two different sources")
		}
	})

	t.Run("falls back to url when guid is absent", func(t *testing.T) {
		t.Parallel()
		withoutGUID := base
		withoutGUID.GUID = ""
		if withoutGUID.ID() == "" {
			t.Error("ID() = empty string, want a digest derived from the URL")
		}
		if withoutGUID.ID() == base.ID() {
			t.Error("ID() ignored the GUID when one was present")
		}
	})
}
