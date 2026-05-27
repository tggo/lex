# ADR 0010 — Badger as the triplestore backend; search index decoupled

- **Status**: Accepted
- **Date**: 2026-05-27
- **Supersedes**: the storage-backend choice in [ADR-0003](0003-rdf-triplestore-goRDFlib.md)
  and the same-file premise in [ADR-0004](0004-fts5-for-full-text-search.md)

## Context

ADR-0003 picked goRDFlib's **SQLite** backend, partly to co-locate an FTS5 index
in one file (ADR-0004). Reconsidering for scale: a country's full legislation is
large and write-heavy at ingest time. goRDFlib also ships a **Badger** backend
(pure-Go LSM KV, ACID, context-aware), which handles large/write-heavy loads
better than SQLite.

Trade-off:
- **SQLite**: single file (nice for Releases), FTS5 in the same file, tool-
  inspectable; slower bulk writes.
- **Badger**: faster writes, scales to large triple counts; stored as a
  **directory** (not one file) and is not SQL, so no in-engine FTS.

The maintainer chose Badger, accepting that search becomes a separate index and
that the distributable artifact is a directory (archived).

## Decision

1. The primary triplestore backend is **Badger** (`badgerstore`, via
   `store.Open(dir)`). Tests use the in-memory graph.
2. **Search is decoupled** from the triplestore: full-text (and later vector)
   search lives in its own sibling index, built from the triples — a separate
   SQLite **FTS5** file is the v1 plan, but the choice is open (a dedicated
   engine may replace it). It is explicitly *not* required to share storage with
   the graph.
3. The distributable dataset artifact is the Badger **directory**, archived
   (e.g. `.tar.zst`) for GitHub Releases — amends ADR-0006's "single file".

## Consequences

- Better ingest performance and headroom for large corpora.
- Release artifacts are archived directories, not a single `.db`; the importer
  and Release tooling must pack/unpack accordingly.
- The search index is rebuildable from the graph and versioned alongside it;
  losing/regenerating it never risks the authoritative triples.
- `internal/store` exposes `OpenMemory()` (tests) and `Open(dir)` (Badger);
  `DumpSorted` gives deterministic N-Triples for golden tests regardless of
  backend.
