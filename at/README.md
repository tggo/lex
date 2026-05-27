# Austria (`at`)

Scrapers and configuration for ingesting Austrian federal legislation into the
`lex` RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the official RIS OGD API (NOT scraping)

Austria's **Rechtsinformationssystem des Bundes (RIS)**, run by the
**Bundeskanzleramt (BKA)**, publishes a versioned **OGD REST API**. We ingest
the **consolidated federal-law** application *Bundesnormen* /
"Bundesrecht konsolidiert" (`Applikation=BrKons`) via the `Bundesrecht` route.
We hit the API directly and never crawl the human site. See
[ADR-0023](../docs/adr/0023-at-ingest-ris-ogd-api.md).

Base: `https://data.bka.gv.at/ris/api/v2.6/Bundesrecht`

| Endpoint | Contents |
|----------|----------|
| `GET /Bundesrecht?Applikation=BrKons&Gesetzesnummer={gn}&DokumenteProSeite=OneHundred&Seitennummer={n}` | all § documents of one law (paginated) |
| `GET …/Dokumente/Bundesnormen/{NOR}/{NOR}.xml` | one §'s content body (also `.html`, `.rtf`, `.pdf`) |

### Data model

RIS is **not** a 1:1 ELI API. In the consolidated federal-law application **each
§ (paragraph) is a separate "Norm" document** (a `NOR…` id). All documents of one
law share a stable **`Gesetzesnummer`** (the law's work id), and a **`§ 0` head
document** carries the title and Stammnorm metadata. So:

- one **`Gesetzesnummer` = one `schema.Act`**;
- each non-head **§ document = one `schema.Article`** (`lex:text` from its XML body).

## Legal status & license

- RIS open data is published on **data.gv.at** under **Creative Commons
  Attribution 4.0 International (CC BY 4.0)** — reuse (incl. commercial) is
  permitted **with attribution**. Only the Bundesgesetzblatt wording is legally
  binding.
- We attribute **RIS / Bundeskanzleramt (data.bka.gv.at)** and preserve
  `lex:sourceURL` per record.

## Act-type mapping (AT `BrKons.Typ` → ELI `eli:type_document` slug)

| RIS `Typ` | slug | note |
|-----------|------|------|
| BG | `bundesgesetz` | federal law |
| (title contains *Gesetzbuch*) | `gesetzbuch` | a code (override) |
| BVG | `bundesverfassungsgesetz` | constitutional law |
| V | `verordnung` | regulation |
| K | `kundmachung` | promulgation |
| VBG / Vereinbarung | `vereinbarung` | agreement |
| *(other / empty)* | ASCII-folded slug of `Typ` (`norm` if empty) | fallback |

Identity: work `Number` = `eli:id_local` = the **`Gesetzesnummer`**; `Year` from
`StammnormBgblnummer` (`"727/1990"` → 1990).

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A single law, with article (§) text — e.g. the ABGB (Gesetzesnummer 10001622):
go run ./at/scripts/import -out at/data/graph -gn 10001622 -articles

# Several laws at once:
go run ./at/scripts/import -out at/data/graph -gn 10007061,10001622 -articles
```

Flags: `-out`, `-base`, `-ua`, `-gn` (comma-separated Gesetzesnummer ids),
`-articles`, `-rps` (request rate limit, default 5/s).

- `scripts/ris/` — pure, offline parser + mapper: search result + § content XML
  → `schema.Act`. Golden-tested on committed real fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) is the latest per-document `Geaendert`
(falling back to the head's `Inkrafttretensdatum`); status from the presence of
an `Ausserkrafttretensdatum` (repealed) vs. `Inkrafttretensdatum` (in force).

## Status

✅ Metadata + article-text pass works end-to-end (identity, title, version date,
in-force/repealed status, `eli:date_no_longer_in_force`, source URL,
`lex:Article`s).
🚧 Next: relation edges (amends/repeals/cites) — RIS gives only free-text BGBl.
references, so they need a BGBl→Gesetzesnummer resolution step; law discovery
(enumerate all `Gesetzesnummer` rather than supplying them); sub-paragraph
granularity (Abs./Z/lit.); point-in-time revisions; then the MCP server + search
index (country-agnostic, shared with UA/PL).
