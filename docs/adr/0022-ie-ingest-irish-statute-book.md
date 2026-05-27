# ADR 0022 — Ireland: ingest via the electronic Irish Statute Book (native ELI)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Ireland (`./ie`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

Ireland is well-suited because the **electronic Irish Statute Book (eISB)**,
produced by the Office of the Attorney General, serves a **native ELI**: every
act page carries RDFa `<meta>` elements expressing the European Legislation
Identifier vocabulary — `eli:title`, `eli:date_document`, `eli:type_document`,
`eli:number`, `eli:language`, `eli:has_part`, `eli:changes`, … — which is the
very backbone of our ontology (ADR-0005). The acts are written in Akoma Ntoso
(sections are `<section>` elements, rendered in the print HTML as `<a
name="secN">` anchors).

Two no-auth channels are used:

- **Enumeration**: the **Houses of the Oireachtas open-data API**
  (`https://api.oireachtas.ie/v1/legislation`). It lists acts per year and
  carries each act's `statutebookURI` (its eISB ELI, e.g.
  `http://www.irishstatutebook.ie/eli/2015/act/60`).
- **Act detail + section text**: the eISB print page
  `http://www.irishstatutebook.ie/eli/{year}/act/{no}/enacted/en/print.html`,
  carrying the native-ELI RDFa metadata and the section bodies.

Verified live (2026-05-27):

| Endpoint | Purpose |
|----------|---------|
| `GET api.oireachtas.ie/v1/legislation?act_year={y}&lang=en&limit=&skip=` | act listing — `{head.counts.resultCount, results[].bill.act.{actYear,actNo,shortTitleEn,statutebookURI}}` |
| `GET irishstatutebook.ie/eli/{y}/act/{no}/enacted/en/print.html` | act page: RDFa `eli:*` meta + section bodies (`<a name="secN">`) |

The eISB `.xml`/`/print` direct extensions 404; the print HTML page is the
served representation and already carries full RDFa, so we parse it directly
(analogous to how the PL importer pulls JSON + `text.html`). `data.oireachtas.ie`
serves raw Akoma Ntoso XML but returns **403** without credentials, so it is not
the no-auth channel; we do not use it. No `robots.txt` is present (404), so no
crawl directives are violated; we still rate-limit.

Sample RDFa from `eli/2015/act/60`:

```html
<meta about="eisb:2015/act/60/enacted" property="eli:type_document"
      resource="http://www.irishstatutebook.ie/resources/resource-type#ACT" />
<meta about="eisb:2015/act/60/enacted" property="eli:date_document"
      CONTENT="2015-12-25" datatype="xsd:date" />
<meta about="eisb:2015/act/60/enacted/en" property="eli:title"
      CONTENT="Bankruptcy (Amendment) Act 2015" xml:lang="en" />
<meta about="eisb:2015/act/60/enacted" property="eli:changes"
      resource="http://www.irishstatutebook.ie/eli/1988/act/27/enacted" />
<meta about="eisb:2015/act/60/enacted" property="eli:has_part"
      resource="http://www.irishstatutebook.ie/eli/2015/act/60/section/1" />
```

### License

Re-use is governed by the **OIREACHTAS (OPEN DATA) PSI LICENCE** — a standard
licence for the purposes of Directive (EU) 2019/1024 on open data and the re-use
of public-sector information, transposed by the European Union (Open Data and
Re-use of Public Sector Information) Regulations 2021 (S.I. No. 376/2021). The
licence **incorporates the Creative Commons Attribution 4.0 International
License**. Each eISB act page carries `eli:licence` pointing at
`http://www.irishstatutebook.ie/eli/open-data.html`.

Re-use (including redistribution of a prebuilt dataset) is therefore permitted
**with attribution**. We attribute the **Irish Statute Book / Office of the
Attorney General** (and, for enumeration, the **Houses of the Oireachtas**) and
preserve `lex:sourceURL` per record.

## Decision

Ireland ingestion pulls from the **eISB native-ELI pages**, enumerated via the
**Oireachtas open-data API**, not scraped UI. The IE importer:

1. for each year in the requested range, pages
   `GET /v1/legislation?act_year={y}` to list acts (each carrying its eISB ELI);
2. for each act fetches the eISB English print page;
3. parses the RDFa `eli:*` metadata and the section bodies into ELI RDF per
   `docs/ontology.md`.

### Mapping decisions

- **Identity**: the eISB ELI path `2015/act/60` → `eli:id_local`. Work URI uses
  `Country="ie"`, `Year=2015`, `Number="60"`. `SourceURL` =
  `http://www.irishstatutebook.ie/eli/2015/act/60`.
- **Type slug** (`eli:type_document`): mapped from the `eli:type_document`
  resource (`…resource-type#ACT` → `act`, `#SI` → `si`), falling back to the ELI
  path's type token. See `ie/README.md`.
- **As-of date** (`eli:version_date`, MANDATORY): the eISB exposes
  `eli:date_document` — the act's signing/enactment date. v1 ingests the
  **enacted** expression (the as-enacted consolidated text the eISB serves at the
  `/enacted/en` path), so the document date is both the version date and
  `eli:first_date_entry_in_force`. (Point-in-time *revised* texts are produced
  separately by the Law Reform Commission; ingesting those is a later phase.)
- **Status**: the enacted text the eISB serves is treated as **in force**.
  Repeal/lapse status is not exposed on the enacted page; deriving it (e.g. from
  inbound `eli:changes`/LRC revised editions) is a next-phase edge.
- **Relations**: `eli:changes` → `eli:amends` (the acts this act amends), with
  targets resolved to lex work URIs via their recovered ELI path. Inbound
  amend/repeal/cite edges are a next-phase addition (no schema change needed).
- **Articles**: each `<a name="secN">` section anchor begins a `lex:Article`
  whose `lex:number` is N, label `"Section N"`, and `lex:text` is the collapsed
  text from that anchor up to the next section anchor. Sub-paragraph anchors
  (`s1._p0`) and schedule anchors (`sched1`) are not section delimiters.
  Titles/labels carry `@en`; `eli:language` uses the ENG authority URI. Acts are
  bilingual (English + Irish/Gaeilge); v1 ingests the **English** expression.

## Consequences

- Legally clean (PSI licence incorporating CC BY 4.0), no UI crawling — the
  RDFa-annotated print page is the official served representation.
- Native ELI means a clean country mapping (comparable to PL).
- **Politeness**: the importer rate-limits (default 5 req/s) and backs off on
  429/5xx. A year range (`-from`/`-to`) is required so runs are incremental.
- Sub-section granularity (subsection/paragraph) is kept inside the section's
  `lex:text` for v1; finer structure is additive later, no schema change.
- **Scoped out (next phase)**: LRC *revised* (point-in-time) editions; repeal
  status; Statutory Instruments (the `si` slug is mapped but the importer
  enumerates Acts only); the Irish-language (`ga`) expression; inbound
  amended_by/repealed_by/cites edges.
- **No network in tests** — a real eISB act page (RDFa head + 3 sections) and a
  real Oireachtas list response are captured under `ie/scripts/eisb/testdata/`
  and served via `httptest`.
