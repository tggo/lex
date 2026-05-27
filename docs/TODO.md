# TODO / improvement backlog

## ★ Per-country search stemming & tokenization (REQUIRED for all countries)

Search matching is stem-based (`internal/search`): the index stores
language-stemmed tokens and records its language so serving picks the same
`Stemmer`. **Ukraine (`uk`) is implemented; every other country still uses the
identity stemmer (no stemming) and must get its own.**

What each country importer must do:

- Open its index with the language: `search.OpenLang(indexPath, "<lang>")`
  (UA does this; `jp/pl/uk/us/fr/es/...` currently call `search.Open` → identity).
- Register a `Stemmer` for that language in `internal/search` (`StemmerFor`).

Per-language notes:

- **uk** — done (lightweight rule-based suffix stripper; conservative).
- **pl, es, fr, fi, de(at/ch/lu)** — inflected; Snowball stemmers exist for most.
  Wire a Snowball port or rule-based stemmer per language.
- **en (au, ie, nz, uk, us)** — Snowball English; relatively easy.
- **ja (jp)** — ⚠️ different problem: Japanese has **no word spaces**, so
  suffix stemming is wrong. Needs a segmenting tokenizer (morphological, e.g.
  Kagome/MeCab, or bigram/n-gram tokenization) at index *and* query time.
  Treat tokenization, not just stemming, as the per-language plug-point.

Design direction: make the index's analysis pipeline (tokenizer + stemmer) a
language module selected by the dataset language — not just a stemmer. The UA
work already added the language registry hook (`StemmerFor`); generalize it to
a `TextAnalyzer` interface (tokenize → normalize → stem) so CJK languages fit.

## Search quality

- **Index article text by default.** Without `-articles` only titles are
  indexed, so intent queries ("як оформити спадщину") miss — the answer lives in
  article bodies. Either default `-articles` on, or always index article text.
- **Better snippets/ranking.** Current snippet is a simple Go window; consider
  BM25 column weighting (title > article) and multi-term highlight quality.
- **Return the act title with article hits** so a hit is self-describing
  (currently an article hit only carries URIs + snippet).
- **Semantic / vector search** as a later layer for true natural-language
  questions (embeddings over articles), complementing FTS.
- **Stemmer hardening:** stop-list for very common tokens; expand UA endings and
  add exceptions; guard against over-merging with tests per language.

## Data & graph

- **Inverse relation edges:** populate `eli:amended_by` / `eli:repealed_by` by
  inverting resolved `amends`/`repeals` across the corpus (cross-act pass).
- **Revision history (P3, ADR-0014):** multi-expression per act; JP/e-Gov has
  per-revision text and should drive it; UA contributes revision *dates*
  (`ogd.ParseHistory`).
- **Cross-dataset relation coverage:** relation targets resolve better as more
  document types are ingested; consider a shared global id→URI map per country.

## Ops

- **Prebuilt dataset Releases + CI** (`go test -cover` gate >80%, build binary,
  publish archived datasets per country).
- **`cmd/lex` logs the dataset language** alongside the served country.
