# ADR 0014 — Japan: historical revision timeline (metadata-only expressions)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

ADR-0011 stores one expression per act — the current enforced consolidated
version. The e-Gov API also exposes the full revision history of each law via
`GET /api/2/law_revisions/{law_id}`, which returns every revision
(`PreviousEnforced` / `CurrentEnforced` / `UnEnforced` / repeal) with its
enforcement date and the `amendment_law_id` that produced it (33 for the Civil
Code). Surfacing that timeline lets the server answer "what versions existed and
when, and what changed each time" — the historical-awareness the versioning
policy defers to "a later phase".

The constraint: `schema.Act` holds a single `Expression`, and the store's
`GetAct` selects the act's expression with a title-bearing SPARQL pattern
(`?expr dct:title ?title ; eli:version_date ?vdate`). A naive multi-expression
model would make `GetAct` ambiguous and ripple through every country.

## Decision

Add the revision timeline **additively, as metadata only**:

- `schema.Act` gains `Revisions []Revision`. A `Revision` carries
  `VersionDate`, `Status`, and resolved `AmendedBy` / `RepealedBy` edges — no
  title, no article text.
- The store writes each revision as a real `eli:LegalExpression` node realizing
  the same resource, with `eli:version_date`, `eli:in_force`, and the
  amend/repeal edges — but deliberately **without `dct:title`**. Because
  `GetAct` filters on `dct:title`, it keeps returning exactly the current
  expression; revision nodes are read back separately (those realizing the
  resource that have a version date but *no* title), sorted by date.
- For Japan, `egov.ParseRevisions` maps the `law_revisions` response; the
  importer (behind `-revisions`) fetches the timeline per act, skips the current
  enforced revision (it is already the `Expression`), and resolves each
  revision's producing law against the in-set `law_id` map — `eli:repealed_by`
  when the revision is a repeal, else `eli:amended_by` — dropping unresolvable
  targets (same honesty rule as ADR-0012/0013).

## Consequences

- Backward-compatible: UA/PL leave `Revisions` nil, emit no revision nodes, and
  `GetAct` behaves exactly as before. The change is additive (ADR-0005).
- Data stays small: one lightweight node per revision, no duplicated text. Full
  historical *text* per revision remains a later phase (it would be ~33× the
  text and fetches per act).
- The timeline is gated behind `-revisions` (one `law_revisions` call per act),
  like `-articles`, so the cheap metadata import stays cheap.
- A revision's amend/repeal edge resolves only if the producing law is in the
  ingested set, so partial imports show partial edges (the dates/status are
  always complete); a full import resolves the most.
- Convention recorded in `docs/ontology.md`: a metadata-only expression is one
  realizing a resource with `eli:version_date` but no `dct:title`.
