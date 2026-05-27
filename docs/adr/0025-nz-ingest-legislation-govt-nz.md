# ADR 0025 — New Zealand: ingest via legislation.govt.nz PCO XML

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

New Zealand (`./nz`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / machine channel** over
HTML scraping, and must record source, endpoints, and license.

New Zealand legislation is published by the **Parliamentary Counsel Office
(PCO)** on the New Zealand Legislation website. Every act, bill and piece of
secondary legislation is available **as XML** (with the associated DTDs) — the
authoritative machine form. The XML is reachable two ways:

- **Per-act, format-suffixed URLs** on the website: appending a format
  extension to the act's "latest" page yields the document in that format, e.g.
  `https://www.legislation.govt.nz/act/public/1990/0109/latest/whole.xml`
  (the human page is the same path with `whole.html`).
- A **bulk XML directory** mirrored on `data.govt.nz` (all versions of all
  Acts/Bills/secondary legislation plus DTDs), maintained by the PCO.

Verified live (2026-05-27): the per-act `whole.xml` URL pattern and the bulk
data.govt.nz directory are the documented public download channels. The site
sits behind a bot-protection layer that answers automated clients lacking a
browser session with an HTTP 202 interstitial — this is rate-limiting / bot
mitigation, **not** authentication: no credentials or key are required for the
public XML, and the data itself is public-domain.

There is **also** a separate Developer API (`api.legislation.govt.nz`) that
requires an API key requested by email. That is an **authenticated** channel
and is therefore **out of scope** for this ingester (CLAUDE.md: no-auth only).
We use the public, no-auth XML export instead.

| Channel | Auth | Use |
|---------|------|-----|
| `GET /act/{category}/{year}/{number}/latest/whole.xml` | none | per-act consolidated XML (used) |
| `data.govt.nz` NZ Legislation directory (XML + DTDs) | none | bulk mirror (alternative source) |
| `api.legislation.govt.nz` Developer API | API key (email) | **not used** — authenticated |

### License

- **New Zealand Acts, Bills, Legislative Instruments and Supplementary Order
  Papers are not subject to Crown copyright** (Copyright Act 1994, s 27) — they
  are free to reproduce.
- The PCO publishes the XML data explicitly so it "can be used and repurposed by
  business, researchers and citizens", under the New Zealand Government Open
  Access and Licensing framework (**NZGOAL**), i.e. **Creative Commons
  Attribution 4.0 (CC BY 4.0)** for the dataset, attributing the **New Zealand
  Parliamentary Counsel Office**.

Attribution: **New Zealand Parliamentary Counsel Office / legislation.govt.nz**.

## Decision

New Zealand ingestion pulls the **PCO XML export** (per-act `whole.xml`), not
scraped HTML and not the authenticated Developer API. The NZ importer:

1. reads a legislation index (XML listing) to enumerate acts (category, year,
   number, title);
2. for each act fetches `…/{category}/{year}/{number}/latest/whole.xml`;
3. parses the `<cover>` metadata and `<body>` provision tree;
4. maps each act into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: the NZ canonical id `act/<category>/<year>/<number>` →
  `eli:id_local`. Work URI uses `Country="nz"`, `Year=<year>`,
  `Number="<number>"` (the PCO zero-padded 4-digit form, e.g. `0038`, kept
  verbatim so the URI reconstructs the source `whole.xml` URL). `SourceURL` is
  the human `whole.html` page.
- **Type slug** (`eli:type_document`): mapped from the listing category —
  `public`→`public-act`, `local`→`local-act`, `private`→`private-act`,
  `imperial`→`imperial-act`, `provincial`→`provincial-act` — with a title
  override (`code` when the title contains "Code"), mirroring PL/UA. See
  `nz/README.md`.
- **As-of date** (`eli:version_date`, MANDATORY): PCO publishes each
  consolidated reprint with a `<version-date>` (the "as at" date of the
  reprint) — we use that. When absent we fall back to `<commencement>`, then
  `<assent-date>`. An act with no resolvable version date is dropped (never
  guessed). `<commencement>` (else `<assent-date>`) →
  `eli:first_date_entry_in_force`.
- **Status**: in force by default; a `<repeal-date>` or a `repealed` flag on the
  cover → not in force, with the repeal date as `eli:date_no_longer_in_force`.
- **Articles**: sections are `<prov>` provisions carrying a `<label>`; the tree
  is flattened depth-first through `<part>`/`<subpart>` containers. Each
  section's heading + body text → `lex:text`; label `Section <n>`,
  `@en` / `eli:language` ENG.
- **Relations**: the plain `whole.xml` export does not expose a clean,
  machine-resolvable amends/repeals/cites edge set with target identifiers, so
  relation edges are **deferred to a next phase** (when the PCO point-in-time /
  cross-reference data is wired in). `schema.ResourceURI` is ready to mint edge
  targets once a source for them is parsed.

## Consequences

- Legally clean (NZ acts non-copyrightable; CC BY 4.0 dataset), no
  authenticated API, no HTML crawling.
- **Politeness**: the importer rate-limits (default 2 req/s) and backs off on
  429/5xx. Year-range flags allow incremental runs. The site's 202 bot wall may
  require running from an allowed network / honoring its guidance; this ADR
  does not bypass it.
- Sub-section granularity (subsections, paragraphs) is kept inside the section's
  `lex:text` for v1; finer structure is additive later, no schema change.
  Point-in-time revisions and cross-reference relation edges are out of scope
  for v1 (store current consolidated text).
- No network in tests — a real `whole.xml` (cover + sections, incl. a nested
  `<part>`) and a listing fixture are captured under `nz/scripts/lenz/testdata/`
  and served via `httptest`.

> **Open point (verify before public redistribution):** confirm the exact
> NZGOAL/CC BY attribution string the PCO requires and re-check the bot-wall /
> acceptable-use guidance before publishing a prebuilt NZ dataset (ADR-0006).
