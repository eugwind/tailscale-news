// Package feed fetches and parses Tailscale news sources and normalises every
// entry into a canonical [Item].
//
// Normalisation is what makes de-duplication possible: the same story published
// on the official blog, mirrored by a community post, and linked from a release
// note must collapse to a single [Item.DedupKey].
package feed

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Category classifies a source and the items it produces.
type Category string

// Categories recognised by the aggregator, ordered roughly by editorial weight.
const (
	CategorySecurity     Category = "security"
	CategoryReleaseNotes Category = "release-notes"
	CategoryOfficial     Category = "official"
	CategoryDevelopment  Category = "development"
	CategoryCommunity    Category = "community"
	CategoryThirdParty   Category = "third-party"
)

// Source describes where items come from.
type Source struct {
	// Name is the human-readable source name, for example "Tailscale blog".
	Name string
	// Category classifies every item produced by this source.
	Category Category
	// URL is the feed address. It is also the base for resolving relative links.
	URL string
	// PollInterval overrides the global poll interval for this source. Zero
	// means the global interval applies.
	PollInterval time.Duration
}

// ValidCategory reports whether c is one of the recognised categories.
func ValidCategory(c Category) bool {
	switch c {
	case CategorySecurity, CategoryReleaseNotes, CategoryOfficial,
		CategoryDevelopment, CategoryCommunity, CategoryThirdParty:
		return true
	default:
		return false
	}
}

// Item is a single normalised news entry.
//
// URL is always absolute and canonicalised, Summary is plain text, and Published
// is in UTC. Published is the zero time when the source omits a usable date.
type Item struct {
	Source    string
	Category  Category
	Title     string
	URL       string
	Summary   string
	Published time.Time
	// GUID is the source-provided identifier, empty when the source omits one.
	GUID string
}

// ID returns a stable identifier for the item within its source. It is derived
// from the source name and the source's own GUID, falling back to the canonical
// URL, so re-polling an unchanged entry always yields the same ID.
func (i Item) ID() string {
	identity := i.GUID
	if identity == "" {
		identity = i.URL
	}
	return digest(i.Source, identity)
}

// DedupKey returns a key that is equal for the same story published by different
// sources. It combines the canonical URL with the normalised title so that a
// syndicated copy pointing at the same article collapses into one entry.
func (i Item) DedupKey() string {
	return digest(i.URL, normalizeTitle(i.Title))
}

// HasDate reports whether the source supplied a usable publication date.
func (i Item) HasDate() bool { return !i.Published.IsZero() }

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}

// trackingParams are query parameters that identify the referrer rather than the
// content, and so must not affect the canonical URL.
var trackingParams = map[string]bool{
	"fbclid":  true,
	"gclid":   true,
	"igshid":  true,
	"mc_cid":  true,
	"mc_eid":  true,
	"ref":     true,
	"ref_src": true,
}

// CanonicalURL resolves raw against base and reduces it to a comparable form:
// lower-case scheme and host, no default port, no tracking parameters, sorted
// query, and no trailing slash outside the site root.
//
// Fragments are preserved. Anchor-based feeds such as the Tailscale changelog
// give every entry the same path and distinguish entries only by fragment, so
// dropping it would collapse the whole feed onto a single URL.
func CanonicalURL(base, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrNoURL
	}

	ref, err := url.Parse(raw)
	if err != nil {
		return "", ErrNoURL
	}

	if !ref.IsAbs() && base != "" {
		baseURL, err := url.Parse(base)
		if err == nil {
			ref = baseURL.ResolveReference(ref)
		}
	}
	if !ref.IsAbs() {
		return "", ErrNoURL
	}

	ref.Scheme = strings.ToLower(ref.Scheme)
	if ref.Scheme != "http" && ref.Scheme != "https" {
		return "", ErrNoURL
	}

	ref.Host = strings.ToLower(ref.Host)
	if (ref.Scheme == "http" && strings.HasSuffix(ref.Host, ":80")) ||
		(ref.Scheme == "https" && strings.HasSuffix(ref.Host, ":443")) {
		ref.Host = ref.Host[:strings.LastIndex(ref.Host, ":")]
	}

	query := ref.Query()
	for key := range query {
		if trackingParams[strings.ToLower(key)] || strings.HasPrefix(strings.ToLower(key), "utm_") {
			query.Del(key)
		}
	}
	if len(query) == 0 {
		ref.RawQuery = ""
	} else {
		keys := make([]string, 0, len(query))
		for key := range query {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, key := range keys {
			values := query[key]
			sort.Strings(values)
			for _, value := range values {
				if sb.Len() > 0 {
					sb.WriteByte('&')
				}
				sb.WriteString(url.QueryEscape(key))
				sb.WriteByte('=')
				sb.WriteString(url.QueryEscape(value))
			}
		}
		ref.RawQuery = sb.String()
	}

	if len(ref.Path) > 1 {
		ref.Path = strings.TrimSuffix(ref.Path, "/")
	}

	return ref.String(), nil
}

// normalizeTitle reduces a title to a comparable form: plain text, folded case,
// collapsed whitespace, and no surrounding punctuation.
func normalizeTitle(title string) string {
	folded := strings.ToLower(plainText(title))
	return strings.TrimFunc(folded, func(r rune) bool {
		return unicode.IsPunct(r) || unicode.IsSpace(r)
	})
}

// plainText strips HTML markup, resolves entities, and collapses whitespace.
func plainText(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))

	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
			sb.WriteByte(' ')
		case depth == 0:
			sb.WriteRune(r)
		}
	}

	return strings.Join(strings.Fields(html.UnescapeString(sb.String())), " ")
}

// timeLayouts covers the formats real feeds use, in rough order of frequency.
var timeLayouts = []string{
	time.RFC3339,
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	time.RFC822Z,
	time.RFC822,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseTime converts a feed timestamp to UTC. It returns the zero time when the
// value is missing or unparseable — a missing date is normal in the wild and
// must not discard an otherwise usable item.
func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
