# ADR 0007 — v1 stores current consolidated text + metadata only

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Laws are amended continuously. Sources like `zakon.rada.gov.ua` keep every
historical redaction with effective-date ranges. Storing full history is the
most legally complete option but is a large scope and storage increase, and it
slows v1 substantially.

## Decision

v1 stores, per act, the **current consolidated expression** plus the metadata
needed to be honest about it: `eli:version_date` (as-of date, mandatory),
`eli:first_date_entry_in_force`, in-force/repealed status, and source URL +
retrieval timestamp. Full historical redactions are deferred (P3).

The FRBR Resource→Expression model (ADR-0005) already accommodates multiple
expressions, so adding history later is additive, not a rewrite.

## Consequences

- Every answer can state "as of <date>, in force / repealed" — non-negotiable
  for a legal tool.
- "What did this article say in 2018?" is out of scope for v1, by design.
- Scrapers that cannot determine a version date drop the record rather than
  guess (see ontology invariant 1).
