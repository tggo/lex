# ADR 0020 — United States (federal): ingest via USLM XML bulk (uscode.house.gov)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

The United States federal level (`./us`) is the next country. Per project
convention (CLAUDE.md, ADR-0009) we prefer a country's official **open-data /
bulk channel** over HTML scraping, and must record source, endpoints, and
license.

The authoritative codification of US federal statutes is the **United States
Code**, maintained by the **Office of the Law Revision Counsel (OLRC)** of the
U.S. House of Representatives. The OLRC publishes the entire Code as
**USLM (United States Legislative Markup) XML** — a stable, documented,
government-published schema — in per-title bulk files, with no authentication.

- Download portal: `https://uscode.house.gov/download/download.shtml`
- Release points (point-in-time snapshots, keyed by the most recent public law):
  `https://uscode.house.gov/download/releasepoints/us/pl/<congress>/<law>/xml_usc<TT>@<congress>-<law>.zip`
  (e.g. `.../us/pl/119/4/xml_usc01@119-4.zip`)
- Current release: `https://uscode.house.gov/download/currentrelease.shtml`
- USLM schema & user guide: `https://uscode.house.gov/download/resources/`

Each per-title zip contains a single USLM XML document (`usc<TT>.xml`). The
relevant structure:

| Element | Meaning |
|---------|---------|
| `<uslm>` | document root |
| `<meta>` | publication metadata: `docNumber`, `dc:title`, `dcterms:created`, `dcterms:modified` |
| `<main><title identifier="/us/usc/t1">` | the USC title (one per file) — modeled as one lex Act |
| `<chapter identifier="/us/usc/t1/ch1">` | chapter grouping (optional nesting level) |
| `<section identifier="/us/usc/t1/s1" status=… startPeriod=…>` | a USC section — modeled as a `lex:Article` |
| `<num value="1">` / `<heading>` / `<content>` | section number, heading, body |

Source verified (2026-05-27): `uscode.house.gov` is the official OLRC host
(resolves to the congressional `143.231.0.0/16` block); the USLM bulk channel
and release-point URL scheme are the long-standing documented distribution
method. (Direct download from the build sandbox was TCP-filtered; the host name
resolves and the channel is well documented, so the fixture is a small,
hand-trimmed USLM Title 1 subset rather than a captured multi-MB file.)

### License

US Government legal edicts are in the **public domain**:

- **17 U.S.C. § 105** — copyright protection is not available for any work of
  the United States Government.
- Under the **government edicts doctrine** (e.g. *Georgia v. Public.Resource.Org*,
  2020) the law itself — statutes and official codifications — is not subject to
  copyright.
- The OLRC download page states the USLM files are not subject to copyright and
  may be freely used.

No attribution is legally required, but we preserve `lex:sourceURL` per record
and credit **Office of the Law Revision Counsel, U.S. House of Representatives**.
This is a no-auth, sanctioned bulk channel; we do **not** crawl any UI.

## Decision

US ingestion pulls the **OLRC USLM XML bulk** release points, not scraped
pages. The US importer:

1. for each requested USC title number (default 1..54), fetches the
   per-title zip `xml_usc<TT>@<release>.zip` from the release-point directory;
2. extracts the single `.xml` entry (`archive/zip`, stdlib);
3. parses the USLM document and maps it into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Act = USC title.** Each title is one `eli:LegalResource`. Work URI uses
  `Country="us"`, `TypeSlug="usc-title"`, `Year` = the version-date year,
  `Number="title-<n>"`. `eli:id_local` = `usc/title-<n>`.
- **Type slug** (`eli:type_document`): a single slug `usc-title` — the US Code
  is one codification; finer act typing (public laws, CFR) is a later phase.
- **Article = USC section.** Each `<section>` becomes a `lex:Article`:
  `lex:number` from `<num value>` (fallback: the tail of `@identifier`),
  `skos:prefLabel` = `<num>` text + `<heading>`, `lex:text` = the flattened
  `<content>` (tags stripped, entities decoded, whitespace collapsed). Sections
  may sit under a `<chapter>` or directly under the `<title>`; both are walked.
- **Status**: per-section USLM `@status` (`repealed`/`omitted`/`transferred`/…
  → not in force; absent → operative) is collapsed to a work-level status — the
  title is in force if any section is operative, repealed only if all are gone.
- **As-of date** (`eli:version_date`, MANDATORY): USLM per-title files are
  point-in-time release points. We use `<meta><dcterms:modified>` (the release
  publication timestamp), falling back to `<dcterms:created>`, then to the first
  section's `@startPeriod`. If none is present the section/title yields a zero
  date and the store rejects it (ontology invariant) — we never guess.
- **Language**: `en` / authority `ENG`.
- **Relations** (`eli:amends`/`repeals`/`cites`): **scoped out for v1.** USLM
  carries amendment/source notes as free-text inside `<notes>`/`<sourceCredit>`
  (e.g. "Pub. L. 99-514 …") rather than as machine target identifiers, so there
  is no clean structured edge to a target USC provision. Extracting and
  resolving those citations to lex work URIs (via `schema.ResourceURI`) is a
  next-phase task; v1 stores the title text, identity, version date, and status.

## Consequences

- Legally clean (public-domain edicts), official no-auth bulk channel, no HTML
  crawling.
- **Politeness**: the full Code is 54 titles of multi-MB zips; the importer
  rate-limits (default 2 req/s) and backs off on 429/5xx. A `-titles` flag
  allows incremental runs.
- Sub-section USLM granularity (subsections, paragraphs) is kept inside the
  section's `lex:text` for v1; finer structure is additive later, no schema
  change.
- Amendment/citation edges and point-in-time multi-release history are out of
  scope for v1 (store the current release's text).
- No network in tests — a small real-shaped USLM Title 1 document is captured
  under `us/scripts/uslm/testdata/`; the importer test zips it in-memory and
  serves it via `httptest`.
