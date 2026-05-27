# ADR 0005 — ELI vocabulary + FRBR layering for the data model

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

We need a vocabulary for the RDF graph. Rolling our own would be fast but
isolating and would force us to reinvent amend/repeal/cite/versioning semantics.

Candidates: **ELI** (European Legislation Identifier — EU/W3C, models
legislation and its relations, FRBR-based), **Akoma Ntoso** (rich XML for
document *structure/markup*, heavier than we need for a graph), bespoke `lex:`.

## Decision

Use **ELI** as the backbone vocabulary, with FRBR layering for versioning:
`eli:LegalResource` (the act as a stable work) realized by
`eli:LegalExpression` (a consolidated version at a date). Use ELI's relation
properties (`eli:amends`, `eli:repeals`, `eli:cites`, `eli:consolidates`) for
edges. Add a minimal `lex:` namespace only where ELI is silent — notably a
node for an individual **article** (`lex:Article`, `lex:text`, `lex:number`)
and source provenance (`lex:sourceURL`, `lex:retrievedAt`).

Full shape and invariants live in `docs/ontology.md`.

## Consequences

- Versioning has a natural home: each new consolidated text is a new Expression
  with its own `eli:version_date`; the Resource identity is stable.
- Interoperable with other ELI consumers; not EU-specific despite the name.
- Country act types must be mapped to ELI/`eli:type_document` slugs per country
  (documented in each country README).
- We must not over-extend `lex:` — prefer an ELI/Dublin Core/SKOS term first.
