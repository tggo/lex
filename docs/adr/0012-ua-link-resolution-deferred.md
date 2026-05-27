# ADR 0012 — UA act relations: defer link resolution (needs global dokid map)

- **Status**: Superseded by [ADR-0013](0013-ua-relations-implemented.md) —
  both blockers were resolved (global `doc.txt` index + `vidnosh`/`typ` legends).
- **Date**: 2026-05-27

## Context

The plan was to turn the OGD card `links` field into `eli:amends` / `eli:cites`
graph edges. Investigation of the live `cards.json` (2941 primary acts,
2026-05-27) shows two blockers:

1. **Targets are mostly outside the primary set.** The field has the stable
   shape `<dokid>#<type:count|…>##`: one target document id plus relation-type
   codes. Resolving the target `dokid` against the primary-acts (`perv`) set
   succeeds for only **3 of 2941** acts — the other 2926 point at documents in
   other OGD datasets (amendments/redactions/etc.). A meaningful citation graph
   therefore needs a **global `dokid → nreg` map across all OGD datasets**, not
   just `perv`.
2. **Relation-type codes are undocumented.** The codes (most common: 17, 6, 2,
   1, 22, 26, 25…) are not explained by any reference we have. Guessing which
   code means "amends" vs "repeals" vs "cites" would risk asserting wrong legal
   relationships — unacceptable for a legal tool (cf. ADR-0007's honesty rule).

## Decision

Defer link/relation ingestion. v1 ships acts **without** relation edges rather
than a 0.1%-complete graph with unclassified edge types. Article structure
(ADR scope already done) and metadata stand on their own.

When pursued, the relation pass will:
1. ingest the broader OGD document registry to build a global `dokid → nreg`
   (and `dokid → resource URI`) map;
2. obtain/derive the relation-type code legend and map codes to ELI predicates
   (`eli:amends`, `eli:repeals`, `eli:cites`), defaulting unknown codes to the
   generic `eli:cites`;
3. emit edges only for resolvable targets, preserving the source code for audit.

## Consequences

- `schema.Expression` already has `Amends/Repeals/Cites/Consolidates`; the store
  writes whatever is present, so this is purely an ingestion gap — no schema or
  store change needed later.
- No misleading partial relation graph is shipped.
- Tracked as the next UA pass after the global registry is available.
