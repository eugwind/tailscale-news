package feed

import "time"

// registeredSources is the built-in source list. Sources live in code rather
// than a config file so that a typo fails the build instead of a poll.
//
// Every URL below was probed with
// .github/skills/feed-health-check/scripts/check-feeds.sh on 2026-09-18.
var registeredSources = []Source{
	{
		Name:         "Tailscale blog",
		Category:     CategoryOfficial,
		URL:          "https://tailscale.com/blog/index.xml",
		PollInterval: 30 * time.Minute,
	},
	{
		Name:         "Tailscale changelog",
		Category:     CategoryReleaseNotes,
		URL:          "https://tailscale.com/changelog/index.xml",
		PollInterval: 30 * time.Minute,
	},
	{
		// The documented page is HTML, but the site advertises this RSS feed in
		// its <head>, so no scrape parser is needed.
		Name:         "Tailscale security bulletins",
		Category:     CategorySecurity,
		URL:          "https://tailscale.com/security-bulletins/index.xml",
		PollInterval: 10 * time.Minute,
	},
	{
		Name:         "Tailscale Learn",
		Category:     CategoryOfficial,
		URL:          "https://tailscale.com/learn/index.xml",
		PollInterval: 6 * time.Hour,
	},
	// "Tailscale dev blog" (https://tailscale.dev/feed.xml) disabled 2026-09-21:
	// the feed itself still returns 200 with well-formed items, but tailscale.dev
	// now blanket-redirects every /blog/<slug> path to https://tailscale.com/blog/,
	// so every item link resolves to the generic blog homepage instead of the
	// story. Re-enable only if the dev blog content is republished at a live URL.
	{
		// Reddit's subreddit feed is Atom, so no new parser is needed. "new"
		// (not the default hot sort) is used so nothing is missed between polls.
		Name:         "r/Tailscale",
		Category:     CategoryCommunity,
		URL:          "https://www.reddit.com/r/Tailscale/new.rss",
		PollInterval: 30 * time.Minute,
	},
}

// Sources returns the registered sources. The result is a copy, so callers
// cannot mutate the registry.
func Sources() []Source {
	out := make([]Source, len(registeredSources))
	copy(out, registeredSources)
	return out
}
