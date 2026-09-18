package store_test

import (
	"sync"
	"testing"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
	"github.com/eugwind/tailscale-news/internal/store"
)

func item(source string, category feed.Category, title, url string, published time.Time) feed.Item {
	return feed.Item{
		Source:    source,
		Category:  category,
		Title:     title,
		URL:       url,
		Published: published,
	}
}

var (
	sept12 = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	sept15 = time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	sept17 = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
)

func TestPut_NewItems_AreStored(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(10)
	stats := s.Put([]feed.Item{
		item("blog", feed.CategoryOfficial, "First", "https://example.test/1", sept12),
		item("blog", feed.CategoryOfficial, "Second", "https://example.test/2", sept15),
	})

	if stats.Added != 2 || stats.Updated != 0 || stats.Duplicates != 0 {
		t.Errorf("stats = %+v, want 2 added and nothing else", stats)
	}
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2", s.Len())
	}
}

func TestPut_SameStoryFromTwoSources_CollapsesToOneRecord(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(10)
	s.Put([]feed.Item{item("community", feed.CategoryCommunity, "Subnet failover", "https://example.test/failover", sept12)})
	stats := s.Put([]feed.Item{item("blog", feed.CategoryOfficial, "Subnet failover", "https://example.test/failover", sept12)})

	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 — the same story must collapse", s.Len())
	}
	if stats.Added != 0 || stats.Updated != 1 {
		t.Errorf("stats = %+v, want the official version to supersede the community one", stats)
	}

	got := s.List(store.Filter{})[0]
	if got.Source != "blog" || got.Category != feed.CategoryOfficial {
		t.Errorf("winning item = %s/%s, want blog/official", got.Source, got.Category)
	}
	if len(got.Sources) != 2 {
		t.Errorf("Sources = %v, want both sources recorded", got.Sources)
	}
}

func TestPut_CategoryPrecedence(t *testing.T) {
	t.Parallel()

	const url = "https://example.test/story"

	tests := []struct {
		name       string
		first      feed.Item
		second     feed.Item
		wantSource string
	}{
		{
			name:       "security beats official",
			first:      item("blog", feed.CategoryOfficial, "Story", url, sept12),
			second:     item("bulletins", feed.CategorySecurity, "Story", url, sept12),
			wantSource: "bulletins",
		},
		{
			name:       "official is not displaced by community",
			first:      item("blog", feed.CategoryOfficial, "Story", url, sept12),
			second:     item("forum", feed.CategoryCommunity, "Story", url, sept12),
			wantSource: "blog",
		},
		{
			name:       "release notes beat development",
			first:      item("dev", feed.CategoryDevelopment, "Story", url, sept12),
			second:     item("changelog", feed.CategoryReleaseNotes, "Story", url, sept12),
			wantSource: "changelog",
		},
		{
			name:       "security is not displaced by release notes",
			first:      item("bulletins", feed.CategorySecurity, "Story", url, sept12),
			second:     item("changelog", feed.CategoryReleaseNotes, "Story", url, sept12),
			wantSource: "bulletins",
		},
		{
			name:       "same rank, a dated item beats an undated one",
			first:      item("mirror", feed.CategoryOfficial, "Story", url, time.Time{}),
			second:     item("blog", feed.CategoryOfficial, "Story", url, sept12),
			wantSource: "blog",
		},
		{
			name:       "same rank, an undated item does not displace a dated one",
			first:      item("blog", feed.CategoryOfficial, "Story", url, sept12),
			second:     item("mirror", feed.CategoryOfficial, "Story", url, time.Time{}),
			wantSource: "blog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := store.NewMemory(10)
			s.Put([]feed.Item{tt.first})
			s.Put([]feed.Item{tt.second})

			records := s.List(store.Filter{})
			if len(records) != 1 {
				t.Fatalf("Len() = %d, want 1", len(records))
			}
			if records[0].Source != tt.wantSource {
				t.Errorf("winning source = %q, want %q", records[0].Source, tt.wantSource)
			}
		})
	}
}

func TestPut_RepollFromSameSource_RefreshesContent(t *testing.T) {
	t.Parallel()

	const url = "https://example.test/story"

	s := store.NewMemory(10)
	first := item("blog", feed.CategoryOfficial, "Story", url, sept12)
	first.Summary = "original summary"
	s.Put([]feed.Item{first})

	second := item("blog", feed.CategoryOfficial, "Story", url, sept12)
	second.Summary = "corrected summary"
	stats := s.Put([]feed.Item{second})

	if stats.Updated != 1 {
		t.Errorf("stats = %+v, want the re-poll to update the record", stats)
	}
	if got := s.List(store.Filter{})[0].Summary; got != "corrected summary" {
		t.Errorf("Summary = %q, want the refreshed text", got)
	}
	if s.Len() != 1 {
		t.Errorf("Len() = %d, want 1", s.Len())
	}
	if sources := s.List(store.Filter{})[0].Sources; len(sources) != 1 {
		t.Errorf("Sources = %v, want the source recorded once", sources)
	}
}

func TestPut_IsIdempotent(t *testing.T) {
	t.Parallel()

	items := []feed.Item{
		item("blog", feed.CategoryOfficial, "One", "https://example.test/1", sept12),
		item("blog", feed.CategoryOfficial, "Two", "https://example.test/2", sept15),
	}

	s := store.NewMemory(10)
	s.Put(items)
	s.Put(items)
	s.Put(items)

	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2 after three identical polls", s.Len())
	}
}

func TestList_OrdersNewestFirst(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(10)
	s.Put([]feed.Item{
		item("blog", feed.CategoryOfficial, "Middle", "https://example.test/2", sept15),
		item("blog", feed.CategoryOfficial, "Oldest", "https://example.test/1", sept12),
		item("blog", feed.CategoryOfficial, "Newest", "https://example.test/3", sept17),
	})

	got := s.List(store.Filter{})
	want := []string{"Newest", "Middle", "Oldest"}
	for i, title := range want {
		if got[i].Title != title {
			t.Errorf("List()[%d].Title = %q, want %q", i, got[i].Title, title)
		}
	}
}

func TestList_UndatedItemsUseFirstSeen(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(10)
	s.Put([]feed.Item{
		item("blog", feed.CategoryOfficial, "Undated", "https://example.test/undated", time.Time{}),
		item("blog", feed.CategoryOfficial, "Old but dated", "https://example.test/dated", sept12),
	})

	got := s.List(store.Filter{})
	if got[0].Title != "Undated" {
		t.Errorf("List()[0].Title = %q, want the undated item first (first-seen is now)", got[0].Title)
	}
	if got[0].FirstSeen.IsZero() {
		t.Error("FirstSeen is zero for a stored record")
	}
}

func TestList_Filters(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(10)
	s.Put([]feed.Item{
		item("blog", feed.CategoryOfficial, "Official", "https://example.test/1", sept12),
		item("bulletins", feed.CategorySecurity, "Security", "https://example.test/2", sept15),
		item("changelog", feed.CategoryReleaseNotes, "Release", "https://example.test/3", sept17),
	})

	tests := []struct {
		name   string
		filter store.Filter
		want   []string
	}{
		{"no filter", store.Filter{}, []string{"Release", "Security", "Official"}},
		{"by category", store.Filter{Category: feed.CategorySecurity}, []string{"Security"}},
		{"by source", store.Filter{Source: "changelog"}, []string{"Release"}},
		{"by since", store.Filter{Since: sept12}, []string{"Release", "Security"}},
		{"limit", store.Filter{Limit: 2}, []string{"Release", "Security"}},
		{"limit larger than result", store.Filter{Limit: 99}, []string{"Release", "Security", "Official"}},
		{"no match", store.Filter{Category: feed.CategoryThirdParty}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := s.List(tt.filter)
			if len(got) != len(tt.want) {
				t.Fatalf("List() returned %d records, want %d", len(got), len(tt.want))
			}
			for i, title := range tt.want {
				if got[i].Title != title {
					t.Errorf("List()[%d].Title = %q, want %q", i, got[i].Title, title)
				}
			}
		})
	}
}

func TestPut_OverCapacity_EvictsOldest(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(2)
	stats := s.Put([]feed.Item{
		item("blog", feed.CategoryOfficial, "Oldest", "https://example.test/1", sept12),
		item("blog", feed.CategoryOfficial, "Middle", "https://example.test/2", sept15),
		item("blog", feed.CategoryOfficial, "Newest", "https://example.test/3", sept17),
	})

	if stats.Evicted != 1 {
		t.Errorf("Evicted = %d, want 1", stats.Evicted)
	}
	if s.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", s.Len())
	}

	got := s.List(store.Filter{})
	for _, record := range got {
		if record.Title == "Oldest" {
			t.Error("the oldest story survived eviction")
		}
	}
}

func TestList_ReturnsCopies(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(10)
	s.Put([]feed.Item{item("blog", feed.CategoryOfficial, "Story", "https://example.test/1", sept12)})

	first := s.List(store.Filter{})
	first[0].Title = "mutated"
	first[0].Sources[0] = "mutated"

	second := s.List(store.Filter{})
	if second[0].Title != "Story" || second[0].Sources[0] != "blog" {
		t.Errorf("stored record was mutated through a List() result: %+v", second[0])
	}
}

func TestPut_EmptyInput_IsNoop(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(10)
	if stats := s.Put(nil); stats != (store.Stats{}) {
		t.Errorf("Put(nil) = %+v, want the zero Stats", stats)
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
}

func TestMemory_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := store.NewMemory(1000)

	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 50 {
				url := "https://example.test/" + string(rune('a'+worker)) + string(rune('a'+i%26))
				s.Put([]feed.Item{item("blog", feed.CategoryOfficial, "Story", url, sept12)})
				s.List(store.Filter{Limit: 10})
				s.Len()
			}
		}()
	}
	wg.Wait()

	if s.Len() == 0 {
		t.Error("Len() = 0 after concurrent writes")
	}
}
