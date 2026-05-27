# Australia (`au`)

Scrapers and configuration for ingesting Australian Commonwealth legislation
into the `lex` RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the Federal Register of Legislation OData API (NOT scraping)

Australia's **Federal Register of Legislation** (FRL, `legislation.gov.au`),
maintained by the Office of Parliamentary Counsel, is the authorised
whole-of-government source for Commonwealth legislation. It exposes a public,
**no-auth OData v1 JSON API**; the human site is a client-rendered SPA backed by
the same API. We hit the API directly and never crawl the UI. See
[ADR-0024](../docs/adr/0024-au-ingest-frl-odata-api.md).

Base: `https://api.prod.legislation.gov.au/v1`

| Endpoint | Contents |
|----------|----------|
| `GET /titles?$filter=&$top=&$skip=&$count=true` | paginated title list (`{@odata.count, value}`) |
| `GET /titles/{id}` | title detail — `name`, `collection`, `seriesType`, `year`, `number`, `status`, `makingDate` |
| `GET /Versions/Default.Find(titleId='{id}',asAtSpecification='current')` | current compilation — `start` (as-of date), `status`, `reasons[]` (amend/repeal edges) |
| `GET /documents?$filter=titleId eq '{id}'` | document manifests — full text only as `.docx`/`.pdf`/`.epub` |

Each title's register id (e.g. `C1901A00002`) maps to the human page
`https://www.legislation.gov.au/C1901A00002` — stored as `lex:sourceURL`.

## Legal status & license

- Commonwealth legislative material on the FRL is published under the
  **Creative Commons Attribution 4.0 International licence (CC BY 4.0)**,
  consistent with whole-of-government open-content policy. The Commonwealth Coat
  of Arms is excluded.
- Reuse, including commercial reuse and redistribution of a prebuilt dataset, is
  permitted **with attribution**.
- The FRL copyright page is rendered client-side; **verify** the exact current
  CC BY 4.0 wording before publishing a prebuilt AU dataset (see ADR-0024,
  "Open point").
- We attribute **© Commonwealth of Australia, Federal Register of Legislation**
  and preserve `lex:sourceURL` per record.

## Act-type mapping (FRL → ELI `eli:type_document` slug)

| FRL collection / seriesType | slug | note |
|-----------------------------|------|------|
| Act | `act` | a primary act |
| LegislativeInstrument | `legislative-instrument` | a legislative instrument |
| NotifiableInstrument | `notifiable-instrument` | a notifiable instrument |
| Constitution | `constitution` | the Constitution |
| *(other)* | CamelCase/space-folded ASCII slug | fallback |

Identity: work `Number` is the **register id** (e.g. `C1901A00002`) — globally
unique and stable across compilations; `Year` is the FRL `year`; `eli:id_local`
is the register id.

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# One year of Acts:
go run ./au/scripts/import -out au/data/graph -collection Act -from 1901 -to 1901

# A range of years:
go run ./au/scripts/import -out au/data/graph -collection Act -from 2020 -to 2024
```

Flags: `-out`, `-base`, `-ua`, `-collection` (default `Act`), `-from`, `-to`
(both required), `-rps` (request rate limit, default 5/s).

- `scripts/frl/` — pure, offline parser + mapper: title list / detail / version
  → `schema.Act`. Golden-tested on committed real fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from the current Version's
`start` (when the consolidated compilation took effect), falling back to the
title's `makingDate`; status from the FRL `status`; amend / repeal edges from the
Version's `reasons[]` (acts that affected this title → `eli:amended_by` /
`eli:repealed_by`).

## Status

✅ Metadata + relations pass works end-to-end (identity, title, version date,
status, source URL, amended-by / repealed-by edges).
🚧 Next: **section text** — the FRL serves full text only as binary
`.docx`/`.pdf`/`.epub`, so `lex:Article` (section) extraction needs an
OOXML/PDF channel and is deferred (see ADR-0024); inverse `eli:amends`/`cites`
edges from the amending act's record; point-in-time revisions; then the MCP
server + search index (country-agnostic, shared with UA/PL).
