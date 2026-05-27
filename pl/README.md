# Poland (`pl`)

Scrapers and configuration for ingesting Polish legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the official Sejm ELI API (NOT scraping)

Poland publishes a **native ELI API** run by the Kancelaria Sejmu, covering the
official journals **Dziennik Ustaw (`DU`)** and **Monitor Polski (`MP`)**.
Because ELI is the backbone of our ontology, the mapping is close to 1:1. We
hit the API directly and never crawl the human site. See
[ADR-0012](../docs/adr/0012-pl-ingest-sejm-eli-api.md).

Base: `https://api.sejm.gov.pl/eli/acts`

| Endpoint | Contents |
|----------|----------|
| `GET /{pub}` | publisher info incl. list of available `years` |
| `GET /{pub}/{year}` | paginated act list (`?limit=&offset=`) |
| `GET /{pub}/{year}/{pos}` | act detail — `type`, `status`, `inForce`, dates |
| `GET /{pub}/{year}/{pos}/references` | amends / repeals / legal-basis edges |
| `GET /{pub}/{year}/{pos}/struct` | document tree (articles = `type:"arti"`) |
| `GET /{pub}/{year}/{pos}/text.html` | full consolidated text |

Each act's ELI (e.g. `DU/2024/1984`) maps to the human, RDFa-annotated page
`https://eli.gov.pl/eli/DU/2024/1984` — stored as `lex:sourceURL`.

## Legal status & license

- Polish **normative acts are not objects of copyright** (art. 4 *ustawy o
  prawie autorskim i prawach pokrewnych*); reuse is further covered by the
  *ustawa o ponownym wykorzystywaniu informacji sektora publicznego*.
- No explicit API terms-of-use document was located — the acts are freely
  reusable regardless; **verify** for an explicit `regulamin` before publishing
  a prebuilt PL dataset (see ADR-0012, "Open point").
- We attribute **Kancelaria Sejmu / api.sejm.gov.pl** and preserve
  `lex:sourceURL` per record.

## Act-type mapping (PL → ELI `eli:type_document` slug)

| PL act type | slug | note |
|-------------|------|------|
| Ustawa | `ustawa` | a law |
| Ustawa (title contains *Kodeks*) | `kodeks` | a code |
| Rozporządzenie | `rozporzadzenie` | regulation |
| Obwieszczenie | `obwieszczenie` | announcement (often a consolidated text) |
| Uchwała | `uchwala` | resolution |
| Zarządzenie | `zarzadzenie` | order |
| Umowa międzynarodowa | `umowa-miedzynarodowa` | international agreement |
| *(other)* | ASCII-folded slug of the type | fallback |

Identity: work `Number` is `<publisher>/<pos>` (e.g. `DU/1984`) with `Year` from
the ELI; `eli:id_local` is the full ELI (`DU/2024/1984`).

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A single year of Dziennik Ustaw, with article text:
go run ./pl/scripts/import -out pl/data/graph -publishers DU -from 1964 -to 1964 -articles

# Everything (DU + MP, all years) — large, rate-limited crawl:
go run ./pl/scripts/import -out pl/data/graph -articles
```

Flags: `-out`, `-base`, `-ua`, `-publishers DU,MP`, `-from`, `-to`, `-articles`,
`-rps` (request rate limit, default 5/s).

- `scripts/eli/` — pure, offline parser + mapper: list / detail / references /
  struct / `text.html` → `schema.Act`. Golden-tested on committed real fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each act's `changeDate`
(falling back to `promulgation`); status from the `inForce` enum; amends /
repeals / cites edges from the `references` endpoint.

## Status

✅ Metadata + references + article-text pass works end-to-end (identity, title,
version date, status, source URL, amends/repeals/cites edges, `lex:Article`s).
🚧 Next: sub-article granularity (§/pkt/lit.); point-in-time revisions;
incremental updates via `GET /eli/changes/acts`; then the MCP server + search
index (country-agnostic, shared with UA).
