# ADR 0016 — France: ingest via the DILA LEGI open-data bulk dataset

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

France (`./fr`) is the next country. Per project convention (CLAUDE.md,
ADR-0009) we prefer a country's official **open-data** channel over HTML
scraping, and must record source, endpoints, and license.

France's authoritative legislation portal is **Légifrance**, operated by the
**DILA** (Direction de l'information légale et administrative). Légifrance
exposes a modern query API — the **PISTE API** — but it **requires OAuth
client credentials** (a registered application + token exchange). That is an
authenticated channel, which our importers must not depend on.

Fortunately DILA also publishes the underlying consolidated corpus as a **free,
no-auth open-data bulk dataset**: **LEGI** — the full consolidated text of
national codes, laws and regulations (codes, lois, ordonnances, décrets…) back
to 1945. We use LEGI and explicitly **do not** use the PISTE API.

- Bulk root: `https://echanges.dila.gouv.fr/OPENDATA/LEGI/`
  (a global tarball `Freemium_legi_global_*.tar.gz` plus daily incremental
  `LEGI_<date>.tar.gz` deltas). Mirrored on data.gouv.fr.
- data.gouv.fr dataset page: *"LEGI, codes, lois et règlements consolidés"*.

Verified live (2026-05-27): the directory listing is publicly browsable, the
tarballs download without credentials, and the dataset page states the
**Licence Ouverte / Open Licence** (Etalab).

### Format

LEGI is **not** a query API: it is a tarball of one XML file per object, laid
out under sharded paths derived from the object's id, e.g.

```
LEGI/TEXT/00/00/06/07/07/LEGITEXT000006070721.xml   (a CODE — the "version")
LEGI/TEXTELR/.../LEGITEXT000006070721.xml           (its structure / TOC)
LEGI/ARTI/00/00/06/41/92/LEGIARTI000006419280.xml   (one article)
```

Object kinds (DTD LEGIFRANCE):

| Element | Role | Key fields |
|---------|------|-----------|
| `TEXTE_VERSION` | the FRBR **expression** of a text | `META_COMMUN/ID`, `META_TEXTE_CHRONICLE/{CID,DATE_TEXTE,DATE_PUBLI}`, `META_TEXTE_VERSION/{TITRE,TITREFULL,NATURE,DATE_DEBUT,DATE_FIN,ETAT}` |
| `TEXTELR` | the text's **structure** (TOC) | `STRUCT/LIEN_ART` entries (`id,num,etat,debut,fin`) |
| `ARTICLE` | one **article** version | `META_ARTICLE/{NUM,ETAT,DATE_DEBUT,DATE_FIN}`, `BLOC_TEXTUEL/CONTENU`, `LIENS/LIEN` |

### License

- French **normative texts are not objects of copyright** — *Code de la
  propriété intellectuelle* art. L.122-5 2° excludes *"les lois, les décrets et
  tous les actes officiels"* type material from protection; official texts are
  freely reproducible.
- The LEGI dataset is additionally published under the **Licence Ouverte /
  Open Licence** (Etalab) — reuse permitted **with attribution** to the source.

Attribution: **DILA / Légifrance — Licence Ouverte (Etalab)**.

## Decision

France ingestion pulls from the **DILA LEGI open-data XML corpus**, never the
OAuth-gated PISTE API and never scraping the Légifrance UI. The FR importer:

1. takes a set of text CIDs (`LEGITEXT…`) to import;
2. fetches each text's `TEXTE_VERSION` and `TEXTELR` XML at its sharded path;
3. when `-articles` is set, fetches each member `ARTICLE` XML listed in the
   struct and parses its `BLOC_TEXTUEL/CONTENU`;
4. maps each text into ELI RDF per `docs/ontology.md`.

### Mapping decisions

- **Identity**: the text CID (`LEGITEXT000006070721`) is both `eli:id_local`
  and the work `Number`; `Year` is taken from `DATE_TEXTE` (falling back to
  `DATE_PUBLI`, then `DATE_DEBUT`), ignoring LEGI's open-end sentinel
  `2999-01-01`. `Country="fr"`. `SourceURL` =
  `https://www.legifrance.gouv.fr/codes/texte_lc/<CID>`.
- **Type slug** (`eli:type_document`): from `NATURE`, with a title/nature
  override for codes — see `fr/README.md`. `CODE`→`code`, `LOI`→`loi`,
  `ORDONNANCE`→`ordonnance`, `DECRET`→`decret`, `ARRETE`→`arrete`; else an
  ASCII-folded slug of the nature.
- **Status**: from `META_TEXTE_VERSION/ETAT` — `VIGUEUR`/`VIGUEUR_DIFF` → in
  force; `ABROGE`/`PERIME`/`ANNULE`/… → not in force.
- **As-of date** (`eli:version_date`, MANDATORY): the version's `DATE_DEBUT`
  — the date this consolidated state took effect — falling back to
  `DATE_PUBLI` then `DATE_TEXTE`. `DATE_DEBUT` also →
  `eli:first_date_entry_in_force`.
- **Articles**: walk `TEXTELR/STRUCT/LIEN_ART` for document order; for each,
  parse the corresponding `ARTICLE` XML; `NUM`→`lex:number`, `"Article <num>"`
  →`skos:prefLabel`, `BLOC_TEXTUEL/CONTENU` (whitespace-collapsed) →`lex:text`.
  Titles carry `@fr`; `eli:language` uses the FRA authority URI.
- **Relations**: each article's outgoing `LIENS/LIEN` with a `cidtexte`
  resolves to a target work URI; `typelien` `CITATION`→`eli:cites`,
  `MODIFICATION`→`eli:amends`, `ABROGATION`→`eli:repeals`.

## Consequences

- Legally clean (official texts non-copyrightable + Licence Ouverte), no OAuth,
  no HTML crawling.
- The corpus is huge (the global tarball is ~1.1 GB; ~97k+ texts). v1 imports a
  **caller-supplied set of CIDs**; data is never committed (`./fr/data/`
  gitignored). Tests run fully offline against a **tiny committed slice**: one
  `TEXTE_VERSION` (Code civil), its `TEXTELR`, and two `ARTICLE` files.
- **Next phase** (scoped out of v1):
  - *Relation target typing.* A LEGI `LIEN`'s `cidtexte` gives the target CID
    but not its nature/year, so emitted edges currently use a placeholder
    `texte`/year-0 work URI. Resolving targets to their real type/year needs a
    second pass over the corpus (build a CID→{nature,year} index first).
  - *Bulk discovery / incremental updates.* Walking the global tarball to
    enumerate all CIDs, and applying the daily `LEGI_<date>.tar.gz` deltas.
  - *Sub-article granularity* (sections, alinéas) and point-in-time revisions
    (LEGI carries every article version with `DATE_DEBUT`/`DATE_FIN`); v1 stores
    the current consolidated text only — additive later, no schema change.
- No network in tests — real-shaped LEGI XML fixtures are committed under
  `fr/scripts/legi/testdata/` and served via `httptest`.
