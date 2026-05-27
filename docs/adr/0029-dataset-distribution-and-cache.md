# ADR 0029 — Dataset distribution via Releases, auto-pull, and a fetch cache

- **Status**: Accepted
- **Date**: 2026-05-27
- **Implements**: the distribution intent of ADR-0006 / ADR-0010.

## Context

A built dataset is large (UA with article text ≈ 879 MB on disk; ≈ 225 MB
gzipped). It must never be committed to git. But three audiences need it without
re-scraping the official sources:

- end users running `lex`;
- re-imports (the expensive part is fetching ~2941 act bodies);
- other machines / CI.

## Decision

1. **Distribute prebuilt datasets via a fixed GitHub Release.** Each dataset is
   `lex-<country>.tar.gz` (gzip tar of the dataset root: `graph/` + `index.fts`),
   attached to a stable release tag **`datasets`** (not `latest`, which would
   break when code releases are published). `internal/release.AssetURL` builds
   `…/releases/download/datasets/lex-<cc>.tar.gz`.

2. **`lex` auto-pulls on first run.** If `<root>/graph` is absent, `cmd/lex`
   downloads and extracts the country asset (country inferred from the data path
   or `-country`), guarded against zip-slip. `-no-pull` disables it;
   `-release-url` overrides the source. Users thus run a server over ready data
   without scraping.

3. **Importers cache fetched act bodies on disk.** `importer.Config.CacheDir`
   (default `ua/.cache`, gitignored, *outside* the dataset dir so it survives a
   rebuild) stores each body keyed by `file + version-date`. The version in the
   key means an amended act re-fetches; an unchanged one is served from cache. A
   re-import therefore skips the network for unchanged bodies.

## Consequences

- First-run UX is "download ~225 MB and serve", not "scrape for minutes".
- Publishing is a manual/CI step: build the dataset, `tar czf`, upload to the
  `datasets` release. CI automation is a TODO.
- The cache trades disk for bandwidth; correctness is preserved via the
  version-keyed cache. Tested: a re-import succeeds from cache even when the
  source returns 500 for bodies.
- Distribution stays out of git; only code + small fixtures are versioned.
