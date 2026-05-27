# Ukraine (`ua`)

Scrapers and configuration for ingesting Ukrainian legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source

- **Primary**: `zakon.rada.gov.ua` — the Verkhovna Rada "Legislation of Ukraine"
  portal. Authoritative; covers laws, codes, presidential decrees, Cabinet of
  Ministers resolutions, and ministry orders. Acts have stable URLs
  (`/laws/show/<id>`) and consolidated current text with a redaction date.
- **Open data**: `data.rada.gov.ua` — bulk datasets, useful for the act index /
  metadata seed before fetching full texts.

> ⚠️ Before scraping at scale: check `zakon.rada.gov.ua/robots.txt` and the
> portal's terms; rate-limit politely and cache. (P1 task — verify and record
> the allowed cadence here.)

## Legal status of the data

Under Ukrainian law, normative legal acts are **not** objects of copyright, so
redistributing their text is permitted. We attribute the official source and
preserve the source URL on every record (`lex:sourceURL`).

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

- `scripts/` — Go scrapers. Output RDF (Turtle) and/or write directly to the
  SQLite store under `data/`.
- `data/` — built artifacts (**gitignored**). Either built locally by the
  scrapers or downloaded from GitHub Releases.

## Status

🚧 Not yet implemented. P1: one act type end-to-end (index → fetch → parse →
emit ELI RDF → store → searchable via the server).
