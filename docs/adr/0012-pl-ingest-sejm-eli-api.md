# ADR 0012 — Poland: ingest via the Sejm ELI API (api.sejm.gov.pl/eli)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Poland (`./pl`) is the third country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

Poland is unusually well-suited because the **Kancelaria Sejmu** publishes a
**native ELI API** — the European Legislation Identifier is the very backbone
of our ontology (ADR-0005), so the mapping is close to 1:1. The API covers the
official journals **Dziennik Ustaw (`DU`)** and **Monitor Polski (`MP`)**.

- Base: `https://api.sejm.gov.pl/eli/acts`
- Swagger UI: `https://api.sejm.gov.pl/eli/openapi/ui/`
- Human, RDFa-annotated pages: `https://eli.gov.pl/eli/{publisher}/{year}/{pos}`

Verified live (2026-05-27):

| Endpoint | Purpose |
|----------|---------|
| `GET /eli/acts/{pub}` | publisher info incl. list of available `years` |
| `GET /eli/acts/{pub}/{year}` | paginated act list (`?limit=&offset=`) — `{count,totalCount,offset,items}` |
| `GET /eli/acts/{pub}/{year}/{pos}` | act detail: `ELI`, `type`, `status`, `inForce`, dates, inline `references` |
| `GET /eli/acts/{pub}/{year}/{pos}/references` | relation edges keyed by Polish names, each target carrying `act.ELI` |
| `GET /eli/acts/{pub}/{year}/{pos}/struct` | document tree; articles are nodes with `type:"arti"` |
| `GET /eli/acts/{pub}/{year}/{pos}/text.html` | full consolidated text; article elements carry `id="<symbol>"` |

Sample list item (`GET /eli/acts/DU/2024`):

```jsonc
{ "ELI":"DU/2024/1984", "publisher":"DU", "year":2024, "pos":1984,
  "type":"Rozporządzenie", "status":"obowiązujący", "textHTML":true,
  "promulgation":"2024-12-31", "title":"Rozporządzenie Rady Ministrów …" }
```

The act detail adds `inForce` (`IN_FORCE`/`NOT_IN_FORCE`) and `entryIntoForce`.
References use Polish group names — `Akty zmienione` (acts amended),
`Akty uchylone` (acts repealed), `Podstawa prawna` / `Podstawa prawna z art.`
(legal basis) — each entry nesting an `act` with its own `ELI`.

### License

No explicit reuse license is stated on the API site (the documentation footer's
`© Kancelaria Sejmu` covers the docs, not the legislative data). The legal
basis for free reuse is nonetheless solid:

- **Polish normative acts are not objects of copyright** — art. 4 of the
  *ustawa o prawie autorskim i prawach pokrewnych* excludes normative acts and
  their official drafts, official documents, materials, signs, and symbols.
- Public-sector data reuse is further governed by the *ustawa o ponownym
  wykorzystywaniu informacji sektora publicznego*.
- The API is the **official sanctioned machine channel**; `eli.gov.pl` is a
  client-rendered/RDFa site — we hit the API, we do **not** crawl a UI.

> **Open point (verify before public redistribution):** no machine-readable API
> terms-of-use document was located. The acts themselves are freely reusable per
> art. 4; we attribute the source regardless. Re-check for an explicit
> `regulamin`/ToS before publishing a prebuilt PL dataset (ADR-0006).

Attribution: **Kancelaria Sejmu / api.sejm.gov.pl**.

## Decision

Poland ingestion pulls from the **Sejm ELI API** (JSON + HTML), not scraped
pages. The PL importer:

1. enumerates publishers (`DU`, `MP`) and their `years` via `GET /eli/acts/{pub}`;
2. pages `GET /eli/acts/{pub}/{year}` to list acts;
3. for each act fetches detail + references (+ `struct` & `text.html` when
   article text is requested);
4. maps each act into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: ELI `DU/2024/1984` → `eli:id_local`. Work URI uses
  `Country="pl"`, `Year=2024`, `Number="<publisher>/<pos>"` (e.g. `DU/1984`) so
  the `publisher+year+pos` triple is globally unique (DU and MP positions
  overlap). `SourceURL` = `https://eli.gov.pl/eli/<ELI>`.
- **Type slug** (`eli:type_document`): mapped from `type`, with a title override
  for codes — see `pl/README.md`. `Ustawa`→`ustawa` (but title contains
  *kodeks* →`kodeks`), `Rozporządzenie`→`rozporzadzenie`, etc.
- **Status**: `inForce` enum first (`IN_FORCE`→in-force, `NOT_IN_FORCE`→not in
  force), free-text `status` as fallback.
- **As-of date** (`eli:version_date`, MANDATORY): the API exposes no clean
  "consolidated as of" field, so we use `changeDate` (when the act record/text
  last changed), falling back to `promulgation`. `entryIntoForce` →
  `eli:first_date_entry_in_force`.
- **Relations**: `Akty zmienione`→`eli:amends`, `Akty uchylone`→`eli:repeals`,
  `Podstawa prawna`(`z art.`)→`eli:cites`; targets resolved to lex work URIs via
  each entry's `act.ELI`+`type`.
- **Articles**: walk `struct` for `type:"arti"` nodes; locate each by exact
  `id="<symbol>"` in `text.html`; that element's subtree text → `lex:text`.
  Titles/labels carry `@pl`; `eli:language` uses the POL authority URI.

## Consequences

- Legally clean (acts non-copyrightable), API-sanctioned, no HTML crawling.
- Native ELI means the cleanest country mapping so far.
- **Politeness**: full DU+MP is tens of thousands of acts (DU alone ≈ 97k); the
  importer rate-limits (default 5 req/s) and backs off on 429/5xx. Year-range
  and publisher flags allow incremental runs.
- Richer Polish article granularity (§ ustęp / pkt / lit.) is kept inside the
  article's `lex:text` for v1; finer structure is additive later, no schema
  change. Point-in-time revisions are out of scope for v1 (store current text).
- No network in tests — real `list`/`detail`/`references`/`struct`/`text.html`
  responses are captured once under `pl/scripts/eli/testdata/`.
