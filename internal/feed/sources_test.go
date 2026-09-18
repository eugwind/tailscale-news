package feed_test

import (
	"strings"
	"testing"

	"github.com/eugwind/tailscale-news/internal/feed"
)

func TestSources_AreWellFormed(t *testing.T) {
	t.Parallel()

	sources := feed.Sources()
	if len(sources) == 0 {
		t.Fatal("Sources() is empty, want at least one registered source")
	}

	seenNames := make(map[string]bool, len(sources))
	seenURLs := make(map[string]bool, len(sources))

	for _, src := range sources {
		t.Run(src.Name, func(t *testing.T) {
			if strings.TrimSpace(src.Name) == "" {
				t.Error("Name is empty")
			}
			if seenNames[src.Name] {
				t.Errorf("Name %q is registered more than once", src.Name)
			}
			seenNames[src.Name] = true

			if seenURLs[src.URL] {
				t.Errorf("URL %q is registered more than once", src.URL)
			}
			seenURLs[src.URL] = true

			if !strings.HasPrefix(src.URL, "https://") {
				t.Errorf("URL = %q, want an https:// address", src.URL)
			}
			if _, err := feed.CanonicalURL("", src.URL); err != nil {
				t.Errorf("CanonicalURL(%q) error = %v, want a usable absolute URL", src.URL, err)
			}

			if !feed.ValidCategory(src.Category) {
				t.Errorf("Category = %q, want a recognised category", src.Category)
			}
			if src.PollInterval < 0 {
				t.Errorf("PollInterval = %s, want zero or positive", src.PollInterval)
			}
		})
	}
}

func TestSources_SecurityIsPolledMostOften(t *testing.T) {
	t.Parallel()

	var security, slowest feed.Source
	for _, src := range feed.Sources() {
		if src.Category == feed.CategorySecurity {
			security = src
		}
		if src.PollInterval > slowest.PollInterval {
			slowest = src
		}
	}

	if security.Name == "" {
		t.Fatal("no security source is registered")
	}
	if security.PollInterval == 0 {
		t.Fatal("security source has no explicit poll interval")
	}
	if security.PollInterval >= slowest.PollInterval {
		t.Errorf("security poll interval %s is not shorter than the slowest source %q at %s",
			security.PollInterval, slowest.Name, slowest.PollInterval)
	}
}

func TestSources_ReturnsACopy(t *testing.T) {
	t.Parallel()

	first := feed.Sources()
	original := first[0].Name
	first[0].Name = "mutated"

	if second := feed.Sources(); second[0].Name != original {
		t.Errorf("Sources()[0].Name = %q after caller mutation, want %q", second[0].Name, original)
	}
}
