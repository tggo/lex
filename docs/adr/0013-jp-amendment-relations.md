# ADR 0013 — Japan: ingest amendment & repeal relations (eli:amended_by / eli:repealed_by)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

ADR-0012 deferred UA relation ingestion for two reasons: (1) the OGD `links`
targets mostly point outside the primary-acts set, needing a global registry,
and (2) the relation-type codes are undocumented, so classifying an edge as
amends/repeals/cites would be guesswork — unacceptable for a legal tool.

Japan's e-Gov API does not have either blocker:

1. **Resolvable targets, same id space.** Each law's `revision_info` carries
   `amendment_law_id` — the `law_id` of the law that produced the current
   revision (e.g. the Civil Code's current revision was produced by
   `506AC0000000033`, 民法等の一部を改正する法律). It is a `law_id` in the *same*
   space as the acts we ingest, so it resolves directly against the law list we
   already page through — no separate registry needed.
2. **Documented meaning.** The field's semantics are fixed by the API: it is the
   amending law of this revision. We do not need an undocumented code legend to
   know the relationship is "amended by".

## Decision

Ingest both relations from the same `amendment_law_id` field, switching on
`repeal_status`:

- `amendment_law_id` is the law that produced the current revision. When that
  revision is a repeal (`repeal_status` = "Repeal", in which case `repeal_date`
  equals `amendment_enforcement_date`), the producing law *is* the repealing law
  — so it becomes an **`eli:repealed_by`** edge. Otherwise it is an amending
  law — an **`eli:amended_by`** edge.
- `egov.BuildRecords` carries the producing law on each `Record` as either
  `AmendedByLawID` or `RepealedByLawID` (dropping a self-reference, i.e. an
  initial enactment where the field equals the act's own `law_id`).
- After listing all laws, `importer.resolveAmendments` builds a
  `law_id → resource URI` map over the listed set and adds the edge **only when
  the target is in the set**. Unresolvable targets (a law not in the current
  listing) are dropped, never asserted — same honesty rule as ADR-0012.

This required two additive ontology changes: new `AmendedBy []string` and
`RepealedBy []string` fields on `schema.Expression`, written/read by the store
as the already-defined `eli:amended_by` / `eli:repealed_by` predicates. The
change is country-agnostic.

## Consequences

- `list_amendments(act_id)` can follow `eli:amended_by` for Japanese acts.
- Only the *current* revision's amending law is captured in v1 (one edge per
  act); the full amendment chain across historical revisions is a later phase,
  alongside historical expressions (ADR-0011).
- We model only the `*_by` direction, not the inverse `eli:amends` / `eli:repeals`,
  to avoid fabricating an edge direction we did not observe; the server can query
  the inverse via SPARQL.
- A repealed act still carries its repealed-by edge (and no amended-by), so
  `list_amendments` can show both why text changed and what ended it.
- The schema fields are additive (ADR-0005 / ontology "additive optional triples
  are fine"), so existing UA data and the store are unaffected.
