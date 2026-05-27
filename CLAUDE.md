# lex — open legislation as an MCP knowledge graph

`lex` is an open-source MCP server that exposes the laws and subordinate
legislation of a country to MCP clients (Claude Code, etc.) for search,
retrieval, and relationship traversal. Ukraine (`./ua`) is the first country;
the architecture is country-agnostic so `./jp`, `./uk`, `./us`… plug in later.

## Core architecture: ingestion is separated from serving

Two halves, joined by one contract — the **ELI/RDF ontology**:

1. **Country-specific ingestion** (`./<cc>/scripts`, Go): scrapers for that
   country's official source. Their *only* output is RDF triples conforming to
   the shared ontology (see `docs/ontology.md`). They know nothing about search
   or serving.
2. **Country-agnostic serving** (`cmd/lex`, `internal/`): one Go binary that
   loads the RDF graph and exposes MCP tools. It knows nothing about any
   specific country — it queries whatever triples exist.

> The ontology is the contract. Adding a country = writing a scraper that emits
> conformant RDF. Never put country-specific logic in `cmd/` or `internal/`.

## Stack (decided — see ADRs)

- **Language**: Go everywhere (scrapers + server). Single static binary.
- **Storage**: RDF triplestore via [`goRDFlib`](https://github.com/tggo/goRDFlib)
  with the **SQLite backend**. The SQLite file *is* the distributable artifact.
- **Ontology**: [ELI](https://eur-lex.europa.eu/eli) (European Legislation
  Identifier) as the backbone, FRBR work/expression layering for versioning.
- **Structural queries**: SPARQL 1.1 (amendments, citations, repeals).
- **Full-text search**: SQLite **FTS5** table in the same file, keyed by act /
  article URI (SPARQL text matching is too weak for real search).
- **Distribution**: build locally OR download a prebuilt `.db` from GitHub
  Releases. Data is **never committed** to git (`./*/data/` is gitignored).

## Repository layout

```
docs/{prd,adr}/      project docs (English) — PRD = what/why, ADR = decisions
docs/ontology.md     THE CONTRACT: the ELI/RDF shape every scraper must emit
cmd/lex/             the MCP server binary
internal/schema/     Go types + ontology vocab (namespaces, predicates, URIs)
internal/store/      goRDFlib + SQLite wiring, FTS5 index, queries
internal/mcp/        MCP protocol handlers / tool definitions
ua/                  Ukraine: README, scripts/ (scrapers), data/ (gitignored)
```

## MCP tools (target surface)

- `search_laws(query, filters)` — FTS5 over titles + article text.
- `get_act(id)` — full consolidated text + metadata of an act.
- `get_article(act_id, article)` — a single article.
- `list_amendments(act_id)` — what amends / has amended this act (SPARQL).
- `find_related(act_id)` — citations and cross-references (SPARQL).

## Versioning policy (v1)

Store the **current consolidated text** plus metadata: `eli:version_date`,
`eli:first_date_entry_in_force`, in-force/repealed status, and the source URL.
Always be able to answer "as of what date is this text". Full historical
redaction history is a later phase. **Never** return text without its as-of
date — a legal tool that hides staleness is harmful.

## Conventions

- All docs, code comments, identifiers, and ADRs/PRDs in **English**.
- Conversation with the maintainer may be in Ukrainian; docs stay English.
- Prefer a country's **official open-data export** over HTML scraping. Respect
  `robots.txt`; record the source, endpoints, and license in the country README
  and an ADR. For Ukraine: `data.rada.gov.ua` open data, CC BY 4.0 — NOT
  scraping `zakon.rada.gov.ua` (which disallows bots). See ADR-0009.
- Ukrainian normative acts are not objects of copyright; the open dataset is
  CC BY 4.0 — redistribution is fine **with attribution to the source**.
- One ADR per real decision; supersede rather than rewrite history.

## Build / run

```
go build ./cmd/lex            # build the server
go run ./ua/scripts/...       # run a Ukraine scraper -> ./ua/data/*.ttl|.db
```
