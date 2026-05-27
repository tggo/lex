# ADR 0001 — Record architecture decisions

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

We are starting `lex` from scratch and several structural choices (storage,
ontology, distribution) will be expensive to reverse. We want the *why* of each
choice captured at the time it was made, for contributors and future-us.

## Decision

We use Architecture Decision Records (Michael Nygard format). One file per
decision in `docs/adr/NNNN-title.md`, numbered sequentially, never deleted. A
superseded decision stays and is marked `Superseded by ADR-XXXX`.

Each ADR has: Status, Date, Context, Decision, Consequences.

## Consequences

- New significant decisions get an ADR before code lands.
- History is append-only; we read the chain, not a rewritten "current truth".
