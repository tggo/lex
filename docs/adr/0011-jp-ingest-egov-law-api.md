# ADR 0011 — Japan: ingest via the e-Gov 法令API (v2), CC BY-compatible

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Japan (`./jp`) is the second country. Per the project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

The authoritative source is **e-Gov 法令検索** (Ministry of Internal Affairs and
Communications / Digital Agency), the official consolidated legislation system
covering 憲法・法律・政令・勅令・府省令・規則. It exposes a public **法令API**:

- **v1** — XML only. Base `https://laws.e-gov.go.jp/api/1/`. Endpoints:
  `lawlists/{type}`, `lawdata/{lawNum|lawId}`, `articles;lawId=...;article=...`,
  `updatelawlists/{yyyyMMdd}`.
- **v2** — JSON (and XML), richer and revision-aware. Base
  `https://laws.e-gov.go.jp/api/2/`. Swagger UI at `/api/2/swagger-ui`,
  ReDoc at `/api/2/redoc/`.

We **target v2** — its JSON is easier to consume in Go and it models
point-in-time revisions, which maps directly onto FRBR expressions.

Verified live against v2 (2026-05-27, `GET /api/2/laws?limit=1`):

```jsonc
{ "total_count": 9514, "laws": [ {
  "law_info":     { "law_type":"CabinetOrder", "law_id":"105DF0000000337",
                    "law_num":"明治五年太政官布告第三百三十七号",
                    "promulgation_date":"1872-11-09" },
  "revision_info":{ "law_revision_id":"105DF0000000337_18721109_000000000000000",
                    "law_title":"…（改暦ノ布告）", "category":"文化",
                    "amendment_enforcement_date":"1872-11-09",
                    "repeal_status":"None", "current_revision_status":"CurrentEnforced" },
  "current_revision_info": { "law_revision_id":"…", … }
} ] }
```

Key v2 endpoints we will use:

| Endpoint | Purpose |
|----------|---------|
| `GET /api/2/laws` | paginated list of laws + revision metadata (status, category, dates) |
| `GET /api/2/law_data/{law_id|law_revision_id}` | full structured text of an act (a revision) |
| `GET /api/2/keyword` | server-side keyword search (we still build our own FTS5) |

Stable identifiers: `law_id` (work-level, e.g. `129AC0000000089` = Civil Code) and
`law_revision_id` (expression-level, `<law_id>_<YYYYMMDD>_<seq>`), with
`amendment_enforcement_date` giving the as-of date.

### License

e-Gov 法令検索 content is provided under the **政府標準利用規約（第2.0版）**
(Government Standard Terms of Use, v2.0). Since its 2015 revision this规約 is
**explicitly compatible with CC BY 4.0** — content may be freely copied,
publicly transmitted, translated, and adapted, including commercially, provided
the source is attributed. (Japanese statutes themselves are not objects of
copyright under Art. 13 of the Copyright Act.) Terms: `https://laws.e-gov.go.jp/terms/`.

`robots.txt` is not a concern: the API is the sanctioned machine channel, and
`https://laws.e-gov.go.jp/` is a client-rendered SPA — we hit the API, never
crawl the site.

## Decision

Japan ingestion pulls from the **e-Gov 法令API v2** (JSON), not scraped HTML.
The JP scraper:

1. lists laws via `GET /api/2/laws` (paginated), capturing `law_id`,
   `law_revision_id`, `law_type`, title, category, enforcement/repeal status;
2. for each law's current enforced revision, fetches the structured text via
   `GET /api/2/law_data/{law_revision_id}` and parses 条/項/号 into article nodes;
3. maps each act into ELI RDF per `docs/ontology.md`:
   - `law_id` → `eli:id_local` on the `eli:LegalResource`;
   - the current `law_revision_id` → one `eli:LegalExpression`, with
     `amendment_enforcement_date` → `eli:version_date` (MANDATORY as-of date),
     `promulgation_date` → the work's identifying year in the resource URI,
     `repeal_status`/`current_revision_status` → in-force vs. repealed;
     (`eli:first_date_entry_in_force` is left unset — the API's per-revision
     enforcement dates are not the act's original entry-into-force);
   - `lex:sourceURL` → the human page `https://laws.e-gov.go.jp/law/<law_id>`;
4. attributes the source per CC BY 4.0 in output and docs.

JP `eli:type_document` slugs are mapped from `law_type` in `jp/README.md`.

## Consequences

- Legally clean (CC BY-compatible), API-sanctioned, no HTML crawling.
- v2's revision model fits FRBR: this ADR stores only the **current enforced**
  expression. The historical revision timeline was added later as metadata-only
  expressions — see ADR-0014 (JP historical revisions).
- Japanese article granularity (条 article, 項 paragraph, 号 item) is richer than
  the flat UA article model; v1 maps each 条 to one `lex:Article` and keeps the
  full 条 text (incl. its 項/号) in `lex:text`. Finer structure is additive later.
- Titles/labels carry `@ja`; `eli:language` uses the JPN authority URI.
- No network in tests — commit a few real `/api/2/laws` + `law_data` responses
  as fixtures under `jp/scripts/.../testdata/` (ADR/CLAUDE testing policy).
