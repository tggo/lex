# PRD 0001 — lex: open legislation as an MCP knowledge graph

- **Status**: Draft
- **Date**: 2026-05-27
- **Owner**: maintainer

## Problem

Legislation is public but practically hard to use with AI assistants. Official
portals (e.g. Ukraine's `zakon.rada.gov.ua`) are searchable by humans but offer
no clean machine interface for an LLM to (a) find the right act, (b) read the
*current* text, and (c) follow the web of amendments and citations between acts.
LLMs answering legal questions from training memory are stale and unreliable.

## Vision

A free, open-source, local-first tool that any person can install to query the
laws of their country through an MCP client. The same codebase serves every
country; communities contribute a scraper per jurisdiction. Ukraine first.

## Users

- **Lawyers, paralegals, journalists, citizens** who want grounded answers about
  current law via an AI assistant.
- **Developers** building legal-tech on top of a structured legislation graph.
- **Contributors** who add their country by writing one scraper.

## Goals (v1)

1. Ingest Ukrainian legislation (laws, codes, Cabinet resolutions, presidential
   decrees, key ministry orders) from the official source into an RDF graph.
2. Serve it over MCP with: full-text search, act retrieval, article retrieval,
   amendment listing, related/cited-act traversal.
3. Always report the **as-of date** and in-force/repealed status of any text.
4. Ship a prebuilt database via GitHub Releases and a reproducible local build.
5. Make "add a country" a documented, self-contained task.

## Non-goals (v1)

- Full historical redaction history (only current consolidated text + metadata).
- Legal advice, interpretation, or case law / court decisions.
- Semantic / embedding search (keyword FTS first; semantic is a later layer).
- A web UI. lex is an MCP server; the client is the UI.

## Success criteria

- A user can ask Claude Code "what does Article X of the Civil Code of Ukraine
  say, and is it in force?" and get the current text with its as-of date.
- "What amended this law?" and "what does it cite?" return correct graph edges.
- A second country can be added without touching `cmd/` or `internal/`.

## Key risks

- **Staleness / correctness** of legal text → mitigated by mandatory as-of date
  and in-force status; clear "not legal advice" framing.
- **Source fragility** (HTML changes, rate limits) → polite scraping, snapshots.
- **Data volume** → SQLite artifact, never in git; Releases for prebuilt DBs.

## Phases

- **P0** (now): scaffolding, ontology contract, ADRs.
- **P1**: UA scraper → RDF for one act type end-to-end; store + FTS; one MCP tool.
- **P2**: full UA act-type coverage; all five MCP tools; prebuilt Release DB.
- **P3**: second country; historical versioning; optional semantic search.
