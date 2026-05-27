# lex

**Open legislation as an MCP knowledge graph.**

`lex` is an open-source [MCP](https://modelcontextprotocol.io) server that puts
a country's laws and subordinate legislation at the fingertips of MCP clients
(Claude Code, Claude Desktop, …) — searchable, retrievable, and linked as a
graph (which act amends, repeals, or cites which).

Each country lives in its own directory (`./ua` for Ukraine first; `./jp`,
`./uk`, `./us`… later). A country directory ships Go scrapers that pull the
official legal source and emit RDF; the shared `lex` server indexes that RDF
and serves it over MCP. The goal: anyone, anywhere, can run the laws of their
own country locally and query them with an AI assistant.

## Status

🚧 Early design / scaffolding. See [`docs/prd`](docs/prd) for the vision and
[`docs/adr`](docs/adr) for architecture decisions.

## How it works

```
official source ──scraper (Go, per country)──▶ RDF (ELI ontology)
                                                   │
                                                   ▼
                                      goRDFlib + SQLite triplestore
                                          + FTS5 full-text index
                                                   │
                                                   ▼
                                          lex MCP server  ◀── Claude Code
```

- **Storage / linking**: RDF triplestore via
  [`goRDFlib`](https://github.com/tggo/goRDFlib), modelled with the
  [ELI](https://eur-lex.europa.eu/eli) vocabulary so amendments, repeals, and
  citations are first-class graph edges queryable with SPARQL.
- **Search**: SQLite FTS5 over act titles and article text.
- **Distribution**: build the database yourself with the scrapers, or download
  a prebuilt one from GitHub Releases.

## Quick start

> Not yet runnable — scaffolding in progress.

```bash
go build ./cmd/lex
# Build the Ukraine dataset (or download a prebuilt .db from Releases):
go run ./ua/scripts/...
# Point your MCP client at the lex binary.
```

## Add your country

Write a scraper under `./<cc>/scripts` that emits RDF conforming to
[`docs/ontology.md`](docs/ontology.md). That is the entire integration — the
server is country-agnostic. PRs welcome.

## Legal note

Legislative texts of Ukraine are not objects of copyright. `lex` redistributes
*texts*, with attribution to the official source. Scrapers respect each
source's `robots.txt` and rate limits. See each country's README.

## License

Apache-2.0 (code). Legislative texts retain their public-domain / official
status from the source.
