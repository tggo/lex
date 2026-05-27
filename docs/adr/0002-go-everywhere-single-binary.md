# ADR 0002 — Go for both scrapers and server; one binary

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

The maintainer specified Go for the per-country scrapers. The MCP server could
be any language. Mixing languages multiplies build/CI/contribution friction for
an open-source project where contributors mostly add scrapers.

## Decision

Use Go for everything — scrapers and the MCP server. Ship the server as a
single static binary (`cmd/lex`). Scrapers are Go programs/packages under
`./<cc>/scripts`.

## Consequences

- One toolchain, one CI, `go install`-able. Easy contributor onboarding.
- Static binary = trivial local-first distribution alongside a SQLite file.
- We accept Go's relative verbosity for HTML scraping vs. Python; offset by
  not maintaining a second runtime.
