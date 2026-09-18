# Candidate Sources

Seed list for the aggregator. **Verify each URL with `scripts/check-feeds.sh` before
registering it** — feed paths move, and nothing here is guaranteed current.

| Source | Category | Candidate feed URL | Notes |
|---|---|---|---|
| Tailscale blog | official | `https://tailscale.com/blog/index.xml` | Primary announcement channel |
| Tailscale changelog | release-notes | `https://tailscale.com/changelog` | Likely HTML — needs a scrape parser |
| Tailscale security bulletins | security | `https://tailscale.com/security-bulletins` | High priority; rank above general news |
| `tailscale/tailscale` releases | release-notes | `https://github.com/tailscale/tailscale/releases.atom` | GitHub Atom, stable format |
| `tailscale/tailscale` commits | development | `https://github.com/tailscale/tailscale/commits/main.atom` | High volume — filter hard or skip |
| GitHub advisories | security | GitHub Advisory API, filtered to the `tailscale` org | Requires a token; read-only scope |
| r/Tailscale | community | `https://www.reddit.com/r/Tailscale/.rss` | Rate-limited; back off aggressively |
| Hacker News mentions | community | Algolia HN search API, query `tailscale` | JSON; de-duplicate against official posts |

## Registering a Source

Record for each source: display name, category, feed URL, format, poll interval,
whether auth is required, and the date it was last verified. Security and release-note
sources poll more often than community ones.

## Categories

`official` · `release-notes` · `security` · `development` · `community` · `third-party`

Security items are never suppressed by dedup — if a story appears in both a security
bulletin and a community post, the bulletin wins and keeps its category.
