# ADR 0008 — Apache-2.0 for code; texts keep source public status

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

`lex` is open-source and meant for broad reuse, including by legal-tech and
potentially commercial tools. We need a code license. Separately, the *data*
(legislative texts) is not the project's to license — in Ukraine normative acts
are not objects of copyright, and other jurisdictions have their own status.

## Decision

License the **code** under **Apache-2.0**. It is permissive like MIT but adds an
explicit patent grant and contributor terms, which suits a tool likely embedded
in other software.

**Legislative texts** are not relicensed by lex: they retain their
public-domain / official status from the source, and every record preserves
`lex:sourceURL` for attribution.

Considered and rejected: MIT (no patent grant), AGPL-3.0 (copyleft would deter
the commercial/embedded reuse we want to enable).

## Consequences

- `LICENSE` contains the canonical Apache-2.0 text.
- Contributions are under Apache-2.0 by default (inbound = outbound).
- Documentation should keep the data/code license distinction explicit so users
  don't assume lex grants rights over the legal texts.
