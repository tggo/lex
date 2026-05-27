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

- Bulk root: `https://echanges.dila.gouv.fr/OPENDATA/LEGI/`. DILA publishes LEGI
  **only as bulk gzip tarballs** — there are **no per-text URLs**:
  - a large **global** tarball `Freemium_legi_global_*.tar.gz` (~1.1 GB,
    refreshed ~twice a year), and
  - small **daily delta** tarballs `LEGI_YYYYMMDD-HHMMSS.tar.gz` (hundreds of KB).
- Mirror / dataset page: data.gouv.fr — *"LEGI, codes, lois et règlements
  consolidés"*.

Inside a dump, each text is **one `JORFTEXT…` directory** holding its objects;
the importer walks the tarball once and groups files by that directory, then
keys each text by the `LEGITEXT…` CID found in its version/struct filename:

| Object | Path shape inside the tarball | Contents |
|--------|-------------------------------|----------|
| `TEXTE_VERSION` | `…/JORF/TEXT/<shard>/JORFTEXT…/texte/version/LEGITEXT….xml` | consolidated version: title, nature, dates, in-force state (the FRBR expression) |
| `TEXTELR` | `…/JORFTEXT…/texte/struct/LEGITEXT….xml` | structure / TOC — member articles via `<LIEN_ART>` |
| `ARTICLE` | `…/JORFTEXT…/article/LEGI/ARTI/<shard>/LEGIARTI….xml` | one article: `NUM`, `ETAT`, dates, `<BLOC_TEXTUEL><CONTENU>`, `<LIENS>` |

A text's articles are **co-located** under its own `JORFTEXT…` subtree, so a
single pass over the tarball yields a complete CID→{version, struct, articles}
index without any further fetches.

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

The importer downloads (or opens a local) **tarball**, stream-walks it with
`archive/tar` + `compress/gzip`, builds the CID→entry index, and parses the
matching texts. It never invents per-text URLs.

```bash
# Import everything in a small daily delta (downloads over the network):
go run ./fr/scripts/import -out /tmp/fr -dump https://echanges.dila.gouv.fr/OPENDATA/LEGI/LEGI_20250712-211706.tar.gz -articles

# Same, via the -delta shortcut (builds the URL under -base):
go run ./fr/scripts/import -out /tmp/fr -delta LEGI_20250712-211706.tar.gz -articles

# An already-downloaded tarball, filtered to specific texts:
go run ./fr/scripts/import -out fr/data -dump-file legi_global.tar.gz \
    -cids LEGITEXT000006070721,LEGITEXT000006070719 -articles
```

Flags:

- `-dump <url>` — absolute URL of a `.tar.gz` to download (the global tarball or
  a daily delta).
- `-dump-file <path>` — a tarball already on disk (skips the network).
- `-delta <name|YYYYMMDD>` — convenience: resolves to `-base/<name>` (pass the
  exact `LEGI_YYYYMMDD-HHMMSS.tar.gz` filename, since the publish suffix can't
  be guessed from a bare date).
- `-base` — the `OPENDATA/LEGI` directory used to build `-delta` URLs.
- `-cids` — comma-separated `LEGITEXT…` ids to import. **If omitted, every text
  found in the tarball is imported.**
- `-articles` — also parse each text's co-located `ARTICLE` files.
- `-out`, `-ua`.

Exactly one source is used; `-dump-file` takes precedence over `-dump`/`-delta`.
The downloaded tarball is streamed to a temp file and removed when the run ends.

- `scripts/legi/` — pure, offline parser + mapper: `TEXTE_VERSION` / `TEXTELR` /
  `ARTICLE` XML → `schema.Act`. Golden-tested on committed real-shaped fixtures.
- `scripts/importer/` — tarball download + tar/gzip walk + CID index + build +
  write to the Badger store. Tested against a committed tiny `.tar.gz` fixture
  (`testdata/legi_delta_sample.tar.gz`, the legi fixtures arranged in the real
  DILA `JORFTEXT…` layout) plus an `httptest`-served download path.
- `scripts/import/` — thin CLI shim.

`version_date` (the MANDATORY as-of date) comes from the version's `DATE_DEBUT`
(falling back to `DATE_PUBLI`/`DATE_TEXTE`); status from `ETAT`; amends / repeals
/ cites edges from the articles' outgoing `<LIENS>`.

## Status

✅ Metadata + article-text pass works end-to-end (identity, title, version date,
status, source URL, `lex:Article`s, plus citation/amendment/repeal edges from
article `<LIENS>`).
✅ Bulk-dump pipeline works end-to-end: download a DILA tarball (global or daily
delta) → tar/gzip stream-walk → CID index → parse → Badger store + FTS index.
Verified live against `LEGI_20250712-211706.tar.gz` (73 acts) and
`LEGI_20250713-205013.tar.gz` (110 acts) on 2026-05-27.

🚧 Next: typed relation targets (resolve a `LIEN`'s `cidtexte` to the target's
real type/year via a CID index); sub-article granularity (sections/alinéas) and
point-in-time revisions (LEGI carries every article version with
`DATE_DEBUT`/`DATE_FIN`); then the country-agnostic MCP server + search index
(shared with UA/PL).

> **Note:** ADR-0016's "Decision" step *"fetches each text's TEXTE_VERSION and
> TEXTELR XML at its sharded path"* describes a per-text URL fetch that does not
> exist — DILA serves LEGI **only** as bulk tarballs. The implementation
> (and this README) reflect the bulk-dump model; the ADR text is superseded on
> that point. (ADR file lives outside `fr/` and is left for a follow-up edit.)
