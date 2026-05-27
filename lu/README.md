# Luxembourg (`lu`)

Scrapers and configuration for ingesting Luxembourg legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the official Legilux SPARQL endpoint (NOT scraping)

Luxembourg (Service central de législation, **Legilux**) publishes its
legislation as Linked Open Data using the **JOLux + ELI** models. Because ELI is
the backbone of our ontology, the mapping is close to 1:1. We query the SPARQL
endpoint directly and never crawl the human site. See
[ADR-0018](../docs/adr/0018-lu-ingest-legilux-sparql.md).

Endpoint: `https://data.legilux.public.lu/sparqlendpoint` (no authentication).
Results requested as `application/sparql-results+json`.

The data model (JOLux, FRBR-shaped):

| Node | Carries |
|------|---------|
| `jolux:Work` (a `jolux:Act`) | `jolux:typeDocument`, `jolux:dateDocument`, `jolux:dateEntryInForce`, `jolux:dateNoLongerInForce`, `jolux:inForceStatus`, `jolux:isRealizedBy` → Expression |
| `jolux:Expression` (one per language, `…/fr`) | `jolux:title`, `jolux:language` |
| relation edges (Work→Work) | `jolux:modifies`, `jolux:repeals`, `jolux:cites`, `jolux:consolidates` |

Each act's ELI work URI (e.g.
`http://data.legilux.public.lu/eli/etat/leg/rgd/2020/03/18/a165/jo`) is stored
verbatim as `lex:sourceURL` — it resolves to the act's landing page.

## Legal status & license

- Luxembourg **normative acts are open data**, published by Legilux under
  **CC BY** — redistribution is fine **with attribution**.
- Attribution: **Legilux / État du Grand-Duché de Luxembourg**, with the source
  work URI preserved per record (`lex:sourceURL`).
- The SPARQL endpoint is the **official, no-auth machine channel**; the
  `data.legilux.public.lu/sparql` path is a JS web UI (Casemates) — we query
  `…/sparqlendpoint`, we do **not** crawl a UI.

## Language

Legilux is **multilingual** (French `fr`/`FRA`, German `de`, Luxembourgish
`lb`). v1 ingests the **French** expression as primary (`LangTag "fr"`,
`LangAlpha3 "FRA"`); other-language expressions are an additive next phase.

## Act-type mapping (LU `jolux:typeDocument` → ELI `eli:type_document` slug)

The resource-type authority code (the trailing segment of the `typeDocument`
URI) maps to a slug:

| LU type code | slug | note |
|--------------|------|------|
| `LOI` | `loi` | a law |
| `RGD` | `rgd` | règlement grand-ducal |
| `ARGD` | `argd` | arrêté grand-ducal portant règlement |
| `AGD` | `agd` | arrêté grand-ducal |
| `AMIN` | `amin` | arrêté ministériel |
| `RMIN` | `rmin` | règlement ministériel |
| `A` | `arrete` | arrêté |
| `REG_UE` | `reg-ue` | EU regulation |
| `DIR_UE` | `dir-ue` | EU directive |
| *(work URI under `/code/`)* | `code` | a consolidated code |
| *(other)* | lower-cased authority code | fallback |

Identity: work `Number` (and `eli:id_local`) is the path of the work URI after
the Legilux ELI base, e.g. `etat/leg/rgd/2020/03/18/a165/jo` — globally unique;
the schema's per-segment escaping keeps the slashes intact. `Year` comes from
`jolux:dateDocument` (falling back to the year segment of the URI).

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# First 100 acts (smoke test):
go run ./lu/scripts/import -out lu/data/graph -limit 100

# Everything — large, rate-limited crawl:
go run ./lu/scripts/import -out lu/data/graph
```

Flags: `-out`, `-endpoint`, `-ua`, `-limit`, `-rps` (request rate limit,
default 2/s), `-articles` (also fetch each act's French HTML full text and
parse article-level text).

### Full text (`-articles`)

The act metadata comes from SPARQL; the article text comes from each act's
**French HTML manifestation**, which Legilux serves from its filestore at
`<workURI>/fr/html` (a stable, no-auth URL — content negotiation redirects to
the filestore `.html` file). The body is XHTML in which each article is a
`<div class="richtext_article" id="art_<num>">` carrying a
`<p class="richtext_num_article">Art. N.</p>` heading and the article body;
quoted articles inserted by an amendment are nested with an empty `id` and stay
part of their host article's text. Acts that have no HTML embodiment (notably
old scanned Mémorial pages published only as PDF) return the site's Angular
shell, which has no `richtext_article` elements and parses to zero articles —
those acts stay metadata-only. Article fetch/parse is best-effort: a per-act
failure is logged and skipped, never fatal.

- `scripts/legilux/` — pure, offline parser + mapper: SPARQL JSON results
  (acts page / relations) → `schema.Act`. Golden-tested on committed real
  SPARQL results captured from the live endpoint.
- `scripts/importer/` — query (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each act's
`jolux:dateDocument`; status from `jolux:inForceStatus`; amends / repeals /
cites / consolidates edges from the `modifies` / `repeals` / `cites` /
`consolidates` predicates.

## Status

✅ Metadata + relations pass works end-to-end (identity, French title, version
date, status, source URL, amends/repeals/cites/consolidates edges).
✅ **Article-level full text** via `-articles` (French HTML manifestation,
`<workURI>/fr/html`); legislative acts (LOI/RGD/…) carry per-article text,
PDF-only acts stay metadata-only.
🚧 Next: German & Luxembourgish expressions; point-in-time consolidations as
revision nodes; then the MCP server + search index (country-agnostic, shared
with UA).
