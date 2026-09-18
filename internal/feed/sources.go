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
	{
		Name:         "Tailscale dev blog",
		Category:     CategoryDevelopment,
		URL:          "https://tailscale.dev/feed.xml",
		PollInterval: 6 * time.Hour,
	},
}

// Sources returns the registered sources. The result is a copy, so callers
// cannot mutate the registry.
func Sources() []Source {
	out := make([]Source, len(registeredSources))
	copy(out, registeredSources)
	return out
}
