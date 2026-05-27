# United Kingdom (`uk`)

Scrapers and configuration for ingesting UK legislation into the `lex` RDF
graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — legislation.gov.uk CLML + Atom feeds (NOT scraping)

The UK official publisher, **The National Archives**, runs
**legislation.gov.uk**, which exposes **native ELI identifiers** and a
machine-readable channel: every item is available as **CLML** (Crown
Legislation Markup Language) XML by appending `/data.xml`, and item lists are
published as **Atom feeds** (`/data.feed`). No authentication is required.
Because ELI is the backbone of our ontology, the mapping is close to 1:1. We hit
this channel directly and never crawl the human site. See
[ADR-0015](../docs/adr/0015-uk-ingest-legislation-gov-uk.md).

Base: `https://www.legislation.gov.uk`

| Endpoint | Contents |
|----------|----------|
| `GET /{type}/{year}/data.feed?page=N` | paginated Atom act list; `<leg:page>`/`<leg:morePages>` drive paging |
| `GET /{type}/{year}/{number}/data.xml` | full CLML document — title, dates, sections, inline citations |
| `GET /{type}/{year}/{number}` | human ELI page — stored as `lex:sourceURL` |

Each act's path (e.g. `ukpga/2023/57`) maps to the human, ELI-bearing page
`https://www.legislation.gov.uk/ukpga/2023/57` — stored as `lex:sourceURL`.

## Legal status & license

- legislation.gov.uk content is published under the **Open Government Licence
  v3.0 (OGL v3.0)** by The National Archives — copy, publish, distribute, adapt,
  and exploit commercially, **provided the source is attributed**.
- UK primary legislation is Crown copyright but explicitly licensed for reuse
  under the OGL.
- Required attribution: *"Contains public sector information licensed under the
  Open Government Licence v3.0."* We attribute **legislation.gov.uk / The
  National Archives** and preserve `lex:sourceURL` per record.

## Act-type mapping (legislation.gov.uk type code → ELI `eli:type_document` slug)

The legislation.gov.uk type code is already a short, stable ASCII slug, so the
mapping is **identity** (lower-cased):

| Type code | slug | note |
|-----------|------|------|
| `ukpga` | `ukpga` | UK Public General Act |
| `uksi` | `uksi` | UK Statutory Instrument |
| `asp` | `asp` | Act of the Scottish Parliament |
| `anaw` | `anaw` | Act of Senedd Cymru / National Assembly for Wales |
| `nia` | `nia` | Act of the Northern Ireland Assembly |
| `ukla` | `ukla` | UK Local Act |
| *(other)* | the code itself, lower-cased | fallback |

Identity: work `Number` is the act number (e.g. `57`) with `Year` from the path;
`eli:id_local` is the full path (`ukpga/2023/57`).

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A single year of UK Public General Acts:
go run ./uk/scripts/import -out uk/data/graph -types ukpga -from 2023 -to 2023

# Several types across a year range — large, rate-limited crawl:
go run ./uk/scripts/import -out uk/data/graph -types ukpga,uksi -from 2000 -to 2024
```

Flags: `-out`, `-base`, `-ua`, `-types ukpga,uksi,…`, `-from`, `-to`,
`-rps` (request rate limit, default 5/s). A year range is required (defaults to
the current year) so a bare run is bounded.

- `scripts/clml/` — pure, offline parser + mapper: Atom feed + CLML `data.xml` →
  `schema.Act`. Golden-tested on committed real fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each document's
`RestrictStartDate` (falling back to `dct:valid`, then enactment date); status
is recorded as in force for served consolidations; `eli:cites` edges come from
inline `<Citation>` elements.

## Status

✅ Metadata + inline citations + article-text pass works end-to-end (identity,
title, version date, first-in-force date, source URL, `eli:cites` edges,
`lex:Article`s).
🚧 Next: amends/repeals via point-in-time *effects* (`ukm:UnappliedEffect` / the
changes feed); sub-section granularity (`P2`/`P3`/schedules); point-in-time
revisions; incremental updates; then the MCP server + search index
(country-agnostic, shared with UA/PL).
