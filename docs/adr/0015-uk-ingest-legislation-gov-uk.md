# ADR 0015 — United Kingdom: ingest via legislation.gov.uk (CLML + Atom feeds)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

The United Kingdom (`./uk`) is the next country. Per project convention
(CLAUDE.md, ADR-0009) we prefer a country's official **open-data / API channel**
over HTML scraping, and must record source, endpoints, and license.

The UK is well-suited: **legislation.gov.uk**, run by **The National Archives**,
is the official publisher and exposes **native ELI identifiers** plus a
machine-readable channel — every item is available as **CLML** (Crown
Legislation Markup Language) XML by appending `/data.xml` to its URL, and item
lists are published as **Atom feeds** (`/data.feed`). No authentication is
required and there is no per-call key. The European Legislation Identifier is
the backbone of our ontology (ADR-0005), so the mapping is close to 1:1.

- Base: `https://www.legislation.gov.uk`
- Developer docs / API overview: `https://www.legislation.gov.uk/developer`
- Human, ELI-bearing pages: `https://www.legislation.gov.uk/{type}/{year}/{number}`

Verified live (2026-05-27):

| Endpoint | Purpose |
|----------|---------|
| `GET /{type}/{year}/data.feed?page=N` | paginated Atom list of acts of that type/year; `<leg:page>`/`<leg:morePages>` drive paging; each `<entry>` has an `<id>` `…/id/{type}/{year}/{number}` and `ukm:Year`/`ukm:Number`/`ukm:DocumentMainType` |
| `GET /{type}/{year}/{number}/data.xml` | full CLML document: `ukm:Metadata` (title, dates, type), `Primary/Body/P1group` sections, inline `<Citation>` references |
| `GET /{type}/{year}/{number}` | human ELI page — stored as `lex:sourceURL` |

Sample feed entry (`GET /ukpga/2023/data.feed`):

```xml
<entry>
  <id>http://www.legislation.gov.uk/id/ukpga/2023/57</id>
  <title>National Insurance Contributions (Reduction in Rates) Act 2023</title>
  <ukm:Year Value="2023"/><ukm:Number Value="57"/>
  <ukm:DocumentMainType Value="UnitedKingdomPublicGeneralAct"/>
</entry>
```

The CLML document root carries `RestrictStartDate` (the date from which the
served point-in-time consolidation is in force); `ukm:Metadata` carries
`dc:title`, `dct:valid`, and `ukm:PrimaryMetadata/EnactmentDate`. Sections are
`Primary/Body/P1group` nodes (the `P1` carries the `Pnumber`); inline
`<Citation URI="…/id/{type}/{year}/{number}" Class="…">` elements (scattered
through `Commentaries`) reference other legislation.

### License

legislation.gov.uk content is published under the **Open Government Licence
v3.0 (OGL v3.0)** by The National Archives. The OGL permits copying,
publishing, distributing, adapting and commercially exploiting the information,
provided the source is **attributed**. UK primary legislation is also Crown
copyright but explicitly licensed for reuse under the OGL.

> **Attribution (required by OGL v3.0):** "Contains public sector information
> licensed under the Open Government Licence v3.0." Source:
> **legislation.gov.uk / The National Archives**. We preserve `lex:sourceURL`
> per record. The OGL is a clear, machine-channel-sanctioned reuse licence — no
> open point, unlike PL (ADR-0012).

Attribution: **legislation.gov.uk / The National Archives (OGL v3.0)**.

## Decision

UK ingestion pulls from legislation.gov.uk's **CLML XML + Atom feed** channel,
not scraped HTML pages. The UK importer:

1. for each configured `type` (default `ukpga`) and year range, pages
   `GET /{type}/{year}/data.feed` to list acts;
2. for each listed act fetches `GET /{type}/{year}/{number}/data.xml`;
3. maps each CLML document into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: the act path `ukpga/2023/57` → `eli:id_local`. Work URI uses
  `Country="uk"`, `Year=2023`, `Number="57"`, `TypeSlug="ukpga"`.
  `SourceURL` = `https://www.legislation.gov.uk/{type}/{year}/{number}`.
- **Type slug** (`eli:type_document`): the legislation.gov.uk type code is
  already a short, stable ASCII slug, so the mapping is identity (`ukpga`,
  `uksi`, `asp`, `anaw`, `nia`, `ukla`, …). See `uk/README.md`.
- **As-of date** (`eli:version_date`, MANDATORY): the CLML document root's
  **`RestrictStartDate`** — the date from which the served point-in-time
  consolidation is in force — is the cleanest "as of" signal; we fall back to
  `dct:valid`, then `ukm:EnactmentDate`. `ukm:EnactmentDate` →
  `eli:first_date_entry_in_force`.
- **Status**: legislation.gov.uk's point-in-time service serves text that is in
  force as of the requested date, and the default `/data.xml` returns the latest
  in-force consolidation; CLML `DocumentStatus` ("revised"/"final") is editorial
  state, not in-force state. We therefore record served consolidations as
  **in force**. Repeal detection (point-in-time / `UnappliedEffect`) is a next
  phase (see Consequences).
- **Relations**: inline `<Citation>` elements → `eli:cites`, resolved to lex
  work URIs from each citation's `URI` (`…/id/{type}/{year}/{number}`), with a
  `Class`+`Year`+`Number` attribute fallback. Amends/repeals edges are exposed
  by legislation.gov.uk as point-in-time *effects* (`ukm:UnappliedEffect` /
  the changes feed), not as simple references; mapping those is a next phase.
- **Articles**: each `Primary/Body/P1group` is one `lex:Article`; `Number` is
  the `P1`'s `Pnumber`, `Label` is the group `Title`, and `Text` is the
  whitespace-flattened text of the whole section body (sub-paragraphs,
  amendments, citations included). Labels/text carry `@en`; `eli:language` uses
  the ENG authority URI.

## Consequences

- Legally clean (OGL v3.0, explicit reuse licence with attribution),
  channel-sanctioned, no HTML crawling.
- Native ELI + CLML means a clean country mapping, on par with PL.
- **Politeness**: the full corpus is large (tens of thousands of items across
  all types and years); the importer rate-limits (default 5 req/s) and backs off
  on 429/5xx. Type and year-range flags allow incremental runs, and a year range
  is required (defaults to the current year) so a bare run is bounded.
- Richer UK section granularity (subsections `P2`, paragraphs `P3`, schedules)
  is kept inside the section's `lex:text` for v1; finer structure is additive
  later, no schema change. Point-in-time revisions and amends/repeals *effects*
  are out of scope for v1 (store the current consolidated text + cites).
- No network in tests — a real CLML act document and a real Atom feed are
  captured once under `uk/scripts/clml/testdata/`.
