# ADR 0023 — Austria: ingest via the RIS OGD API (data.bka.gv.at/ris/api)

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

Austria (`./at`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data / API channel** over HTML
scraping, and must record source, endpoints, and license.

Austria's **Rechtsinformationssystem des Bundes (RIS)**, run by the
**Bundeskanzleramt (BKA)**, publishes a versioned **OGD REST API**. We ingest
the **consolidated federal-law** application — *Bundesnormen* / "Bundesrecht
konsolidiert" (`Applikation=BrKons`) — under the `Bundesrecht` route.

- Base: `https://data.bka.gv.at/ris/api/v2.6/Bundesrecht`
- API help: `https://data.bka.gv.at/ris/api/v2.6/Help`
- Human consolidated-law pages:
  `https://www.ris.bka.gv.at/GeltendeFassung.wxe?Abfrage=Bundesnormen&Gesetzesnummer=<gn>`

Verified live (2026-05-27): `GET /Bundesrecht?Applikation=BrKons&...` returns
`200` with JSON; `GET .../NOR<id>.xml` returns the document body XML. No
authentication is required.

### Data model — different from a native ELI API

Unlike Poland's Sejm ELI API (ADR-0012), RIS does **not** expose one document
per act. In the consolidated federal-law application, **each § (paragraph) of a
law is its own "Norm" document** with a `NOR…` id. All documents of one law
share a stable **`Gesetzesnummer`** — the law's work id. A special **`§ 0` head
document** carries the law's title (`Kurztitel`/`Titel`) and Stammnorm metadata
(`StammnormBgblnummer`, `Typ`, `Inkrafttretensdatum`, `Ausserkrafttretensdatum`).

So our unit of ingestion is **one `Gesetzesnummer` = one `schema.Act`**, and
each non-head § document with body text = one `schema.Article`.

Search response (JSON, RIS serializes its internal XML — note the `#text` /
`@attr` conventions, and that `OgdDocumentReference` is a bare object for a
single hit and an array otherwise):

```jsonc
{ "OgdSearchResult": { "OgdDocumentResults": {
  "Hits": { "#text": "3" },
  "OgdDocumentReference": [ { "Data": {
    "Metadaten": {
      "Technisch": { "ID": "NOR12076986" },
      "Allgemein": { "Geaendert": "...", "Veroeffentlicht": "..." },
      "Bundesrecht": { "Kurztitel": "...", "Titel": "...<br/>StF: ...",
        "BrKons": { "Typ": "V", "Paragraphnummer": "1",
          "ArtikelParagraphAnlage": "§ 1", "StammnormBgblnummer": "727/1990",
          "Inkrafttretensdatum": "1990-12-16",
          "Ausserkrafttretensdatum": "1994-09-30",
          "Gesetzesnummer": "10007061" } } },
    "Dokumentliste": { "ContentReference": { "Urls": { "ContentUrl": [
      { "DataType": "Xml", "Url": "https://www.ris.bka.gv.at/.../NOR12076986.xml" } ] } } }
  } } ] } } }
```

The content XML (`risdok/nutzdaten/abschnitt`) carries the substantive text in
the `<absatz>` runs following the `<ueberschrift typ="titel">Text</ueberschrift>`
marker.

### License

RIS open data is published on **data.gv.at** under **Creative Commons
Attribution 4.0 International (CC BY 4.0)** (RIS OGD F.A.Q.; dataset "RIS Daten
Version 2.6"). Commercial and non-commercial reuse are both permitted with
attribution; the API is the official sanctioned machine channel and requires no
authentication. Only the wording in the Bundesgesetzblatt is legally binding.

Attribution: **RIS / Bundeskanzleramt (data.bka.gv.at)**, preserved as
`lex:sourceURL` per act.

## Decision

Austria ingestion pulls from the **RIS OGD API** (`Applikation=BrKons`), not
scraped pages. The AT importer:

1. takes a list of `Gesetzesnummer` (law work ids);
2. pages `GET /Bundesrecht?Applikation=BrKons&Gesetzesnummer=<gn>` to collect
   all § documents of each law;
3. when article text is requested, fetches each non-head document's content XML
   (`…/NOR<id>.xml`) and extracts the body;
4. maps each law into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: work `Number` = `IDLocal` = the **`Gesetzesnummer`** (stable law
  id, e.g. `10007061`). `Year` is parsed from `StammnormBgblnummer`
  (`"727/1990"` → 1990). `SourceURL` = the `GeltendeFassung.wxe` consolidated-law
  page for the Gesetzesnummer.
- **Type slug** (`eli:type_document`): mapped from `BrKons.Typ` — `BG`→
  `bundesgesetz`, `BVG`→`bundesverfassungsgesetz`, `V`→`verordnung`, `K`→
  `kundmachung`, with a title override `Gesetzbuch`→`gesetzbuch` (a code,
  mirroring the `kodeks` override in PL/UA). Unknown types ASCII-fold (German
  umlaut transliteration) as a last resort.
- **Status**: an `Ausserkrafttretensdatum` (date no longer in force) on the head
  document means **repealed** (and is recorded as
  `eli:date_no_longer_in_force`); otherwise an `Inkrafttretensdatum` means
  **in force**.
- **As-of date** (`eli:version_date`, MANDATORY): RIS exposes no single
  "consolidated as of" field. We take the **latest `Geaendert` (last-changed)
  date across the law's documents**, falling back to the head's
  `Inkrafttretensdatum`. `Inkrafttretensdatum` →
  `eli:first_date_entry_in_force`.
- **Articles**: one per non-head § document; `lex:number` from
  `Paragraphnummer`, `skos:prefLabel` from `ArtikelParagraphAnlage` (e.g.
  `§ 1`), `lex:text` from the content XML body. Labels/text carry `@de`;
  `eli:language` uses the `DEU` authority URI.
- **Relations**: **deferred to a later phase.** RIS records changing acts only
  as free-text Bundesgesetzblatt references (`BrKons.Aenderung`,
  `NovellenBeziehung` e.g. "aufgehoben durch BGBl. Nr. 782/1994"). These carry
  no stable `Gesetzesnummer` for the target, so they cannot be resolved to lex
  work URIs (`schema.ResourceURI`) without a BGBl→Gesetzesnummer lookup. No
  amend/repeal/cite edges are emitted in v1.

## Consequences

- Legally clean (CC BY 4.0), API-sanctioned, no HTML crawling; attribution to
  RIS / Bundeskanzleramt is preserved per record.
- The per-§ document model means many small requests per law; the importer
  rate-limits (default 5 req/s) and backs off on 429/5xx. Laws are selected
  explicitly by `Gesetzesnummer`, so runs are naturally incremental.
- Relation edges (amends/repeals/cites) are out of scope for v1 pending a
  BGBl→Gesetzesnummer resolution step; this is additive, no schema change.
- Point-in-time revisions are out of scope for v1 (store current text).
- No network in tests — a real small law (Gesetzesnummer `10007061`, 3
  documents) plus its content XML are captured under
  `at/scripts/ris/testdata/`.
