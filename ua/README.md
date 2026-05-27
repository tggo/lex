# Ukraine (`ua`)

Scrapers and configuration for ingesting Ukrainian legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — official open data (NOT scraping)

`zakon.rada.gov.ua` disallows bots in `robots.txt` (`User-Agent: * →
Disallow: /`), so we do **not** scrape the website. Instead we use the
Verkhovna Rada's **open data** export at `data.rada.gov.ua`, which publishes the
"Законодавство України" system in machine-readable form, rebuilt **hourly**.
See [ADR-0009](../docs/adr/0009-ua-ingest-official-open-data.md).

### Primary-acts dataset (`perv`)

Base: `https://data.rada.gov.ua/ogd/zak/perv/`

| File | Contents |
|------|----------|
| `meta.json` | dataset descriptor (pubDate / lastBuildDate) |
| `cards.{csv,json,xml}` (+ `.zip`) | act cards / metadata (~7 MB) |
| `texts.{csv,json,xml}` (+ `.zip`) | text index (~797 KB) |
| `text/texts.zip` | full act texts as HTML (~334 MB, ~3043 files) |
| `…/ogd/zak/laws/data/csv/perv1.txt` | **active** act IDs (e.g. `435-15`) |
| `…/ogd/zak/laws/data/csv/perv0.txt` | **inactive** (repealed) act IDs |
| `perv_codes.txt` | code reference |

Each act `id` (e.g. `435-15`) maps to the human page
`https://zakon.rada.gov.ua/laws/show/<id>` — stored as `lex:sourceURL`.

## Legal status & license

- Ukrainian normative legal acts are **not** objects of copyright.
- The open dataset is published under **CC BY 4.0** (attribution required),
  backed by CMU Resolutions №835/№867 and the Law "On Access to Public
  Information" — free to copy, redistribute, and use commercially **with a
  reference to the source**.
- We therefore attribute **data.rada.gov.ua / Verkhovna Rada (CC BY 4.0)** in
  this README, in dataset Releases, and preserve `lex:sourceURL` per record.

## Act-type mapping (UA → ELI `eli:type_document` slug)

| UA act type | slug | example |
|-------------|------|---------|
| Конституція | `konstytutsiya` | Конституція України |
| Кодекс | `kodeks` | Цивільний кодекс України |
| Закон | `zakon` | Закон «Про…» |
| Постанова КМУ | `postanova-kmu` | Постанова Кабінету Міністрів |
| Указ Президента | `ukaz-prezydenta` | Указ Президента України |
| Наказ міністерства | `nakaz` | Наказ Міністерства… |

## Directories

- `scripts/` — Go importer. Downloads the OGD datasets, parses act cards +
  HTML bodies into article structure, emits RDF (Turtle) and/or writes directly
  to the SQLite store under `data/`.
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Status

🚧 Not yet implemented. P1: one act end-to-end — fetch `cards.json` + the act's
HTML body from `texts.zip`, set status from `perv1/perv0.txt`, parse into
articles, emit ELI RDF per [`../docs/ontology.md`](../docs/ontology.md), store,
and confirm it is searchable via the server.
