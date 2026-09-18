package feed

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Parse errors. Callers distinguish them with [errors.Is].
var (
	// ErrUnsupportedFormat means the document is not RSS 2.0 or Atom.
	ErrUnsupportedFormat = errors.New("unsupported feed format")
	// ErrNoURL means an entry had no resolvable absolute link.
	ErrNoURL = errors.New("no usable url")
)

// Parse reads an RSS 2.0 or Atom document and returns normalised items.
//
// Entries without a resolvable link are skipped rather than failing the whole
// feed, since a single malformed entry must not cost a source its other items.
// An empty feed is not an error.
func Parse(r io.Reader, src Source) ([]Item, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read feed %s: %w", src.Name, err)
	}

	root, err := rootElement(raw)
	if err != nil {
		return nil, fmt.Errorf("parse feed %s: %w", src.Name, err)
	}

	switch root {
	case "rss":
		return parseRSS(raw, src)
	case "feed":
		return parseAtom(raw, src)
	default:
		return nil, fmt.Errorf("parse feed %s: root element %q: %w", src.Name, root, ErrUnsupportedFormat)
	}
}

func rootElement(raw []byte) (string, error) {
	decoder := newDecoder(raw)
	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrUnsupportedFormat
			}
			return "", err
		}
		if start, ok := token.(xml.StartElement); ok {
			return strings.ToLower(start.Name.Local), nil
		}
	}
}

func newDecoder(raw []byte) *xml.Decoder {
	decoder := xml.NewDecoder(strings.NewReader(string(raw)))
	decoder.Strict = false
	decoder.CharsetReader = charsetReader
	return decoder
}

// charsetReader handles the single-byte encodings still found in older feeds
// without pulling in a dependency. Anything else is rejected.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "utf-8", "utf8", "us-ascii", "ascii", "":
		return input, nil
	case "iso-8859-1", "latin1", "iso8859-1", "windows-1252", "cp1252":
		return &latin1Reader{source: input}, nil
	default:
		return nil, fmt.Errorf("charset %q: %w", charset, ErrUnsupportedFormat)
	}
}

// latin1Reader widens each input byte to the matching rune and re-encodes it as
// UTF-8. Windows-1252 differs from Latin-1 only in the 0x80–0x9F range, which
// carries punctuation that is acceptable to approximate.
type latin1Reader struct {
	source  io.Reader
	pending []byte
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	if len(l.pending) > 0 {
		n := copy(p, l.pending)
		l.pending = l.pending[n:]
		return n, nil
	}

	buf := make([]byte, len(p))
	n, err := l.source.Read(buf)
	if n > 0 {
		encoded := []byte(string([]rune(bytesToRunes(buf[:n]))))
		copied := copy(p, encoded)
		l.pending = append(l.pending[:0], encoded[copied:]...)
		return copied, nil
	}
	return 0, err
}

func bytesToRunes(b []byte) []rune {
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return runes
}

type rssDocument struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	Encoded     string `xml:"encoded"`
	PubDate     string `xml:"pubDate"`
	Date        string `xml:"date"`
	GUID        struct {
		Value       string `xml:",chardata"`
		IsPermaLink string `xml:"isPermaLink,attr"`
	} `xml:"guid"`
}

func parseRSS(raw []byte, src Source) ([]Item, error) {
	var doc rssDocument
	if err := newDecoder(raw).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse rss %s: %w", src.Name, err)
	}

	items := make([]Item, 0, len(doc.Channel.Items))
	for _, entry := range doc.Channel.Items {
		link := entry.Link
		if link == "" && !strings.EqualFold(entry.GUID.IsPermaLink, "false") {
			link = entry.GUID.Value
		}

		canonical, err := CanonicalURL(src.URL, link)
		if err != nil {
			continue
		}

		summary := entry.Description
		if summary == "" {
			summary = entry.Encoded
		}

		published := parseTime(entry.PubDate)
		if published.IsZero() {
			published = parseTime(entry.Date)
		}

		items = append(items, Item{
			Source:    src.Name,
			Category:  src.Category,
			Title:     plainText(entry.Title),
			URL:       canonical,
			Summary:   plainText(summary),
			Published: published,
			GUID:      strings.TrimSpace(entry.GUID.Value),
		})
	}

	return items, nil
}

type atomDocument struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title   string `xml:"title"`
	ID      string `xml:"id"`
	Updated string `xml:"updated"`
	Publish string `xml:"published"`
	Summary string `xml:"summary"`
	Content string `xml:"content"`
	Links   []struct {
		Href string `xml:"href,attr"`
		Rel  string `xml:"rel,attr"`
		Type string `xml:"type,attr"`
	} `xml:"link"`
}

func parseAtom(raw []byte, src Source) ([]Item, error) {
	var doc atomDocument
	if err := newDecoder(raw).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse atom %s: %w", src.Name, err)
	}

	items := make([]Item, 0, len(doc.Entries))
	for _, entry := range doc.Entries {
		canonical, err := CanonicalURL(src.URL, atomLink(entry))
		if err != nil {
			continue
		}

		summary := entry.Summary
		if summary == "" {
			summary = entry.Content
		}

		published := parseTime(entry.Publish)
		if published.IsZero() {
			published = parseTime(entry.Updated)
		}

		items = append(items, Item{
			Source:    src.Name,
			Category:  src.Category,
			Title:     plainText(entry.Title),
			URL:       canonical,
			Summary:   plainText(summary),
			Published: published,
			GUID:      strings.TrimSpace(entry.ID),
		})
	}

	return items, nil
}

// atomLink picks the entry's readable link: an explicit alternate first, then
// any HTML link, then the first link of any kind.
func atomLink(entry atomEntry) string {
	var htmlLink, fallback string
	for _, link := range entry.Links {
		if link.Href == "" {
			continue
		}
		switch {
		case link.Rel == "alternate" || link.Rel == "":
			return link.Href
		case link.Type == "text/html" && htmlLink == "":
			htmlLink = link.Href
		case fallback == "":
			fallback = link.Href
		}
	}
	if htmlLink != "" {
		return htmlLink
	}
	return fallback
}
