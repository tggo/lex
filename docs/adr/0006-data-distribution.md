# ADR 0006 — Data distribution: build locally or download a prebuilt DB

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

A country's full legislation is large and changes over time. Committing it to
git would bloat the repo and go stale instantly. But requiring every user to
scrape from scratch on first run is slow and hammers the official source.

## Decision

Two supported paths, never committing data to git:

1. **Build locally** — run the country's scrapers to produce the SQLite
   database into `./<cc>/data/` (gitignored).
2. **Download prebuilt** — fetch a ready `.db` from **GitHub Releases**,
   published by CI on a schedule.

`./*/data/` is gitignored. Releases are versioned and note the `version_date`
coverage and source snapshot time.

## Consequences

- Repo stays lean; users pick convenience (Release) or freshness/independence
  (local build).
- Need CI to periodically rebuild and publish Releases; need a documented,
  reproducible scraper run. Tracked for P2.
- Release artifacts must state the as-of date prominently (legal staleness).
