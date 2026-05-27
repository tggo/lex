# Japan (`jp`)

Scrapers and configuration for ingesting Japanese legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — official API (NOT scraping)

We use the **e-Gov 法令API v2**, the official machine channel of *e-Gov 法令検索*
(MIC / Digital Agency), the authoritative consolidated legislation system for
憲法・法律・政令・勅令・府省令・規則. The website itself is a client-rendered SPA;
we hit the API and never crawl the site. See
[ADR-0011](../docs/adr/0011-jp-ingest-egov-law-api.md).

Base: `https://laws.e-gov.go.jp/api/2/` — JSON (XML also available).
Swagger UI: `/api/2/swagger-ui` · ReDoc: `/api/2/redoc/`.

### Endpoints used

| Endpoint | Contents |
|----------|----------|
| `GET /api/2/laws` | paginated law list + revision metadata (~9,514 laws) |
| `GET /api/2/law_data/{law_id\|law_revision_id}` | full structured text of an act |
| `GET /api/2/keyword` | server-side keyword search (we still build our own FTS5) |

### Identifiers

- `law_id` — stable work-level id, e.g. `129AC0000000089` (Civil Code). Stored as
  `eli:id_local`.
- `law_revision_id` — expression-level, `<law_id>_<YYYYMMDD>_<seq>`. Maps to one
  `eli:LegalExpression`; its `amendment_enforcement_date` is the mandatory
  `eli:version_date` (as-of date).

Each act maps to the human page `https://laws.e-gov.go.jp/law/<law_id>` — stored
as `lex:sourceURL`.

## Legal status & license

- Japanese statutes are **not** objects of copyright (Copyright Act, Art. 13).
- e-Gov content is provided under **政府標準利用規約（第2.0版）** (Government
  Standard Terms of Use v2.0), which since 2015 is **explicitly compatible with
  CC BY 4.0** — free to copy, redistribute, translate, adapt, including
  commercially, **with attribution to the source**. Terms:
  `https://laws.e-gov.go.jp/terms/`.
- We attribute **e-Gov 法令検索 / 総務省 (CC BY 4.0)** in this README, in dataset
  Releases, and preserve `lex:sourceURL` per record.

## Act-type mapping (JP `law_type` → ELI `eli:type_document` slug)

| `law_type` (API) | slug | example |
|------------------|------|---------|
| `Constitution` | `kenpo` | 日本国憲法 |
| `Act` | `horitsu` | 民法 (Civil Code) |
| `CabinetOrder` | `seirei` | 〜施行令 |
| `ImperialOrder` | `chokurei` | pre-war 勅令 |
| `MinisterialOrdinance` | `furei` | 〜施行規則 |
| `Rule` | `kisoku` | agency 規則 |

## Article structure (条/項/号)

Japanese acts nest 条 (article) › 項 (paragraph) › 号 (item). v1 maps each **条**
to one `lex:Article`, keeping the full 条 text (including its 項/号) in `lex:text`;
labels carry `@ja`. Finer-grained 項/号 nodes are an additive extension later.

## Directories

- `scripts/` — Go importer. Lists laws via the v2 API, fetches each act's current
  enforced revision, parses 条 structure, emits RDF (Turtle) and/or writes
  directly to the SQLite store under `data/`.
- `data/` — built artifacts (**gitignored**). Built locally or downloaded from
  GitHub Releases.

## Status

🚧 Not yet implemented. Source, endpoints, and license are decided (ADR-0011).
P1: one act end-to-end — list via `/api/2/laws`, fetch the Civil Code
(`129AC0000000089`) current revision via `/api/2/law_data`, set status from
`current_revision_status`, parse into 条, emit ELI RDF per
[`../docs/ontology.md`](../docs/ontology.md), store, and confirm it is searchable
via the server.
