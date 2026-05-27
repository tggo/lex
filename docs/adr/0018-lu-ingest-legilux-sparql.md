# ADR 0018 — Luxembourg: ingest via the Legilux SPARQL endpoint

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Luxembourg (`./lu`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

Luxembourg is well-suited because the **Service central de législation
(Legilux)** publishes its legislation as **Linked Open Data** using the
**JOLux + ELI** models — the European Legislation Identifier is the very
backbone of our ontology (ADR-0005), so the mapping is close to 1:1.

- SPARQL endpoint: `https://data.legilux.public.lu/sparqlendpoint`
- (The `https://data.legilux.public.lu/sparql` path is a JavaScript web UI
  — "Casemates" — not a machine endpoint; we use `…/sparqlendpoint`.)
- Human/landing pages are the ELI work URIs themselves, e.g.
  `http://data.legilux.public.lu/eli/etat/leg/rgd/2020/03/18/a165/jo`.

Verified live (2026-05-27): `…/sparqlendpoint` answers SPARQL 1.1 SELECT
queries with `Accept: application/sparql-results+json`, **no authentication**.
The store holds ~382k `jolux:Work`, ~256k `jolux:Expression` nodes.

### The JOLux data model (FRBR-shaped)

| Node | Predicates we read |
|------|--------------------|
| `jolux:Work` typed `jolux:Act` | `jolux:typeDocument` (→ resource-type authority, e.g. `RGD`/`LOI`/`A`), `jolux:dateDocument`, `jolux:dateEntryInForce`, `jolux:dateNoLongerInForce`, `jolux:inForceStatus`, `jolux:isRealizedBy` → Expression |
| `jolux:Expression` (one per language; the `…/fr` node) | `jolux:title`, `jolux:language` (EU language authority URI) |
| Work→Work edges | `jolux:modifies`, `jolux:repeals`, `jolux:cites`, `jolux:consolidates` |

`jolux:inForceStatus` values are application-status authority URIs:
`in-force` / `applicable` (in force), `no-longer-in-force` /
`no-longer-in-force-implicit` / `not-applicable` (no longer in force),
`not-yet-in-force` / `not-yet-applicable` (unknown for v1).

### License

Luxembourg **normative acts are open data**, published by Legilux under
**Creative Commons Attribution (CC BY)**. Redistribution is permitted with
attribution; we attribute **Legilux / État du Grand-Duché de Luxembourg** and
preserve the source work URI per record (`lex:sourceURL`).

## Decision

Luxembourg ingestion pulls from the **Legilux SPARQL endpoint** (JSON results),
not scraped pages. The LU importer:

1. pages a single acts-metadata query (`?work a jolux:Act` with title, type,
   dates, status) with `ORDER BY ?work LIMIT/OFFSET`;
2. for each act issues a relations query (`modifies`/`repeals`/`cites`/
   `consolidates`);
3. maps each act into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: the work URI's path after the Legilux ELI base
  (`etat/leg/rgd/2020/03/18/a165/jo`) is `eli:id_local` and the work `Number`;
  it is globally unique. `Country="lu"`, `Year` from `jolux:dateDocument`
  (falling back to the URI's year segment). `SourceURL` = the work URI itself.
- **Type slug** (`eli:type_document`): the `typeDocument` authority code →
  slug (`LOI`→`loi`, `RGD`→`rgd`, `A`→`arrete`, `DIR_UE`→`dir-ue`, …); a work
  URI under `/code/` → `code`. See `lu/README.md`.
- **Status**: `jolux:inForceStatus` authority code (enum above).
- **As-of date** (`eli:version_date`, MANDATORY): JOLux exposes no separate
  "consolidated as of" field on the base `Act` (consolidations are distinct
  Work nodes). We use **`jolux:dateDocument`** — always present and the stable
  as-of anchor for the act text — falling back to `dateEntryInForce`. An act
  with neither is dropped (never guessed). `dateEntryInForce` →
  `eli:first_date_entry_in_force`; `dateNoLongerInForce` →
  `eli:date_no_longer_in_force` when repealed.
- **Relations**: `jolux:modifies`→`eli:amends`, `jolux:repeals`→`eli:repeals`,
  `jolux:cites`→`eli:cites`, `jolux:consolidates`→`eli:consolidates`. Targets
  under the Legilux ELI base resolve to lex work URIs (via `schema.ResourceURI`,
  with an empty type slug since the target's own type is not in the edge);
  foreign targets (rare) are kept verbatim.
- **Language**: French is primary — `LangTag "fr"`, `LangAlpha3 "FRA"`,
  `eli:language` = the FRA authority URI. German (`de`) and Luxembourgish (`lb`)
  expressions are an additive next phase.

### Scoped out for v1

- **Article full text** is **deferred** (next phase). Article structure lives in
  the *consolidation* Work's XML/HTML manifestations, not the base `Act`
  metadata; reaching it cleanly is more work than the metadata pass and is
  additive (no schema change). v1 delivers metadata + relations with >80% test
  coverage, meeting the ontology's mandatory invariants (title, version date,
  status, source URL).
- Point-in-time consolidations as `schema.Revision` nodes (the
  `jolux:Consolidation` Works) are a later phase.

## Consequences

- Legally clean (acts are CC BY open data), endpoint-sanctioned, no HTML
  crawling, no auth.
- Native JOLux/ELI means a clean country mapping.
- **Politeness**: the corpus is large (~230k acts); the importer rate-limits
  (default 2 req/s) and backs off on 429/5xx. A `-limit` flag allows partial
  runs.
- No network in tests — real `sparqlendpoint` SELECT responses (an acts page
  and a relations result) are captured once under
  `lu/scripts/legilux/testdata/` and served via `httptest`.
