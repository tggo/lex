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
                                      goRDFlib + Badger triplestore
                                       + decoupled search index (FTS5)
                                                   │
                                                   ▼
                                          lex MCP server  ◀── Claude Code
```

- **Storage / linking**: RDF triplestore via
  [`goRDFlib`](https://github.com/tggo/goRDFlib) (Badger backend), modelled with
  the [ELI](https://eur-lex.europa.eu/eli) vocabulary so amendments, repeals, and
  citations are first-class graph edges queryable with SPARQL.
- **Search**: a decoupled sibling index (SQLite FTS5 to start) over act titles
  and article text.
- **Distribution**: build the database yourself with the scrapers, or download
  a prebuilt one from GitHub Releases.

## Quick start

```bash
# 1. Build the Ukraine dataset (or download a prebuilt one from Releases).
#    A dataset is a directory holding graph/ (Badger) + index.fts (full text):
go run ./ua/scripts/import -out ua/data            # metadata
go run ./ua/scripts/import -out ua/data -articles  # + article text (slower)

# 2. Build and run the MCP server over that dataset:
go build -o lex ./cmd/lex
./lex -data ua/data
```

Then register `lex` as a stdio MCP server in your client (e.g. Claude Code).
It exposes:

- `search_laws(query, limit)` — full-text search; returns hits with an `act_uri`.
- `get_act(uri)` — metadata (title, **as-of date**, in-force status, source) + articles.
- `get_article(act_uri, number)` — a single article.
- `list_amendments(uri)` — `amends` / `amended_by` / `repeals`.
- `find_related(uri)` — `cites` / `consolidates`.

## Add your country

Write a scraper under `./<cc>/scripts` that emits RDF conforming to
[`docs/ontology.md`](docs/ontology.md). That is the entire integration — the
server is country-agnostic. PRs welcome.

## Legal note

Legislative texts of Ukraine are not objects of copyright, and the source data
is published as **open data under CC BY 4.0**. `lex` redistributes texts with
attribution to the official source (`data.rada.gov.ua` / Verkhovna Rada for
Ukraine). We use official open-data exports, not website scraping. See each
country's README.

## License

Apache-2.0 (code). Legislative texts retain their public-domain / official
status from the source.
