# ADR 0019 — Finland: ingest via Finlex open data (opendata.finlex.fi)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Finland (`./fi`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

Finland is well-suited: the **Ministry of Justice** publishes **Finlex open
data**, a public REST API serving legislation as **Akoma Ntoso 3.0 XML** (OASIS
LegalDocML) with **ELI** aliases and **ECLI** identifiers — ELI is the backbone
of our ontology (ADR-0005), so the mapping is close. Data is produced by Edita
Publishing on behalf of the Ministry.

- Base: `https://opendata.finlex.fi/finlex/avoindata/v1`
- Documents are Akoma Ntoso with a `finlex:` proprietary extension namespace
  (`http://data.finlex.fi/schema/finlex`).
- No authentication or registration is required.

Verified live (2026-05-27):

| Endpoint | Purpose |
|----------|---------|
| `GET /akn/fi/act/statute-consolidated?limit=&offset=` | paged list of consolidated ("ajantasa") statutes; `<AknXmlList><Results>` envelope wrapping AKN documents |
| `GET /akn/fi/act/statute-consolidated/{year}/{number}/fin@` | full Finnish consolidated expression — metadata + body `<section>` (§) nodes |

Responses require `Accept: application/xml` (the API answers `406 Not
Acceptable` to `application/json` for these collections, advertising
`application/xml`). Each document's `<FRBRWork>`/`<FRBRExpression>` carry:

```xml
<FRBRalias name="eli" value="http://data.finlex.fi/eli/sd/2019/469/ajantasa"/>
<FRBRdate date="2019-03-29" name="dateIssued"/>     <!-- work -->
<FRBRdate date="2022-12-20" name="dateConsolidated"/> <!-- expression -->
<FRBRcountry value="fi"/> <FRBRnumber value="469"/>
```

and a `<proprietary>` block with `finlex:documentYear`, `finlex:typeStatute`
(`refersTo="#act"` / `#decree`, resolved via the document's `TLCConcept`
`showAs` label "Laki"/"Asetus"), `finlex:isInForce`,
`finlex:inForce/dateEntryIntoForce`, and relations `finlex:amendedBy`,
`finlex:repealedBy`, `finlex:issuedUnderActs` — each nesting
`<finlex:ref href="/akn/fi/act/statute/<year>/<number>">`.

### License

- Finnish **normative acts are not objects of copyright** — Tekijänoikeuslaki
  (404/1961) § 9 excludes laws and decrees from protection.
- The **Finlex open dataset is published under CC BY 4.0**; redistribution is
  permitted with attribution to **Finlex / Ministry of Justice (Edita
  Publishing)**.
- The API is a public, **no-auth** machine channel — no key, no registration.

> **Open point (verify before public redistribution):** confirm the exact
> published CC BY 4.0 terms and the required attribution wording on the Finlex
> open-data site before publishing a prebuilt FI dataset (ADR-0006). The acts
> themselves are non-copyrightable per § 9 regardless; we attribute the source.

Attribution: **Finlex / Ministry of Justice, Edita Publishing**.

## Decision

Finland ingestion pulls consolidated statutes from the **Finlex open-data API**
(Akoma Ntoso XML), not scraped pages. The FI importer:

1. pages `GET /akn/fi/act/statute-consolidated` to list consolidated statutes;
2. for each, fetches the full Finnish expression
   `.../{year}/{number}/fin@` (which carries the body sections);
3. maps each act into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: work URI uses `Country="fi"`, `Year` from
  `finlex:documentYear` (or the work FRBRuri path), `Number="<year>/<position>"`
  (e.g. `2019/469`). `eli:id_local` and `lex:sourceURL` are the act's ELI alias.
- **Type slug** (`eli:type_document`): from `finlex:typeStatute` resolved via
  `TLCConcept` — `act`→`laki`, `decree`→`asetus`, else an ASCII-folded slug of
  the label (fallback `saados`). See `fi/README.md`.
- **Status**: `finlex:isInForce` (`true`→in force, `false`→not in force); a
  present `finlex:noLongerInForce` forces repealed.
- **As-of date** (`eli:version_date`, MANDATORY): the expression's
  `dateConsolidated` — the point in time the consolidated text reflects —
  falling back to the expression's `dateIssued`, then the work's `dateIssued`.
  A record with no derivable version date is **dropped**, never guessed
  (ontology invariant 1). `finlex:dateEntryIntoForce` →
  `eli:first_date_entry_in_force`.
- **Relations**: `finlex:amendedBy`→`eli:amended_by`,
  `finlex:repealedBy`→`eli:repealed_by`,
  `finlex:issuedUnderActs`→`eli:cites`. Targets resolved to lex work URIs via
  each `finlex:ref` path's year/number. The target's precise type slug is
  unknown from the ref alone, so a neutral `statute` slug is used; the target
  gains its precise type when that act is itself ingested (a second-pass rewrite
  is left to a later phase).
- **Articles**: body `<section>` (§ "pykälä") nodes; the `<num>` numeral (e.g.
  "1" from "1 §") → `lex:number`, the `<heading>` + `<subsection>`/`<content>`
  text → `lex:text`. Titles/labels carry `@fi`; `eli:language` uses the FIN
  authority URI.

## Consequences

- Legally clean (acts non-copyrightable; dataset CC BY 4.0), API-sanctioned,
  no-auth, no HTML crawling.
- Native AKN + ELI means a clean country mapping.
- **Politeness**: the consolidated collection is large; the importer
  rate-limits (default 5 req/s), backs off on 429/5xx, and supports `-limit`
  for incremental runs.
- Swedish (`swe@`) expressions are out of scope for v1 (Finnish ingested as
  primary) — additive later, no schema change. Point-in-time revisions are out
  of scope for v1 (store current consolidated text). Sub-section granularity
  (momentti/kohta) stays inside the section's `lex:text` for v1.
- No network in tests — real list and consolidated-expression responses are
  captured once under `fi/scripts/akn/testdata/`.
