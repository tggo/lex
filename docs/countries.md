# Countries with open legislation — source survey

A scouting list of jurisdictions that publish their legislation through an
**official, machine-readable channel** (API, bulk download, or SPARQL/RDF), the
raw material a `lex` country scraper needs. Per project convention (CLAUDE.md,
ADR-0009) we only build on official open-data/API channels with a clear reuse
license — never HTML scraping of a site that disallows it.

This file is a research index, not a contract. The contract is
[`ontology.md`](./ontology.md); each adopted country still gets its own ADR
recording the exact source, endpoints, and license.

## Selection criteria (what makes a good `lex` source)

1. **Official** — published by the government / official journal, authoritative.
2. **Machine-readable** — REST/JSON, XML bulk, or SPARQL/RDF; not just HTML.
3. **Reusable license** — public-domain edicts, CC BY, or explicit reuse terms.
4. **ELI-native (bonus)** — emits European Legislation Identifier metadata, so
   the mapping to our ELI/RDF ontology is close to 1:1.

Legend: ⭐ = native ELI · 🟢 strong fit · 🟡 usable with work · 🔴 caution
(restrictive license or no real machine channel).

## Already in `lex`

| Country | Source | Access | License | ADR |
|---|---|---|---|---|
| 🇺🇦 Ukraine | `data.rada.gov.ua` (OGD) | Bulk CSV/JSON/XML + HTML bodies | CC BY 4.0 | [0009](./adr/0009-ua-ingest-official-open-data.md) |
| 🇯🇵 Japan | e-Gov 法令API v2 | REST JSON, revision-aware | Gov Standard Terms v2.0 (CC BY-compatible) | [0011](./adr/0011-jp-ingest-egov-law-api.md) |
| 🇵🇱 Poland ⭐ | Sejm ELI API (`api.sejm.gov.pl/eli`) | REST JSON + HTML, native ELI | acts non-copyrightable (art. 4) | [0012](./adr/0012-pl-ingest-sejm-eli-api.md) |
| 🇬🇧 United Kingdom ⭐ | legislation.gov.uk (CLML + Atom) | REST XML + feeds | OGL v3.0 | [0015](./adr/0015-uk-ingest-legislation-gov-uk.md) |
| 🇫🇷 France | DILA LEGI open data (XML bulk) | No-auth bulk XML | Licence Ouverte (Etalab) | [0016](./adr/0016-fr-ingest-dila-legi-opendata.md) |
| 🇨🇭 Switzerland ⭐ | Fedlex SPARQL | SPARQL/RDF (JoLux) | official texts not copyrightable | [0017](./adr/0017-ch-ingest-fedlex-sparql.md) |
| 🇱🇺 Luxembourg ⭐ | Legilux SPARQL | SPARQL/RDF (JOLux+ELI) | CC BY | [0018](./adr/0018-lu-ingest-legilux-sparql.md) |
| 🇫🇮 Finland ⭐ | Finlex open data (Akoma Ntoso) | REST XML, ELI+ECLI | CC BY 4.0 | [0019](./adr/0019-fi-ingest-finlex-open-data.md) |
| 🇺🇸 USA (federal) | US Code USLM (OLRC) | Bulk XML (zip) | public domain | [0020](./adr/0020-us-ingest-uslm-bulk.md) |
| 🇪🇸 Spain | BOE Datos Abiertos | REST JSON + XML | reuse permitted (BOE) | [0021](./adr/0021-es-ingest-boe-open-data.md) |
| 🇮🇪 Ireland ⭐ | Irish Statute Book (eISB RDFa) | RDFa + Oireachtas API | PSI / CC BY 4.0 | [0022](./adr/0022-ie-ingest-irish-statute-book.md) |
| 🇦🇹 Austria | RIS OGD API (Bundesrecht) | REST JSON + content XML | CC BY 4.0 | [0023](./adr/0023-at-ingest-ris-ogd-api.md) |
| 🇦🇺 Australia | Federal Register of Legislation (OData) | REST JSON | CC BY 4.0 | [0024](./adr/0024-au-ingest-frl-odata-api.md) |
| 🇳🇿 New Zealand | legislation.govt.nz (PCO LENZ XML) | Per-act XML | CC BY 4.0 (NZGOAL) | [0025](./adr/0025-nz-ingest-legislation-govt-nz.md) |

> The 11 rows below Poland were implemented on 2026-05-27, each mirroring the
> Poland scraper (pure offline parser + rate-limited importer + thin CLI, TDD,
> >80% coverage, offline fixtures). Several deferred relation edges or
> article-text passes to a next phase — see each ADR's "Consequences" and the
> per-country README "Status".

## Europe

| Country | Source | Access | ELI | License | Fit |
|---|---|---|---|---|---|
| 🇬🇧 United Kingdom | [legislation.gov.uk](https://www.legislation.gov.uk/developer) | REST API + bulk XML (CLML/Akoma Ntoso) | ⭐ | Open Government Licence v3.0 | 🟢 |
| 🇨🇭 Switzerland | [Fedlex](https://fedlex.data.admin.ch/) | SPARQL endpoint, RDF (Casemates) | ⭐ | Free reuse (federal law) | 🟢 |
| 🇱🇺 Luxembourg | [data.legilux.public.lu](http://data.legilux.public.lu/sparql) | SPARQL, RDF (JOLux + ELI) | ⭐ | CC BY / open | 🟢 |
| 🇫🇮 Finland | [Finlex open data](https://opendata.finlex.fi/finlex/avoindata/v1) | REST API, no auth; ELI + ECLI | ⭐ | CC BY 4.0 | 🟢 |
| 🇫🇷 France | [Légifrance API (PISTE)](https://www.legifrance.gouv.fr/contenu/pied-de-page/open-data-et-api) | REST API + DILA bulk dumps | ⭐ | Licence Ouverte (Etalab) | 🟢 |
| 🇪🇸 Spain | [BOE Datos Abiertos](https://www.boe.es/datosabiertos/api/api.php) | REST API (consolidated legislation) + RDF | ⭐ | Reuse permitted (BOE terms) | 🟢 |
| 🇩🇪 Germany | [Gesetze im Internet](https://www.gesetze-im-internet.de/) → new [Rechtsinformationsportal](https://digitalservice.bund.de/en/blog/new-project-picks-up-pace) | Per-act XML + bulk TOC XML; new portal adds metadata API | partial | No copyright (§5 UrhG) | 🟡→🟢 |
| 🇳🇱 Netherlands | [BWB / wetten.overheid.nl](https://data.overheid.nl/dataset/basis-wetten-bestand) | SRU search + XML repository (~45k regs) | partial | Public, free reuse | 🟢 |
| 🇮🇹 Italy | [Normattiva](https://www.normattiva.it/) | Akoma Ntoso XML; no clean public API | ⭐(IDs) | Reuse permitted | 🟡 |
| 🇮🇪 Ireland | [Irish Statute Book](https://www.irishstatutebook.ie/) | Akoma Ntoso XML bulk | ⭐ | PSI / reuse permitted | 🟢 |
| 🇦🇹 Austria | [RIS](https://www.ris.bka.gv.at/) | REST/OGD API, XML | partial | CC BY 4.0 (RIS OGD) | 🟢 |
| 🇳🇴 Norway | [Lovdata](https://lovdata.no/) | Limited; ELI IDs exist | ⭐ | 🔴 Lovdata reuse is restricted | 🔴 |
| 🇪🇺 EU | [EUR-Lex](https://eur-lex.europa.eu/) / Cellar SPARQL | Bulk + Webservice + SPARQL | ⭐ | Free reuse (notice) | 🟢 |

> EU/EEA context: the authoritative roster of ELI adopters is the EUR-Lex
> [ELI register — implementing countries](https://eur-lex.europa.eu/eli-register/implementing_countries.html).
> The ELI Task Force also includes Albania, Belgium, Croatia, Hungary, Malta,
> Portugal, Serbia, Slovenia — most at metadata/URI level, varying machine access.

## Americas

| Country | Source | Access | License | Fit |
|---|---|---|---|---|
| 🇺🇸 USA (federal) | [govinfo API](https://www.govinfo.gov/features/api), [U.S. Code XML](https://uscode.house.gov/download/download.shtml), [eCFR API](https://www.ecfr.gov/), [Federal Register API](https://www.federalregister.gov/) | REST JSON + bulk XML (USLM) | Public domain (federal edicts) | 🟢 |
| 🇨🇦 Canada | [Justice Laws](https://laws-lois.justice.gc.ca/) | Bulk XML downloads | Reproduction of Federal Law Order (free, no permission) | 🟢 |
| 🇧🇷 Brazil | [LexML Brasil](https://www.lexml.gov.br/), DOU (`in.gov.br`) | Federated search, Akoma Ntoso / URN-LEX | Public | 🟡 |
| 🇲🇽 Mexico | [DOF](https://www.dof.gob.mx/) | Searchable, some data services | Public | 🟡 |

## Asia & Oceania

| Country | Source | Access | License | Fit |
|---|---|---|---|---|
| 🇯🇵 Japan | e-Gov 法令API (in `lex`) | REST JSON | Gov Standard Terms (CC BY-compat) | 🟢 |
| 🇮🇳 India | [India Code](https://www.indiacode.nic.in/) | Bulk download, metadata | Government open data | 🟡 |
| 🇨🇳 China | [National Laws & Regs DB](https://flk.npc.gov.cn/) | JSON endpoints (undocumented) | 🔴 no explicit reuse license | 🔴 |
| 🇦🇺 Australia | [Federal Register of Legislation](https://www.legislation.gov.au/) | Bulk + emerging API | CC BY 4.0 | 🟢 |
| 🇳🇿 New Zealand | [legislation.govt.nz](https://www.legislation.govt.nz/) | Bulk XML | CC BY (PCO) | 🟢 |

## Recommended next candidates for `lex`

Ranked by ease (native ELI / clean machine channel / permissive license):

1. **🇬🇧 United Kingdom** — mature REST API + bulk XML, native ELI, OGL v3.0.
   The reference-quality open-legislation source.
2. **🇫🇷 France (Légifrance)** — full API, ELI, Licence Ouverte; large corpus.
3. **🇨🇭 Switzerland / 🇱🇺 Luxembourg / 🇫🇮 Finland** — SPARQL/RDF already in ELI
   ontology; smallest mapping distance to our store.
4. **🇺🇸 USA federal** — public domain, USLM XML + several REST APIs (Code, eCFR,
   Federal Register, govinfo).
5. **🇪🇸 Spain (BOE)**, **🇮🇪 Ireland**, **🇦🇹 Austria**, **🇦🇺 Australia**, **🇳🇿 NZ** —
   all official, machine-readable, permissively licensed.

Avoid for now: 🔴 **Norway (Lovdata)** and 🔴 **China** — no clean, openly
licensed machine channel.

## Sources

- EUR-Lex — [What is ELI](https://eur-lex.europa.eu/eli-register/what_is_eli.html),
  [implementing countries](https://eur-lex.europa.eu/eli-register/implementing_countries.html)
- [openlegaldata/awesome-legal-data](https://github.com/openlegaldata/awesome-legal-data)
- National portals linked inline in the tables above.

_Last surveyed: 2026-05-27. Licenses and endpoints change — re-verify in each
country's ADR before ingesting._
