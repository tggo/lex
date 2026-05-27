# ADR 0009 — Ukraine: ingest via official open data (OGD), not HTML scraping

- **Status**: Accepted
- **Date**: 2026-05-27

## Context

The initial plan was to scrape `zakon.rada.gov.ua`. Checking its `robots.txt`
showed `User-Agent: * → Disallow: /`: any self-identifying bot is disallowed
from the entire site (only named crawlers like Googlebot are allowed, and even
they are blocked from `/print`, `/card`, `/stru`). HTML scraping is therefore
off the table.

Investigation found the proper channel: the Verkhovna Rada publishes the
"Законодавство України" system as **open data** at `data.rada.gov.ua`, under
**CC BY 4.0** (attribution required), with legal backing from CMU Resolutions
№835/№867 and the Law "On Access to Public Information" — explicitly free to
copy, redistribute, and use commercially with a source reference.

The primary-acts dataset (`perv`) is live, machine-readable, and rebuilt
**hourly**. Verified endpoints (2026-05-27):

| File | Contents |
|------|----------|
| `…/ogd/zak/perv/meta.json` | dataset descriptor |
| `…/ogd/zak/perv/cards.{csv,json,xml}` | act cards / metadata (~7 MB) |
| `…/ogd/zak/perv/texts.{csv,json,xml}` | text index (~797 KB) |
| `…/ogd/zak/perv/text/texts.zip` | full act texts as HTML (~334 MB, 3043 files) |
| `…/ogd/zak/laws/data/csv/perv1.txt` | active act IDs (e.g. `435-15`) |
| `…/ogd/zak/laws/data/csv/perv0.txt` | inactive (repealed) act IDs |

Base host `data.rada.gov.ua`; legacy `/ogd/...` paths still serve files even
though `/ogd/<dir>/` index pages redirect to `/open/data/<id>`.

## Decision

Ukraine ingestion pulls the **official OGD datasets** (cards + texts +
active/inactive lists), not scraped HTML. The UA scraper:

1. downloads `cards.json` (metadata) and `texts.zip` (HTML bodies),
2. uses `perv1.txt`/`perv0.txt` to set in-force/repealed status,
3. maps each act into ELI RDF per `docs/ontology.md`, preserving the source URL
   (`https://zakon.rada.gov.ua/laws/show/<id>`) and `lex:retrievedAt`,
4. attributes the source per CC BY 4.0 in output and docs.

The act `id` (e.g. `435-15`) is the stable native identifier (`eli:id_local`).

## Consequences

- Legally clean, robots-compliant, and far more reliable than HTML scraping.
- Hourly updates make scheduled Release rebuilds (ADR-0006) straightforward.
- We parse the OGD's HTML *act bodies* into article structure, but we do not
  *crawl the website* — an important distinction.
- Attribution to `data.rada.gov.ua` / Verkhovna Rada (CC BY 4.0) is mandatory in
  README, dataset Releases, and ideally tool output.
- Other countries will each need their own ADR for their source + license.
