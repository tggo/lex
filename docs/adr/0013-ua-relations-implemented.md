# ADR 0013 — UA relations implemented via the global doc index + code legends

- **Status**: Accepted
- **Date**: 2026-05-27
- **Supersedes**: [ADR-0012](0012-ua-link-resolution-deferred.md) (deferral lifted)

## Context

ADR-0012 deferred relation ingestion for two reasons: (1) `links` targets mostly
fell outside the primary-acts set, and (2) the relation-type codes were
undocumented. Both blockers are now resolved by additional OGD datasets:

1. **Global document index** — `…/laws/data/csv/doc.txt` (CP1251, ~48 MB,
   tab-separated) maps **every** document: `dokid, nreg, title, status,
   types(|-list), organs("orgid:date:num")`. This resolves link targets beyond
   the primary set and yields each target's type code and adoption year — enough
   to mint its lex resource URI.
2. **Code legends** (`…/laws/data/csv/`):
   - `vidnosh.txt` — relation-type codes. Mapped to ELI: `1 Змінює`→`amends`;
     `4 Скасовує`/`22 Визнає нечинним`/`25 Припиняє дію`/`29 …крім окремих`
     →`repeals`; everything else →`cites` (safe generic reference).
   - `typ.txt` — document-type codes → ELI type slugs (`1`→zakon, `21/124`→
     kodeks, `100/216`→konstytutsiya, `2`→postanova, `3`→ukaz, `9`→nakaz, …;
     unknown→`akt`).

## Decision

The importer's `-relations` flag fetches `doc.txt` once (CP1251-decoded), builds
a `dokid → {nreg, type, year}` index, and resolves each act's `links` field into
`eli:amends` / `eli:repeals` / `eli:cites` edges pointing at minted target
resource URIs. Targets absent from the index or lacking a year are skipped — we
never mint a guessed URI. Parsing and mapping live in `ua/scripts/ogd`
(`ParseLinks`, `ParseDocIndex`, `ResolveRelations`), golden/unit-tested offline.

## Consequences

- Verified on live data: of 2941 primary acts, 399 gained `cites`, 122 `amends`,
  68 `repeals` edges (vs. 3 resolvable before).
- Relation-type classification is conservative: only clear amend/repeal codes are
  promoted; ambiguous codes stay `cites`. This keeps a legal tool honest.
- `-relations` adds a ~48 MB download, so it is opt-in (default off) and run when
  building the full distributable dataset.
- Target URIs use the same scheme as ingested acts, so edges line up once the
  referenced acts are also imported (cross-dataset coverage grows over time).
