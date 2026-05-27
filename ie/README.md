# Ireland (`ie`)

Scrapers and configuration for ingesting Irish legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the electronic Irish Statute Book (native ELI)

Ireland's **electronic Irish Statute Book (eISB)**, produced by the Office of
the Attorney General, serves a **native ELI**: every act page carries RDFa
`<meta>` elements expressing the European Legislation Identifier vocabulary —
the very backbone of our ontology — so the mapping is close to 1:1. Acts are
written in Akoma Ntoso; sections are `<section>` elements (rendered in the print
HTML as `<a name="secN">` anchors).

Acts are **enumerated** from the Houses of the Oireachtas open-data API, which
carries each act's eISB ELI; their metadata and text come from the eISB page. We
hit these two no-auth channels directly and never crawl the UI. See
[ADR-0022](../docs/adr/0022-ie-ingest-irish-statute-book.md).

| Endpoint | Contents |
|----------|----------|
| `GET https://api.oireachtas.ie/v1/legislation?act_year={y}&lang=en&limit=&skip=` | act listing — `head.counts.resultCount`, `results[].bill.act.{actYear,actNo,shortTitleEn,statutebookURI}` |
| `GET http://www.irishstatutebook.ie/eli/{y}/act/{no}/enacted/en/print.html` | act page: RDFa `eli:*` meta (title, dates, type, `eli:has_part`, `eli:changes`) + section bodies |

Each act's eISB ELI (e.g. `2015/act/60`) maps to the human page
`http://www.irishstatutebook.ie/eli/2015/act/60` — stored as `lex:sourceURL`.

> `data.oireachtas.ie` serves raw Akoma Ntoso XML but returns **403** without
> credentials, so it is not a no-auth channel; we use the RDFa-annotated eISB
> print page instead, which is the official served representation.

## Legal status & license

- Re-use is governed by the **Oireachtas (Open Data) PSI Licence** — a standard
  licence for the purposes of Directive (EU) 2019/1024, transposed by S.I.
  No. 376/2021 — which **incorporates the Creative Commons Attribution 4.0
  International License**. Each act page carries `eli:licence` →
  `http://www.irishstatutebook.ie/eli/open-data.html`.
- Re-use, including redistribution of a prebuilt dataset, is permitted **with
  attribution**. We attribute the **Irish Statute Book / Office of the Attorney
  General** (and the **Houses of the Oireachtas** for enumeration) and preserve
  `lex:sourceURL` per record.

## Act-type mapping (IE → ELI `eli:type_document` slug)

| eISB type | slug | note |
|-----------|------|------|
| `…/resource-type#ACT` (or ELI path token `act`) | `act` | Act of the Oireachtas |
| `…/resource-type#SI` (or ELI path token `si`) | `si` | Statutory Instrument (mapped; enumeration is Acts-only in v1) |
| *(other)* | `act` | fallback |

Identity: work `Number` is the act number (e.g. `60`) with `Year` from the ELI;
`eli:id_local` is the full eISB ELI path (`2015/act/60`).

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A single year of Acts, with section text:
go run ./ie/scripts/import -out ie/data/graph -from 2015 -to 2015 -articles

# A range of years (rate-limited crawl):
go run ./ie/scripts/import -out ie/data/graph -from 2020 -to 2024 -articles
```

Flags: `-out`, `-list-base`, `-eisb-base`, `-ua`, `-from` (required), `-to`
(defaults to `-from`), `-articles`, `-rps` (request rate limit, default 5/s).

- `scripts/eisb/` — pure, offline parser + mapper: Oireachtas list JSON and the
  eISB RDFa act page → `schema.Act`. Golden-tested on committed real fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each act's
`eli:date_document` (the enacted/signing date); status is treated as in-force
for the enacted text the eISB serves; `eli:changes` edges become `eli:amends`.

## Status

✅ Metadata + amends edges + section-text pass works end-to-end (identity,
title, version date, source URL, `eli:amends`, `lex:Article`s).
🚧 Next: Statutory Instruments; LRC point-in-time *revised* editions and derived
repeal status; the Irish-language (`ga`) expression; sub-section granularity;
inbound amended_by/repealed_by/cites edges; then the MCP server + search index
(country-agnostic, shared with UA/PL).
