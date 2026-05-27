# The lex ontology — the scraper ⇄ server contract

Every scraper, for every country, emits RDF triples conforming to this
document. The `lex` server only understands this vocabulary. If it isn't
described here, the server won't use it. Keep this file authoritative.

## Vocabularies used

| Prefix | Namespace | Use |
|--------|-----------|-----|
| `eli`  | `http://data.europa.eu/eli/ontology#` | legislation backbone (acts, versions, relations) |
| `dct`  | `http://purl.org/dc/terms/` | titles, dates, language, publisher |
| `skos` | `http://www.w3.org/2004/02/skos/core#` | labels, alt labels |
| `xsd`  | `http://www.w3.org/2001/XMLSchema#` | datatypes |
| `lex`  | `https://lex.dev/ontology#` | the few things ELI lacks (article nodes, source snapshot) |

ELI is chosen because it is purpose-built for legislation and models the
amend/repeal/cite web as first-class properties. Where ELI is silent (e.g. a
machine node for an individual *article* inside an act), we add minimal `lex:`
terms rather than inventing a parallel scheme.

## FRBR layering (this is how versioning works)

ELI inherits FRBR. We use two of the three levels in v1:

- **LegalResource** (`eli:LegalResource`) — the act as an abstract work,
  identity stable across all amendments. e.g. "the Civil Code of Ukraine".
- **LegalExpression** (`eli:LegalExpression`) — a concrete consolidated version
  at a point in time, in a language. v1 stores **one** expression per act: the
  current consolidated text, tagged with its `eli:version_date`.

Relationships (`eli:amends`, `eli:cites`, …) connect *expressions* where the
source provides that precision, otherwise *resources*.

## URI scheme

ELI-style, mirroring the source's stable identifiers so URIs are reconstructible:

```
Resource:    https://lex.dev/eli/<cc>/<type>/<year>/<number>
Expression:  https://lex.dev/eli/<cc>/<type>/<year>/<number>/<version_date>/<lang>
Article:     <expression-uri>/art_<n>
```

`<cc>` is ISO 3166-1 alpha-2 lowercase (`ua`). `<type>` is a country-mapped
slug (see country README; for UA: `zakon`, `kodeks`, `postanova-kmu`,
`ukaz-prezydenta`, `nakaz`, `konstytutsiya`).

## Required triples per act

### Resource (work)
```turtle
<res> a eli:LegalResource ;
  eli:type_document <type-uri> ;          # the act type
  eli:id_local "254к/96-вр" ;             # source's native id
  eli:is_realized_by <expr> .             # link to current expression
```

### Expression (current consolidated version)
```turtle
<expr> a eli:LegalExpression ;
  eli:realizes <res> ;
  dct:title "Цивільний кодекс України"@uk ;
  eli:language <http://publications.europa.eu/resource/authority/language/UKR> ;
  eli:version_date "2026-01-01"^^xsd:date ;            # as-of date (MANDATORY)
  eli:first_date_entry_in_force "2004-01-01"^^xsd:date ;
  eli:in_force <http://data.europa.eu/eli/ontology#InForce-inForce> ;  # or NotInForce
  eli:date_no_longer_in_force "..."^^xsd:date ;        # only if repealed
  lex:sourceURL <https://zakon.rada.gov.ua/laws/show/435-15> ;
  lex:retrievedAt "2026-05-27T10:00:00Z"^^xsd:dateTime ;
  lex:hasArticle <expr/art_1>, <expr/art_2> ... .
```

### Article (lex extension)
```turtle
<expr/art_1> a lex:Article ;
  lex:number "1" ;
  skos:prefLabel "Стаття 1"@uk ;
  lex:text "..." .            # plain text of the article; goes into FTS5
```

### Relationships (graph edges — queried via SPARQL)
```turtle
<expr-A> eli:amends      <res-B> .   # and/or eli:amended_by
<expr-A> eli:repeals     <res-B> .   # eli:repealed_by
<expr-A> eli:cites       <res-B> .
<expr>   eli:consolidates <res> .
```

## Mandatory invariants (server relies on these)

1. Every `eli:LegalExpression` MUST have `eli:version_date` and an in-force
   status. No version date → the scraper drops the record, never guesses.
2. Article text for FTS lives in `lex:text` on `lex:Article`; act title in
   `dct:title` on the expression.
3. All literals carry a language tag where natural language (`@uk`).
4. URIs are deterministic from source ids — re-running a scraper is idempotent.

## What the server builds from this

- **FTS5 index**: rows = (`uri`, `act_uri`, `kind` ∈ {title,article}, `text`,
  `lang`). Populated from `dct:title` and `lex:text`.
- **SPARQL queries**: `list_amendments` follows `eli:amends`/`eli:amended_by`;
  `find_related` follows `eli:cites`; `get_act` / `get_article` resolve URIs.

## Changing this contract

Bump nothing silently. A breaking change to required triples is an ADR and a
coordinated scraper update. Additive optional triples are fine.
