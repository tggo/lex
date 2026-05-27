# New Zealand (`nz`)

Scrapers and configuration for ingesting New Zealand legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the official PCO XML export (NOT scraping, NOT the keyed API)

New Zealand legislation is published by the **Parliamentary Counsel Office
(PCO)** on the New Zealand Legislation website, with every act available **as
XML**. We hit the public, no-auth XML directly and never crawl the human HTML.
We deliberately do **not** use the keyed Developer API
(`api.legislation.govt.nz`, which needs an emailed API key). See
[ADR-0025](../docs/adr/0025-nz-ingest-legislation-govt-nz.md).

Base: `https://www.legislation.govt.nz`

| Endpoint | Contents |
|----------|----------|
| `GET /legislation-index.xml` | act listing (category, year, number, title) |
| `GET /act/{category}/{year}/{number}/latest/whole.xml` | full consolidated act XML — `<cover>` metadata + `<body>` provisions |

The same act's human page is `…/latest/whole.html`, stored as `lex:sourceURL`.
A bulk XML directory (all versions + DTDs) is also mirrored on `data.govt.nz`.

> The website sits behind a bot-protection layer that answers automated clients
> lacking a browser session with an HTTP 202 interstitial. That is rate-limiting
> / bot mitigation, **not** authentication — no key is needed for the public
> XML. Be a polite client (the importer rate-limits and backs off).

## Legal status & license

- New Zealand **Acts, Bills, Legislative Instruments and Supplementary Order
  Papers are not subject to Crown copyright** (Copyright Act 1994, s 27).
- The PCO publishes the XML so it can be freely reused, under **NZGOAL** —
  i.e. **Creative Commons Attribution 4.0 (CC BY 4.0)** for the dataset.
- We attribute **New Zealand Parliamentary Counsel Office / legislation.govt.nz**
  and preserve `lex:sourceURL` per record.

## Act-type mapping (NZ → ELI `eli:type_document` slug)

| NZ category / title | slug | note |
|---------------------|------|------|
| Public (`public`) | `public-act` | a public act |
| title contains *Code* | `code` | a code |
| Local (`local`) | `local-act` | a local act |
| Private (`private`) | `private-act` | a private act |
| Imperial (`imperial`) | `imperial-act` | inherited imperial act |
| Provincial (`provincial`) | `provincial-act` | provincial act |
| *(other)* | `<ascii-slug>-act` | fallback |

Identity: work `Number` is the PCO zero-padded number (e.g. `0038`) with `Year`
from the listing; `eli:id_local` is the canonical `act/<category>/<year>/<number>`.

## Language

All NZ legislation is in English: `LangTag = "en"`, `LangAlpha3 = "ENG"`.

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A single year, with section text:
go run ./nz/scripts/import -out nz/data/graph -from 1990 -to 1990 -articles

# Everything in the index — rate-limited crawl:
go run ./nz/scripts/import -out nz/data/graph -articles
```

Flags: `-out`, `-base`, `-list`, `-ua`, `-from`, `-to`, `-articles`,
`-rps` (request rate limit, default 2/s).

- `scripts/lenz/` — pure, offline parser + mapper: listing / `whole.xml`
  (cover + provision tree) → `schema.Act`. Golden-tested on committed real
  fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from each reprint's
`<version-date>` (falling back to `<commencement>`, then `<assent-date>`);
status from a `<repeal-date>`/`repealed` flag (else in force).

## Status

✅ Metadata + section text pass works end-to-end (identity, title, version
date, status, source URL, `lex:Article`s for sections).
🚧 Next: amends/repeals/cites relation edges (deferred — not in the plain
`whole.xml` export); sub-section granularity; point-in-time revisions; then the
MCP server + search index (country-agnostic, shared with UA/PL).
