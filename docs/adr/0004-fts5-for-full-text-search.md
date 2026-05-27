# ADR 0004 — SQLite FTS5 for full-text search, alongside the RDF graph

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

`search_laws` needs real full-text search over titles and article text:
ranking, prefix/phrase matching, tokenization. SPARQL only offers
`FILTER(CONTAINS/REGEX)` — a linear scan with no ranking, unusable at the scale
of a country's legislation.

## Decision

Maintain a SQLite **FTS5** virtual table in the *same* SQLite file the RDF
backend uses. Rows: `uri, act_uri, kind (title|article), text, lang`. Populate
it from `dct:title` (expressions) and `lex:text` (articles) during ingestion.
`search_laws` queries FTS5, then resolves matches back to the graph for
metadata via the URI.

Semantic / embedding search is explicitly deferred to a later phase; FTS5 is
the v1 search engine.

## Consequences

- One file holds both the graph and the index — distribution stays simple.
- Ingestion has a second write path (triples + FTS rows) to keep in sync; the
  store layer owns this and rebuilds FTS from triples if they diverge.
- Tokenization for Ukrainian/Cyrillic must be validated (FTS5 `unicode61`);
  stemming may need a later pass. Tracked as a P1 task.
