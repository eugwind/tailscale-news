// Package store keeps normalised news items in memory, collapsing the same
// story from several sources into a single record.
//
// Storage is deliberately in-memory: items are re-fetched after a restart, and
// no database decision is forced before the access patterns are known.
package store

import (
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/eugwind/tailscale-news/internal/feed"
)

// DefaultCapacity bounds memory use when no capacity is configured.
const DefaultCapacity = 5000

// Record is a stored story: the winning item plus the de-duplication history
// that produced it.
type Record struct {
	feed.Item
	// FirstSeen is when this store first observed the story.
	FirstSeen time.Time `json:"first_seen"`
	// Sources lists every source that carried the story, in the order seen.
	Sources []string `json:"sources"`
}

// Effective returns the time used for ordering: the published date when the
// source supplied one, and otherwise the time the story was first seen.
func (r Record) Effective() time.Time {
	if r.Published.IsZero() {
		return r.FirstSeen
	}
	return r.Published
}

// Stats summarises what one [Memory.Put] changed.
type Stats struct {
	// Added counts stories that were not previously stored.
	Added int
	// Updated counts stored stories whose content was replaced.
	Updated int
	// Duplicates counts items that matched a stored story and did not win.
	Duplicates int
	// Evicted counts stories dropped to stay within capacity.
	Evicted int
}

// Filter narrows a [Memory.List] query. The zero value matches everything.
type Filter struct {
	Category feed.Category
	Source   string
	Since    time.Time
	Limit    int
}

// Memory is a concurrency-safe in-memory store keyed by [feed.Item.DedupKey].
type Memory struct {
	capacity int
	now      func() time.Time

	mu      sync.RWMutex
	records map[string]*Record
}

// NewMemory returns a store holding at most capacity stories. A capacity below
// one falls back to [DefaultCapacity].
func NewMemory(capacity int) *Memory {
	if capacity < 1 {
		capacity = DefaultCapacity
	}
	return &Memory{
		capacity: capacity,
		now:      time.Now,
		records:  make(map[string]*Record),
	}
}

// categoryRank orders categories by editorial weight, lowest first. When two
// sources carry the same story, the better-ranked category wins, so a security
// bulletin is never displaced by a community repost of the same URL.
func categoryRank(c feed.Category) int {
	switch c {
	case feed.CategorySecurity:
		return 0
	case feed.CategoryReleaseNotes:
		return 1
	case feed.CategoryOfficial:
		return 2
	case feed.CategoryDevelopment:
		return 3
	case feed.CategoryCommunity:
		return 4
	case feed.CategoryThirdParty:
		return 5
	default:
		return 6
	}
}

// Put merges items into the store and reports what changed.
//
// Items sharing a dedup key collapse into one record. The stored item is
// replaced when the incoming one comes from the same source (a re-poll carries
// the freshest content), when its category outranks the stored one, or when it
// supplies a publication date the stored item lacks.
func (m *Memory) Put(items []feed.Item) Stats {
	if len(items) == 0 {
		return Stats{}
	}

	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	var stats Stats
	for _, item := range items {
		key := item.DedupKey()

		existing, ok := m.records[key]
		if !ok {
			m.records[key] = &Record{Item: item, FirstSeen: now, Sources: []string{item.Source}}
			stats.Added++
			continue
		}

		if !slices.Contains(existing.Sources, item.Source) {
			existing.Sources = append(existing.Sources, item.Source)
		}

		if supersedes(item, existing.Item) {
			existing.Item = item
			stats.Updated++
			continue
		}
		stats.Duplicates++
	}

	stats.Evicted = m.evictLocked()
	return stats
}

// supersedes reports whether incoming should replace stored.
func supersedes(incoming, stored feed.Item) bool {
	if incoming.Source == stored.Source {
		return true
	}
	if rank, storedRank := categoryRank(incoming.Category), categoryRank(stored.Category); rank != storedRank {
		return rank < storedRank
	}
	return !incoming.Published.IsZero() && stored.Published.IsZero()
}

// evictLocked drops the oldest stories until the store fits its capacity. The
// caller must hold the write lock.
func (m *Memory) evictLocked() int {
	excess := len(m.records) - m.capacity
	if excess <= 0 {
		return 0
	}

	type keyed struct {
		key string
		at  time.Time
	}
	all := make([]keyed, 0, len(m.records))
	for key, record := range m.records {
		all = append(all, keyed{key: key, at: record.Effective()})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })

	for _, victim := range all[:excess] {
		delete(m.records, victim.key)
	}
	return excess
}

// List returns matching stories newest first, using the published date when the
// source supplied one and the first-seen time otherwise.
func (m *Memory) List(f Filter) []Record {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Record, 0, len(m.records))
	for _, record := range m.records {
		if f.Category != "" && record.Category != f.Category {
			continue
		}
		if f.Source != "" && !slices.Contains(record.Sources, f.Source) {
			continue
		}
		if !f.Since.IsZero() && !record.Effective().After(f.Since) {
			continue
		}

		copied := *record
		copied.Sources = append([]string(nil), record.Sources...)
		out = append(out, copied)
	}

	sort.Slice(out, func(i, j int) bool {
		left, right := out[i].Effective(), out[j].Effective()
		if left.Equal(right) {
			return out[i].Title < out[j].Title
		}
		return left.After(right)
	})

	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}

// Len returns the number of stored stories.
func (m *Memory) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.records)
}
