# Switzerland (`ch`)

Scrapers and configuration for ingesting Swiss federal legislation into the
`lex` RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the official Fedlex SPARQL endpoint (NOT scraping)

Switzerland publishes **Fedlex** (Swiss Federal Chancellery) as native linked
open data. The Classified Compilation (*Systematische Rechtssammlung*, **SR**)
is exposed as RDF behind a public **SPARQL endpoint**. Fedlex's URIs are
themselves **ELI** URIs and the data is modelled with the **JoLux** ontology
(FRBR work/expression/manifestation), so the mapping to our ELI/FRBR ontology
is close. We query the endpoint directly and never crawl the human site. See
[ADR-0017](../docs/adr/0017-ch-ingest-fedlex-sparql.md).

- SPARQL endpoint: `https://fedlex.data.admin.ch/sparqlendpoint`
  (POST `query=...`, header `Accept: application/sparql-results+json`)
- Linked-data / human entry: `https://fedlex.data.admin.ch`
- **No authentication** required (public, anonymous SPARQL over HTTP).

The importer issues one grouped `SELECT` over `jolux:ConsolidationAbstract`
works classified by SR notation, realized by a German expression carrying the
title/short title, with the work's in-force dates. See ADR-0017 for the exact
query.

## Legal status & license

- Swiss **official federal legislation is not an object of copyright** — art. 5
  para. 1 lit. a–c of the Copyright Act (*URG*, SR 231.1) excludes laws,
  ordinances, international treaties, and other official enactments. Reuse is
  free.
- Fedlex publishes the data as open linked data via an anonymous SPARQL
  endpoint (no token, no auth).
- We attribute **Fedlex / the Swiss Confederation** and preserve the Fedlex ELI
  work URI as `lex:sourceURL` per record. **Verify** for an explicit ToS before
  publishing a prebuilt CH dataset (see ADR-0017, "License").

## Act-type mapping (CH → ELI `eli:type_document` slug)

Switzerland exposes no single machine "type" on the SR work, so the slug is
inferred from the German title (the same title-driven approach used for UA/PL
codes):

| German title contains | slug | note |
|-----------------------|------|------|
| *Gesetzbuch* | `gesetzbuch` | a code (e.g. ZGB, StGB, OR) |
| *Bundesverfassung* | `bundesverfassung` | the Federal Constitution |
| *Bundesgesetz* | `bundesgesetz` | a federal act |
| *Verordnung* | `verordnung` | an ordinance |
| *Bundesbeschluss* | `bundesbeschluss` | a federal decree |
| *Reglement* | `reglement` | a regulation |
| *(other)* | `erlass` | generic enactment fallback |

Identity: work `Number` is the **SR notation** (e.g. `210`); `Year` is the
version-date year; `eli:id_local` is `"SR <notation>"` (e.g. `SR 210`).
`SourceURL` is the Fedlex ELI work URI (`https://fedlex.data.admin.ch/eli/cc/...`).

## Language

Swiss federal law is equally authentic in **German, French, and Italian**. v1
stores the **German** expression (`LangTag "de"`, `LangAlpha3 "DEU"`). French
(`fr`/`FRA`) and Italian (`it`/`ITA`) expressions are a next phase (additive, no
schema change).

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A few named acts by SR notation (Civil Code, Code of Obligations, Criminal Code):
go run ./ch/scripts/import -out ch/data/graph -sr 210,220,311.0

# A capped enumeration of the whole SR (rate-limited):
go run ./ch/scripts/import -out ch/data/graph -limit 500
```

Flags: `-endpoint`, `-out`, `-ua`, `-sr` (comma-separated SR notations; empty =
all), `-limit` (SPARQL `LIMIT`, 0 = none), `-rps` (request rate limit, default
2/s).

- `scripts/fedlex/` — pure, offline parser + mapper: SPARQL JSON results →
  `[]schema.Act`. Golden-tested on a committed real SPARQL JSON fixture.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + map + write to
  the Badger store; tested end-to-end via `httptest` serving the fixture.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each work's
`jolux:dateNoLongerInForce` when repealed (text frozen at repeal), else
`jolux:dateEntryInForce`; status from the presence of `dateNoLongerInForce`.

## Status

✅ Metadata pass works end-to-end (identity via SR notation, German title +
short title, version date, in-force/repealed status, source URL, ELI/FRBR
resource+expression).
🚧 Next (all additive, no schema change): article full text (`lex:Article`,
served via separate Fedlex XML/HTML manifestations); amend/repeal/cite edges
(JoLux `citationFrom/ToLegalResource`); French/Italian expressions;
point-in-time `dateApplicability` revisions; then the MCP server + search index
(country-agnostic, shared with UA/PL).
