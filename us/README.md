# United States — federal (`us`)

Scrapers and configuration for ingesting United States federal legislation (the
**United States Code**) into the `lex` RDF graph. See the root
[`README`](../README.md) and [`docs/ontology.md`](../docs/ontology.md) for the
contract these scrapers obey.

## Source — the official OLRC USLM XML bulk (NOT scraping)

The **Office of the Law Revision Counsel (OLRC)** of the U.S. House of
Representatives publishes the entire United States Code as **USLM (United States
Legislative Markup) XML**, in per-title bulk files, with no authentication. We
fetch the bulk files directly and never crawl a human UI. See
[ADR-0020](../docs/adr/0020-us-ingest-uslm-bulk.md).

Portal: `https://uscode.house.gov/download/download.shtml`

| Resource | Contents |
|----------|----------|
| `releasepoints/us/pl/<congress>/<law>/xml_usc<TT>@<congress>-<law>.zip` | one title's USLM XML (point-in-time release point) |
| `currentrelease.shtml` | the current release point |
| `resources/` | USLM schema + user guide |

Each per-title zip contains a single `usc<TT>.xml`. Structure: `<uslm>` →
`<meta>` (publication dates) + `<main><title>` → `<chapter>`* → `<section>`*.
The release-point zip URL the title was fetched from is stored as
`lex:sourceURL`.

## Legal status & license

- US Government legal edicts are **public domain**: **17 U.S.C. § 105** denies
  copyright to works of the U.S. Government, and the **government edicts
  doctrine** (*Georgia v. Public.Resource.Org*, 2020) puts the law itself
  outside copyright. The OLRC download page states the USLM files are not
  subject to copyright.
- No attribution is legally required; we nonetheless credit **Office of the Law
  Revision Counsel, U.S. House of Representatives** and preserve `lex:sourceURL`
  per record.

## Act-type mapping (US → ELI `eli:type_document` slug)

| US source | slug | note |
|-----------|------|------|
| United States Code title | `usc-title` | one codification; each title is one Act |

Finer typing (public laws, the CFR) is a later phase. Identity: work `Number` is
`title-<n>` (e.g. `title-1`) with `Year` from the release version date;
`eli:id_local` is `usc/title-<n>`. Each USC `<section>` becomes a `lex:Article`
keyed by its section number.

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A few titles from a specific release point, with article text:
go run ./us/scripts/import -out us/data/graph -titles 1,5,26 -release 119-4

# Everything (all 54 titles) — large, rate-limited crawl:
go run ./us/scripts/import -out us/data/graph
```

Flags: `-out`, `-base` (release-point dir URL), `-release` (tag in zip
filenames, e.g. `119-4`), `-ua`, `-titles` (comma-separated, empty = all 1..54),
`-rps` (request rate limit, default 2/s).

- `scripts/uslm/` — pure, offline parser + mapper: USLM per-title XML →
  `schema.Act`. Golden-tested on a committed real-shaped fixture.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + unzip + build
  + write to the Badger store; tested end-to-end via `httptest` serving the
  fixture zipped in-memory.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from the release's
`<meta><dcterms:modified>` (falling back to `dcterms:created`, then a section
`@startPeriod`); status from per-section USLM `@status`, collapsed to the title.

## Status

✅ Metadata + article-text pass works end-to-end (identity, title, version date,
status, source URL, `lex:Article`s per USC section).
🚧 Next: amendment/citation edges (parse `<sourceCredit>`/`<notes>` and resolve
to lex work URIs via `schema.ResourceURI`); sub-section granularity; the CFR via
the eCFR JSON API; then the MCP server + search index (country-agnostic, shared
with UA/PL).
