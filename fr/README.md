# France (`fr`)

Scrapers and configuration for ingesting French legislation into the `lex`
RDF graph. See the root [`README`](../README.md) and
[`docs/ontology.md`](../docs/ontology.md) for the contract these scrapers obey.

## Source — the DILA LEGI open-data bulk dataset (NOT the PISTE API)

France's Légifrance portal exposes a modern query API (**PISTE**), but it
**requires OAuth client credentials** — an authenticated channel our importers
must not depend on. Instead we use the **DILA LEGI open-data bulk dataset**: the
full consolidated text of national codes, laws and regulations, published as
free, no-auth XML. See [ADR-0016](../docs/adr/0016-fr-ingest-dila-legi-opendata.md).

- Bulk root: `https://echanges.dila.gouv.fr/OPENDATA/LEGI/`
  (global tarball `Freemium_legi_global_*.tar.gz` + daily delta tarballs).
- Mirror / dataset page: data.gouv.fr — *"LEGI, codes, lois et règlements
  consolidés"*.

LEGI is a tarball of **one XML file per object**, under sharded paths derived
from each object's id:

| Object | Path shape | Contents |
|--------|-----------|----------|
| `TEXTE_VERSION` | `TEXT/<shard>/LEGITEXT….xml` | consolidated version: title, nature, dates, in-force state (the FRBR expression) |
| `TEXTELR` | `TEXTELR/<shard>/LEGITEXT….xml` | structure / TOC — member articles via `<LIEN_ART>` |
| `ARTICLE` | `ARTI/<shard>/LEGIARTI….xml` | one article: `NUM`, `ETAT`, dates, `<BLOC_TEXTUEL><CONTENU>`, `<LIENS>` |

A text's CID (e.g. `LEGITEXT000006070721`, the Code civil) maps to the human
page `https://www.legifrance.gouv.fr/codes/texte_lc/<CID>` — stored as
`lex:sourceURL`.

## Legal status & license

- French **normative texts are not objects of copyright** (*Code de la propriété
  intellectuelle* art. L.122-5 2° — laws, decrees and official acts).
- The LEGI dataset is published under the **Licence Ouverte / Open Licence**
  (Etalab); reuse is permitted **with attribution**.
- We attribute **DILA / Légifrance — Licence Ouverte (Etalab)** and preserve
  `lex:sourceURL` per record.

## Act-type mapping (LEGI `NATURE` → ELI `eli:type_document` slug)

| LEGI nature | slug | note |
|-------------|------|------|
| CODE (or title contains *code*) | `code` | a code |
| LOI | `loi` | a law |
| ORDONNANCE | `ordonnance` | ordinance |
| DECRET | `decret` | decree |
| ARRETE | `arrete` | order |
| *(other)* | ASCII-folded slug of the nature | fallback |

Identity: work `Number` and `eli:id_local` are the text CID (`LEGITEXT…`);
`Year` is from `DATE_TEXTE` (fallback `DATE_PUBLI`, then `DATE_DEBUT`), ignoring
LEGI's open-end sentinel `2999-01-01`.

## Directories

- `scripts/` — Go importer (see below).
- `data/` — built artifacts (**gitignored**). Either built locally or downloaded
  from GitHub Releases.

## Importer

```bash
# A single consolidated text (Code civil) with article text:
go run ./fr/scripts/import -out fr/data/graph -cids LEGITEXT000006070721 -articles

# Several texts:
go run ./fr/scripts/import -out fr/data/graph -cids LEGITEXT000006070721,LEGITEXT000006070719
```

Flags: `-out`, `-base`, `-ua`, `-cids` (comma-separated `LEGITEXT…`),
`-articles`, `-rps` (request rate limit, default 5/s).

`-base` must point at an HTTP-served extraction of the dataset's sharded `LEGI/`
tree (extract a tarball and serve its directory, or point at a mirror).

- `scripts/legi/` — pure, offline parser + mapper: `TEXTE_VERSION` / `TEXTELR` /
  `ARTICLE` XML → `schema.Act`. Golden-tested on committed real-shaped fixtures.
- `scripts/importer/` — fetch (network, rate-limited + backoff) + build + write
  to the Badger store; tested end-to-end via `httptest` serving the fixtures.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from the version's `DATE_DEBUT`
(falling back to `DATE_PUBLI`/`DATE_TEXTE`); status from `ETAT`; amends / repeals
/ cites edges from the articles' outgoing `<LIENS>`.

## Status

✅ Metadata + article-text pass works end-to-end (identity, title, version date,
status, source URL, `lex:Article`s, plus citation/amendment/repeal edges from
article `<LIENS>`).
🚧 Next (see ADR-0016): typed relation targets (resolve a `LIEN`'s `cidtexte`
to the target's real type/year via a CID index); bulk CID discovery + daily
delta tarballs; sub-article granularity (sections/alinéas) and point-in-time
revisions; then the country-agnostic MCP server + search index (shared with
UA/PL).
