# Finland (`fi`)

Scrapers and configuration for ingesting Finnish legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the official Finlex open data (NOT scraping)

Finland publishes its legislation through **Finlex open data**, a REST API run
by the Ministry of Justice (data produced by Edita Publishing). Statutes are
served as **Akoma Ntoso 3.0 XML** (the OASIS LegalDocML standard) carrying an
**ELI** alias and **ECLI** identifiers, plus a `finlex:` proprietary extension
for status, type, and relations. We hit the API directly and never crawl a
human UI. No authentication or registration is required. See
[ADR-0019](../docs/adr/0019-fi-ingest-finlex-open-data.md).

Base: `https://opendata.finlex.fi/finlex/avoindata/v1`

| Endpoint | Contents |
|----------|----------|
| `GET /akn/fi/act/statute-consolidated?limit=&page=` | paged list of consolidated ("ajantasa") statutes — `<AknXmlList><Results>` envelope wrapping AKN documents. `limit` is capped at 10; paging is by **1-based `page`** (the API silently ignores `offset` and always returns page 1). An out-of-range page returns HTTP 200 with an empty `<Results/>`. |
| `GET /akn/fi/act/statute-consolidated/{year}/{number}/fin@` | the full Finnish consolidated expression — metadata + body sections (§) |

Responses require `Accept: application/xml`. Each act's ELI alias (e.g.
`http://data.finlex.fi/eli/sd/2019/469/ajantasa`) is stored as `lex:sourceURL`
and `eli:id_local`.

## Legal status & license

- Finnish **normative acts are not objects of copyright** (Tekijänoikeuslaki
  404/1961, § 9: laws and decrees are not protected).
- The **Finlex open dataset is licensed CC BY 4.0**. Redistribution is fine
  **with attribution** to **Finlex / Ministry of Justice, Edita Publishing**.
- No authentication/registration is needed — the API is a public, no-auth
  channel (verified live, 2026-05-27). We attribute the source and preserve
  `lex:sourceURL` per record.

> Verify the published CC BY 4.0 terms (and required attribution wording) before
> publishing a prebuilt FI dataset — see ADR-0019, "Open point".

## Languages

Finland's statutes are published in Finnish (`fin`) and Swedish (`swe`). v1
ingests the **Finnish expression** (`LangTag "fi"`, `LangAlpha3 "FIN"`,
`eli:language` → FIN authority URI). Swedish expressions (`/swe@`) are a
straightforward additive next-phase pass.

## Act-type mapping (FI → ELI `eli:type_document` slug)

The `finlex:typeStatute refersTo` id (resolved via the document's `TLCConcept`
`showAs` label) maps to a slug:

| Finlex type concept | label (`showAs`) | slug |
|---------------------|------------------|------|
| `act` | Laki | `laki` |
| `decree` | Asetus | `asetus` |
| *(other)* | — | ASCII-folded slug of the label, else `saados` |

Identity: work `Number` is `"<year>/<position>"` (e.g. `2019/469`, the natural
Finlex citation written `469/2019`) with `Year` from `finlex:documentYear` (or
the work FRBRuri path); `eli:id_local` is the act's ELI alias.

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A handful of consolidated statutes, with section (§) text:
go run ./fi/scripts/import -out fi/data/graph -limit 50 -articles

# Everything (all consolidated statutes) — large, rate-limited crawl:
go run ./fi/scripts/import -out fi/data/graph -articles
```

Flags: `-out`, `-base`, `-ua`, `-limit`, `-articles`, `-rps` (request rate
limit, default 5/s).

- `scripts/akn/` — pure, offline parser + mapper: AKN list / consolidated
  expression XML → `schema.Act`. Golden-tested on committed real fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each expression's
`dateConsolidated` (falling back to its `dateIssued`, then the work's
`dateIssued`); status from `finlex:isInForce` / `finlex:noLongerInForce`;
`first_date_entry_in_force` from `finlex:dateEntryIntoForce`. Articles are the
body's `<section>` (§ "pykälä") nodes — heading + subsection text → `lex:text`.

## Relations

Relations come from the `<proprietary>` block:

- `finlex:amendedBy` → `eli:amended_by`
- `finlex:repealedBy` → `eli:repealed_by`
- `finlex:issuedUnderActs` → `eli:cites` (legal basis)

Each `<finlex:ref href="/akn/fi/act/statute/<year>/<number>">` is resolved to a
lex Resource URI. The target's precise type slug is unknown from the ref alone,
so a neutral `statute` slug is used; the target gains its precise type when that
act is itself ingested. **Next phase:** rewrite target slugs once the full
collection is loaded (a second pass over the store).

## Status

✅ Metadata + relations + section-text pass works end-to-end (identity, title,
version date, status, source URL, amended-by / repealed-by / cites edges,
`lex:Article`s per §).
🚧 Next: Swedish (`swe@`) expressions; precise relation-target type slugs (second
pass); sub-section granularity (momentti/kohta); point-in-time revisions; then
the MCP server + search index (country-agnostic, shared with UA/PL).
