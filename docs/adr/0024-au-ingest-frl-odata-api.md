# ADR 0024 — Australia: ingest via the Federal Register of Legislation OData API

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Australia (`./au`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

The **Federal Register of Legislation** (FRL, `legislation.gov.au`), maintained
by the Office of Parliamentary Counsel, is the authorised whole-of-government
source for Commonwealth legislation. Its relaunched platform exposes a public,
**no-auth OData v1 JSON API** at `https://api.prod.legislation.gov.au/v1`
(the human site is a client-rendered SPA backed by the same API). We hit the API
directly and never crawl the UI.

- Base: `https://api.prod.legislation.gov.au/v1`
- Swagger: `https://api.prod.legislation.gov.au/swagger/v1/swagger.json`
- Human, per-title pages: `https://www.legislation.gov.au/{registerId}`

Verified live (2026-05-27, anonymous — no API key):

| Endpoint | Purpose |
|----------|---------|
| `GET /v1/titles?$filter=&$top=&$skip=&$count=true` | paginated title list (OData) — `{@odata.count, value:[…]}` |
| `GET /v1/titles/{id}` | title detail: `id`, `name`, `collection`, `seriesType`, `year`, `number`, `status`, `isInForce`, `makingDate` |
| `GET /v1/Versions/Default.Find(titleId='{id}',asAtSpecification='current')` | the current point-in-time compilation: `start` (as-of date), `status`, `registerId`, `reasons[]` (amend/repeal edges) |
| `GET /v1/documents?$filter=titleId eq '{id}'` | document manifests — full text only as binary `.docx`/`.pdf`/`.epub` |

Sample title (`GET /v1/titles/C1901A00002`):

```jsonc
{ "id":"C1901A00002", "name":"Acts Interpretation Act 1901",
  "collection":"Act", "seriesType":"Act", "year":1901, "number":2,
  "status":"InForce", "isInForce":true, "makingDate":"1901-07-12T00:00:00" }
```

A current Version carries the as-of date and the affecting edges:

```jsonc
{ "titleId":"C1901A00002", "start":"2026-03-28T00:00:00", "isCurrent":true,
  "status":"InForce", "registerId":"C2026C00117", "compilationNumber":"39",
  "reasons":[ { "affect":"Amend",
    "affectedByTitle":{ "titleId":"C2026A00004", "year":2026, "seriesType":"Act" } } ] }
```

### License

Commonwealth legislative material on the Federal Register of Legislation is
published under the **Creative Commons Attribution 4.0 International licence
(CC BY 4.0)**, consistent with whole-of-government open-content policy (the same
licence applies across Commonwealth public material, e.g. PM&C publications).
The Commonwealth Coat of Arms is excluded. Reuse — including commercial reuse and
redistribution of a prebuilt dataset — is permitted **with attribution**.

Attribution: **© Commonwealth of Australia, Federal Register of Legislation**
(CC BY 4.0); `lex:sourceURL` is preserved per record.

> **Open point (verify before public redistribution):** the FRL copyright page
> is rendered client-side (SPA), so the exact CC BY 4.0 wording could not be
> captured by a plain HTTP fetch at ingest time. The CC BY 4.0 basis is
> well-established whole-of-government policy; re-confirm the current FRL
> copyright statement before publishing a prebuilt AU dataset (ADR-0006).

## Decision

Australia ingestion pulls from the **FRL OData API** (JSON), not scraped pages.
The AU importer:

1. pages `GET /v1/titles` filtered by `year` and `collection` (default `Act`),
   using `$top`/`$skip` and `@odata.count` to terminate;
2. for each title fetches detail + the current Version;
3. maps each title into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: the FRL register id `C1901A00002` → `eli:id_local`. Work URI uses
  `Country="au"`, `Year=<year>`, `Number=<registerId>` (the register id is
  globally unique and stable across compilations, unlike `number` which only
  disambiguates within a year+collection). `SourceURL` =
  `https://www.legislation.gov.au/<registerId>`.
- **Type slug** (`eli:type_document`): mapped from `collection` (falling back to
  `seriesType`) — `Act`→`act`, `LegislativeInstrument`→`legislative-instrument`,
  `NotifiableInstrument`→`notifiable-instrument`, `Constitution`→`constitution`,
  otherwise a CamelCase/space-folded ASCII slug. See `au/README.md`.
- **As-of date** (`eli:version_date`, MANDATORY): the **current Version's
  `start`** — when the current consolidated compilation took effect. This is the
  cleanest "consolidated as of" signal the API offers. Falls back to the title's
  `makingDate` when no current Version exists. `makingDate` →
  `eli:first_date_entry_in_force`.
- **Status**: the Version's `status` (then the title's `status`) — `InForce`
  →in-force; `Repealed`/`Ceased`/`Expired`→not in force.
- **Relations**: a Version's `reasons[]` list the acts that **affected this
  title** (this title was amended/repealed *by* them), so `affect:"Amend"`
  →`eli:amended_by`, `affect:"Repeal"`→`eli:repealed_by`; targets resolved to lex
  work URIs via each `affectedByTitle` (`titleId`+`year`+`seriesType`). Edges are
  emitted in deterministic `titleId` order. (The inverse `eli:amends`/`cites`
  direction is available from the amending act's own record and is additive
  later.)

### Scoped out for v1 — article/section text

ELI calls them articles; in Australia they are **sections**. The FRL API exposes
full text **only as binary `.docx`/`.pdf`/`.epub`** documents — there is no
structured per-section JSON or HTML channel (unlike Poland's `struct` +
`text.html`). Extracting clean section text would require parsing OOXML/PDF,
which needs heuristics and/or a new dependency (forbidden: stdlib +
`golang.org/x/net/html` only) and produces brittle, source-specific output. So
**section text is deferred to a later phase**; v1 stores metadata, version date,
status, source URL, and amend/repeal edges. No `lex:Article` nodes are emitted.
This is additive later (a section-text channel or an OOXML parser) with **no
schema change**.

## Consequences

- Legally clean (CC BY 4.0 with attribution), API-sanctioned, no HTML crawling.
- Clean point-in-time `version_date` from the current compilation's `start`.
- **Politeness**: the FRL holds tens of thousands of titles; the importer
  rate-limits (default 5 req/s) and backs off on 429/5xx. `-from`/`-to`/
  `-collection` flags allow incremental runs. A missing current Version (404) is
  tolerated — metadata still imports.
- No network in tests — real `titles`/`titles/{id}`/`Versions` responses are
  captured once under `au/scripts/frl/testdata/`.
