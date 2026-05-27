# ADR 0021 — Spain: ingest via the BOE open-data API (datos abiertos)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Spain (`./es`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

The **Agencia Estatal Boletín Oficial del Estado (BOE)** runs a machine-readable
open-data API (`datos abiertos`). Its **Legislación Consolidada** collection is
the official, continuously-updated consolidated text of state norms — exactly
the FRBR "current consolidated expression" lex stores in v1.

- Base: `https://www.boe.es/datosabiertos/api/legislacion-consolidada`

Verified live (2026-05-27):

| Endpoint | Purpose |
|----------|---------|
| `GET /?limit=&offset=` | paginated norm list — `{status,data:[…]}` (JSON or XML) |
| `GET /id/{id}/metadatos` | norm metadata: `identificador`, `rango`, dates, `estado_consolidacion`, `estatus_derogacion`, `vigencia_agotada`, `url_eli` (JSON) |
| `GET /id/{id}/analisis` | `referencias.anteriores` / `.posteriores`, each entry an `id_norma` + a coded `relacion` (DEROGA/MODIFICA/AMPLÍA/…) (JSON) |
| `GET /id/{id}/texto` | consolidated text as `<bloque tipo="…">` elements; preceptos carry per-revision `<version>` children (**XML only**) |

Content negotiation is via the `Accept` header. The list / `metadatos` /
`analisis` serve `application/json`; `texto` serves only `application/xml`
(`Accept: application/json` there returns HTTP 400 "No soportado ningún mime
type"). The importer therefore requests JSON for metadata and XML for text.

Sample list item (`GET /…/legislacion-consolidada`):

```jsonc
{ "identificador":"BOE-A-2021-6945", "rango":{"codigo":"1300","texto":"Ley"},
  "fecha_disposicion":"20210428", "fecha_vigencia":"20210430",
  "fecha_actualizacion":"20260527T082330Z", "vigencia_agotada":"N",
  "estado_consolidacion":{"codigo":"3","texto":"Finalizado"},
  "url_eli":"https://www.boe.es/eli/es/l/2021/04/28/6",
  "titulo":"Ley 6/2021, de 28 de abril, …" }
```

### License

- **Spanish normative acts are not objects of copyright** — art. 13 of the
  *Texto refundido de la Ley de Propiedad Intelectual* excludes laws,
  regulations, and the official texts of public bodies.
- BOE participates in the national open-data programme; reuse of BOE
  public-sector information is permitted under its open-data terms, implementing
  Spain's *reutilización de la información del sector público* (Ley 37/2007, as
  amended by Ley 18/2015).
- The `datos abiertos` API is the **official sanctioned machine channel**; we
  hit it directly and do **not** crawl the `buscar/act.php` human UI.

> **Open point (verify before public redistribution):** confirm the explicit
> reuse notice / attribution wording on the BOE `datos abiertos` portal before
> publishing a prebuilt ES dataset (ADR-0006). The acts themselves are freely
> reusable per art. 13 regardless; we attribute the source.

Attribution: **Agencia Estatal Boletín Oficial del Estado (BOE)**.

## Decision

Spain ingestion pulls from the **BOE open-data API** (JSON metadata + XML
text), not scraped pages. The ES importer:

1. pages the consolidated-legislation list (`?limit=&offset=`) until a short
   page signals the end;
2. for each norm fetches `metadatos` + `analisis` (+ `texto` when article text
   is requested);
3. maps each norm into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: the BOE identifier `BOE-A-2021-6945` → `eli:id_local`. Work URI
  uses `Country="es"`, `Year` parsed from the identifier, `Number` = the full
  identifier (globally unique). `SourceURL` = `url_eli` (falling back to the
  consolidated HTML URL).
- **Type slug** (`eli:type_document`): mapped from `rango.texto`, with a title
  override for codes (`código` → `codigo`). `Ley`→`ley`, `Ley Orgánica`→
  `ley-organica`, `Real Decreto`→`real-decreto`, etc.; ASCII-folded fallback.
- **As-of date** (`eli:version_date`, MANDATORY): `fecha_actualizacion` — the
  timestamp of the last consolidation update, BOE's cleanest "consolidated as
  of" signal — falling back to `fecha_disposicion`. `fecha_vigencia` →
  `eli:first_date_entry_in_force`.
- **Status**: `estatus_derogacion`/`vigencia_agotada` (`S` → repealed/no longer
  in force, `N` → in force); unknown otherwise.
- **Relations** (`analisis` referencias): `DEROGA`→`eli:repeals` (inbound
  `eli:repealed_by`), `MODIFICA`→`eli:amends` (inbound `eli:amended_by`), all
  other outbound relations (`AMPLÍA`, `CITA`, `DESARROLLA`, …)→`eli:cites`.
  Targets resolve to lex work URIs from each entry's `id_norma` with the generic
  `norma` type slug (a bare reference does not carry the target's `rango`).
  Inbound non-amend/repeal relations are not modelled in v1.
- **Articles**: each `<bloque tipo="precepto">` becomes one `lex:Article`; its
  `titulo` is the label, a leading numeral (when present) the number, and the
  text of its **latest `<version>`** child (the current wording) → `lex:text`.
  Preamble/signature blocks are skipped. Labels carry `@es`; `eli:language` uses
  the SPA authority URI.

## Consequences

- Legally clean (acts non-copyrightable), API-sanctioned, no HTML crawling.
- BOE consolidated legislation is the cleanest match yet for the v1 "current
  consolidated expression" model.
- **Politeness**: the collection holds tens of thousands of norms; the importer
  rate-limits (default 5 req/s), backs off on 429/5xx, and supports a `-limit`
  for incremental runs.
- Sub-article granularity (apartados/letras) is kept inside `lex:text` for v1;
  finer structure is additive later, no schema change. Point-in-time revisions
  (the per-bloque `<version>` history is already in the payload) and inbound
  `posteriores` edges are out of scope for v1 (store current text).
- No network in tests — real `list`/`metadatos`/`analisis`/`texto` responses are
  captured once under `es/scripts/boe/testdata/` (the `texto` fixture trimmed to
  two precepto blocks).
