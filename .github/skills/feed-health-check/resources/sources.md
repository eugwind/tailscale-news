# Candidate Sources

Registered sources live in `internal/feed/sources.go`. This file tracks what is
registered, what was verified, and what is still a candidate. **Verify any URL
with `scripts/check-feeds.sh` before registering it.**

## Registered (verified 2026-09-18)

| Source | Category | Feed URL | Notes |
|---|---|---|---|
| Tailscale blog | official | `https://tailscale.com/blog/index.xml` | 15 items, all dated |
| Tailscale changelog | release-notes | `https://tailscale.com/changelog/index.xml` | 302 items, ~375 KB; anchor-based links |
| Tailscale security bulletins | security | `https://tailscale.com/security-bulletins/index.xml` | 46 items; anchor-based links |
| Tailscale Learn | official | `https://tailscale.com/learn/index.xml` | 15 items; descriptions are empty |
| Tailscale dev blog | development | `https://tailscale.dev/feed.xml` | 20 items; the only source with a usable `ETag` |

The documented security-bulletins page is HTML, but the site advertises an RSS
feed at `/security-bulletins/index.xml` in its `<head>`, so no scrape parser is
needed.

**No `tailscale.com` feed sends `ETag` or `Last-Modified`**, so conditional
polling is unavailable for four of the five sources. The fetcher must compare a
stored content hash instead.

**Anchor-based feeds**: changelog and security-bulletin entries all share one
path and differ only by fragment (`/changelog/#2026-09-17-service`). URL
canonicalisation must preserve fragments or the entire feed collapses onto a
single URL.

## Candidates (not verified, not registered)

| Source | Category | Candidate feed URL | Notes |
|---|---|---|---|
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
