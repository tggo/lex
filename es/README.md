# Spain (`es`)

Scrapers and configuration for ingesting Spanish legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the official BOE open-data API (NOT scraping)

Spain publishes a machine-readable open-data API run by the **Agencia Estatal
Boletín Oficial del Estado (BOE)**, including a **consolidated-legislation**
collection (*Legislación Consolidada*) — the official, continuously-updated
texts of state norms. We hit the API directly and never crawl the human site.
See [ADR-0021](../docs/adr/0021-es-ingest-boe-open-data.md).

Base: `https://www.boe.es/datosabiertos/api/legislacion-consolidada`

| Endpoint | Contents |
|----------|----------|
| `GET /` | paginated norm list (`?limit=&offset=`), JSON or XML |
| `GET /id/{id}/metadatos` | norm metadata — `rango`, dates, consolidation state (JSON) |
| `GET /id/{id}/analisis` | `referencias` — amends / repeals / cites edges (JSON) |
| `GET /id/{id}/texto` | full consolidated text as `<bloque>` elements (**XML only**) |

Content negotiation is via the `Accept` header: `metadatos`/`analisis`/the list
serve `application/json`; `texto` serves `application/xml` (JSON is not
supported there). Each norm's identifier (e.g. `BOE-A-2021-6945`) maps to the
human consolidated page `https://www.boe.es/buscar/act.php?id=…` and an ELI URL
(`url_eli`, e.g. `https://www.boe.es/eli/es/l/2021/04/28/6`), stored as
`lex:sourceURL`.

## Legal status & license

- Spanish **normative acts are not objects of copyright** (art. 13 of the
  *Texto refundido de la Ley de Propiedad Intelectual* excludes laws,
  regulations, and their official texts).
- Reuse of BOE public-sector information is permitted under the BOE open-data
  terms (the *datos abiertos* programme), implementing Spain's
  *reutilización de la información del sector público* (Ley 37/2007 / 18/2015).
- We attribute **Agencia Estatal Boletín Oficial del Estado (BOE)** and preserve
  `lex:sourceURL` per record. Re-confirm the explicit reuse notice before
  publishing a prebuilt ES dataset (see ADR-0021, "Open point").

## Act-type mapping (BOE `rango` → ELI `eli:type_document` slug)

| BOE rango | slug | note |
|-----------|------|------|
| Ley | `ley` | a law |
| Ley Orgánica | `ley-organica` | organic law |
| Real Decreto | `real-decreto` | royal decree |
| Real Decreto-ley | `real-decreto-ley` | decree-law |
| Real Decreto Legislativo | `real-decreto-legislativo` | legislative decree |
| Decreto | `decreto` | decree |
| Orden | `orden` | ministerial order |
| Resolución | `resolucion` | resolution |
| Constitución | `constitucion` | constitution |
| *(title contains "código")* | `codigo` | a code (overrides rango) |
| *(other)* | ASCII-folded slug of the rango | fallback (`norma` if empty) |

Identity: work `Number` is the full BOE identifier (e.g. `BOE-A-2021-6945`) with
`Year` parsed from it; `eli:id_local` is the same identifier. Referenced norms
(in `analisis`) resolve to work URIs with the generic `norma` slug, since a bare
reference does not carry the target's `rango` — a stable, reconstructible URI
without an extra fetch.

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# First 50 consolidated norms, with article text:
go run ./es/scripts/import -out es/data/graph -limit 50 -articles

# Everything — large, rate-limited crawl:
go run ./es/scripts/import -out es/data/graph -articles
```

Flags: `-out`, `-base`, `-ua`, `-limit`, `-articles`, `-rps` (request rate
limit, default 5/s).

- `scripts/boe/` — pure, offline parser + mapper: list / metadatos / analisis /
  texto → `schema.Act`. Golden-tested on committed real fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each norm's
`fecha_actualizacion` — the timestamp of the last consolidation update, the
cleanest "consolidated as of" signal BOE exposes — falling back to
`fecha_disposicion`. Status from `estatus_derogacion` / `vigencia_agotada`;
`fecha_vigencia` → `eli:first_date_entry_in_force`. Amends / repeals / cites
edges come from the `analisis` `referencias` block (`MODIFICA` → amends,
`DEROGA` → repeals, everything else inbound-of-this-norm → cites).

## Status

✅ Metadata + references + article-text pass works end-to-end (identity, title,
version date, status, source URL, amends/repeals/cites edges, `lex:Article`s
from `<bloque tipo="precepto">`).
🚧 Next: sub-article granularity (apartados/letras); point-in-time revisions
from the per-bloque `<version>` history; inbound `posteriores` amended_by/
repealed_by once target rangos are resolved; then the MCP server + search index
(country-agnostic, shared with UA/PL).
