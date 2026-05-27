# ADR 0003 — RDF triplestore (goRDFlib + SQLite) as the store

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Legislation is fundamentally a graph: acts amend, repeal, consolidate, and cite
one another. The maintainer asked for RDF from the start so these links are
native, and pointed at [`goRDFlib`](https://github.com/tggo/goRDFlib).

Verified `goRDFlib` v0.1.9 (May 2026): full SPARQL 1.1 query + update, SHACL,
Turtle/N-Triples/JSON-LD, and **persistent SQLite and Badger backends**, with
100% W3C conformance on its test suites. Actively maintained (it is the
maintainer's own library — dogfooding is intended).

Alternatives considered: plain SQLite document store with a join table for
links (loses standard vocabularies/SPARQL, reinvents graph querying); Bleve
(search only, no graph); Cayley (graph DB but heavier, non-RDF-native query).

## Decision

Store data as RDF triples via `goRDFlib` with its **SQLite backend**. Model the
domain with the ELI vocabulary (see ADR-0005, `docs/ontology.md`). Use SPARQL
for structural queries (amendments, citations, repeals).

The single SQLite file is the unit of distribution (ADR-0006).

## Consequences

- Links between acts are first-class edges; "what amends X" is a SPARQL query,
  not bespoke join logic.
- Standard vocabularies (ELI) make the data interoperable beyond lex.
- Full-text search is *not* well served by SPARQL → see ADR-0004 (FTS5 layer).
- We take a dependency on a young (v0.x) library. Mitigation: it is the
  maintainer's, W3C-conformant, and the RDF data is portable (Turtle export) if
  we ever swap engines.
