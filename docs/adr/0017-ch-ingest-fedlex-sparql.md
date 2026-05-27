# ADR 0017 — Switzerland: ingest via the Fedlex SPARQL endpoint (fedlex.data.admin.ch)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Switzerland (`./ch`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

Switzerland is well-suited because the **Swiss Federal Chancellery** publishes
**Fedlex** as native linked open data. The Classified Compilation
(*Systematische Rechtssammlung*, SR) and Official Compilation (*Amtliche
Sammlung*, AS) are exposed as RDF behind a public **SPARQL endpoint**. Fedlex's
URIs are themselves **ELI** URIs (`https://fedlex.data.admin.ch/eli/...`), and
the data is modelled with the **JoLux** ontology
(`http://data.legilux.public.lu/resource/ontology/jolux#`), shared with
Luxembourg. JoLux mirrors FRBR (work / expression / manifestation), so the
mapping to `docs/ontology.md` (ELI + FRBR) is close.

- SPARQL endpoint: `https://fedlex.data.admin.ch/sparqlendpoint`
- Linked-data / human entry: `https://fedlex.data.admin.ch`
- No authentication required (public, anonymous SPARQL over HTTP).

Verified live (2026-05-27): a `POST` with
`Accept: application/sparql-results+json` returns SPARQL 1.1 JSON. The endpoint
is Virtuoso-backed.

### The query

We select Classified Compilation works (`jolux:ConsolidationAbstract`)
classified by an SR notation (the stable Swiss identifier), realized by a
German expression carrying the title and short title, with the work's in-force
dates:

```sparql
PREFIX jolux: <http://data.legilux.public.lu/resource/ontology/jolux#>
PREFIX skos: <http://www.w3.org/2004/02/skos/core#>
SELECT ?cc ?srNotation (SAMPLE(?t) AS ?title) (SAMPLE(?ts) AS ?titleShort)
       (SAMPLE(?eif) AS ?dateEntryInForce) (SAMPLE(?nlf) AS ?dateNoLongerInForce) WHERE {
  ?cc a jolux:ConsolidationAbstract ;
      jolux:classifiedByTaxonomyEntry ?tax .
  ?tax skos:notation ?srNotation .
  FILTER(datatype(?srNotation) = <https://fedlex.data.admin.ch/vocabulary/notation-type/id-systematique>)
  FILTER NOT EXISTS { ?cc jolux:dateNoLongerInForce ?repealed }
  ?cc jolux:isRealizedBy ?expr .
  ?expr jolux:language <http://publications.europa.eu/resource/authority/language/DEU> ;
        jolux:title ?t .
  OPTIONAL { ?expr jolux:titleShort ?ts }
  OPTIONAL { ?cc jolux:dateEntryInForce ?eif }
  OPTIONAL { ?cc jolux:dateNoLongerInForce ?nlf }
} GROUP BY ?cc ?srNotation ORDER BY ?srNotation
```

A `GROUP BY ... SAMPLE()` collapses the multiple title/expression solutions
Fedlex returns per work into one row; the importer additionally deduplicates by
SR notation defensively.

Sample solution (German, `Accept: application/sparql-results+json`):

```jsonc
{ "cc":        { "type":"uri", "value":"https://fedlex.data.admin.ch/eli/cc/24/233_245_233" },
  "srNotation":{ "type":"typed-literal",
                 "datatype":"https://fedlex.data.admin.ch/vocabulary/notation-type/id-systematique",
                 "value":"210" },
  "title":     { "type":"literal", "value":"Schweizerisches Zivilgesetzbuch vom 10. Dezember 1907" },
  "titleShort":{ "type":"literal", "value":"ZGB" },
  "dateEntryInForce": { "type":"typed-literal",
                        "datatype":"http://www.w3.org/2001/XMLSchema#date", "value":"1912-01-01" } }
```

### License

Swiss **official federal legislation is not protected by copyright**:
art. 5 para. 1 lit. a–c of the Swiss Copyright Act (URG / SR 231.1) excludes
laws, ordinances, international treaties, and other official enactments from
protection. The texts are therefore freely reusable. Fedlex additionally
publishes its data as open linked data through an anonymous SPARQL endpoint
(no auth, no token). We attribute **Fedlex / the Swiss Confederation** and
preserve the Fedlex ELI work URI as `lex:sourceURL` per record.

> No machine-readable terms-of-use document blocking reuse was located; the
> non-copyrightability of official texts is the controlling fact. Re-check for
> an explicit ToS before publishing a prebuilt CH dataset (ADR-0006).

## Decision

Switzerland ingestion pulls from the **Fedlex SPARQL endpoint** (SPARQL 1.1
JSON), not scraped pages. The CH importer:

1. issues one grouped SPARQL `SELECT` (optionally filtered to specific SR
   notations and/or `LIMIT`-ed) over `jolux:ConsolidationAbstract` works;
2. parses the SPARQL JSON bindings;
3. maps each work into ELI RDF per `docs/ontology.md` and writes it to the
   Badger store.

### Mapping decisions

- **Identity**: the **SR notation** (e.g. `210`) is Switzerland's stable
  systematic identifier → `Number`. Work URI uses `Country="ch"`,
  `Year=<version-date year>`, `Number=<SR notation>`. `eli:id_local` = `"SR 210"`.
  `SourceURL` = the Fedlex ELI work URI (`https://fedlex.data.admin.ch/eli/cc/...`).
- **Type slug** (`eli:type_document`): Switzerland exposes no single machine
  "type" on the SR work, so it is inferred from the German title (same
  title-driven approach used for UA/PL codes): `gesetzbuch` (code, e.g.
  ZGB/StGB/OR), `bundesverfassung`, `bundesgesetz`, `verordnung`,
  `bundesbeschluss`, `reglement`, else `erlass`. See `ch/README.md`.
- **Status**: a work carrying `jolux:dateNoLongerInForce` is repealed
  (`eli:date_no_longer_in_force` set); absent → in force. The base query already
  filters to in-force works, but the parser handles both for unit-level robustness
  and SR-specific runs.
- **As-of date** (`eli:version_date`, MANDATORY): Fedlex's SR consolidation
  works do not expose a single clean "consolidated as of" field at this query
  level, so we use `jolux:dateNoLongerInForce` when repealed (text frozen at
  repeal) and otherwise `jolux:dateEntryInForce` (when the consolidated text took
  effect). A record with neither has no usable version date and is **dropped**
  (the ontology forbids guessing).
- **Language**: Swiss federal law is equally authentic in **German, French, and
  Italian**. v1 stores the **German** expression (`LangTag "de"`,
  `LangAlpha3 "DEU"`, `eli:language` = DEU authority URI). French (`fr`/`FRA`)
  and Italian (`it`/`ITA`) expressions are a next phase — additive, no schema
  change.

## Consequences

- Legally clean (official texts non-copyrightable), endpoint-sanctioned, no HTML
  crawling, no authentication.
- Native ELI URIs and a FRBR-shaped ontology (JoLux) make the mapping clean.
- **Scoped out for v1 (next phases, all additive — no schema change):**
  - **Article full text** (`lex:Article` / `lex:text`): Fedlex serves article
    bodies via separate XML/HTML manifestations, not the metadata SPARQL above.
    v1 delivers a metadata + identity + status pass; article text is deferred.
  - **Relations** (`eli:amends`/`eli:repeals`/`eli:cites`): JoLux models
    citations (`jolux:citationFrom/ToLegalResource`) but they were not cleanly
    reachable for the SR consolidation works in a simple grouped query; resolving
    them to lex work URIs is a next phase.
  - **French/Italian expressions** and **point-in-time `jolux:dateApplicability`
    revisions**.
- **Politeness**: the full SR is thousands of works; the importer rate-limits
  (default 2 req/s) and backs off on 429/5xx. `-sr` and `-limit` flags allow
  small, targeted runs.
- No network in tests — a real grouped SPARQL JSON result (SR 210/220/311.0) is
  captured once under `ch/scripts/fedlex/testdata/` and served via `httptest`.
