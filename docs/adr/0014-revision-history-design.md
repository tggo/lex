# ADR 0014 — Revision history: design and phased rollout (P3)

- **Status**: Proposed
- **Date**: 2026-05-27

## Context

v1 stores one consolidated expression per act (ADR-0007). Phase P3 is true
revision history: an act's text *as it stood* at past points in time. The ELI
ontology already models this — one `eli:LegalResource` (the work) realized by
many `eli:LegalExpression`s (versions), each with its own `eli:version_date`.
So the *graph* is ready; the *Go schema, store, and ingestion* are not.

Two facts shape the rollout:

1. **It is a shared, cross-cutting change.** Today `schema.Act` holds a single
   `*Expression`. History means an act carries *many* expressions. That touches
   `schema`, `store` (write/read multiple expressions, pick "current" by latest
   `version_date`), the MCP views, and every country importer — and every schema
   field addition cascades into all country golden files. It must be coordinated,
   not landed unilaterally while country importers are in flight.
2. **Source coverage differs by country.**
   - **Japan (e-Gov API v2)** exposes each revision (`law_revision_id` +
     `amendment_enforcement_date`) with its full historical text — it can
     populate real historical expressions and should drive the first
     implementation.
   - **Ukraine (OGD `perv`)** ships only the *current* consolidated text. The
     card `history` field gives the redaction *timeline* (dates + event codes)
     but not the historical *text*. So UA can contribute version *dates*, not
     past text, until/unless a historical-text source is found.

## Decision (proposed)

1. Evolve the model so a resource has many expressions:
   `schema.Act` keeps a convenience `Current *Expression` plus
   `Versions []*Expression` (or the store indexes expressions by
   `version_date`). `GetAct` returns the latest; add `GetActVersion(uri, date)`
   and `ListVersions(uri)`. Done as one coordinated change across schema/store/
   mcp with all country goldens regenerated together.
2. JP ingestion populates historical expressions from e-Gov revisions.
3. UA records known redaction **dates** now via `ogd.ParseHistory` /
   `RevisionDates` (already implemented and tested), surfaced as version-date
   metadata; historical text remains out of scope until a source exists.

## Consequences

- `ogd.ParseHistory` lands ahead of the schema change as the UA building block;
  it is pure and tested, wired once the multi-expression model exists.
- Until then, v1's single-expression, current-text behaviour (ADR-0007) stands,
  and every answer still carries its as-of date.
- The coordinated schema change should be scheduled with the JP/PL importer work
  to avoid golden churn and merge conflicts.
