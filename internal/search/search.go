// Package search is the lex full-text index: a SQLite FTS5 table, decoupled
// from the RDF triplestore (ADR-0010). It indexes act titles and article text
// and returns ranked hits that point back into the graph by URI.
//
// Matching is stem-based: the indexed column holds language-stemmed tokens so
// inflected forms match (Ukrainian оренда/оренду/оренди → оренд). The index
// records its language so serving selects the same Stemmer. Depends only on
// schema.
package search

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // pure-Go driver, registers "sqlite"

	"github.com/tggo/lex/internal/schema"
)

// Kinds of indexed document.
const (
	KindTitle   = "title"
	KindArticle = "article"
)

// Hit is one search result, resolvable back into the graph via its URIs.
type Hit struct {
	URI     string // the matched node (expression for titles, article for articles)
	ActURI  string // owning act (LegalResource) URI
	Kind    string // KindTitle | KindArticle
	Snippet string // matched text excerpt, original (inflected) forms highlighted
}

// Index is a stem-based FTS5 full-text index.
type Index struct {
	db   *sql.DB
	stem Stemmer
	lang string
}

// Open opens (creating if needed) a file-backed index, selecting the stemmer
// from the language stored in the index (identity if none).
func Open(path string) (*Index, error) { return open(path, "") }

// OpenLang opens an index and sets its language (and stemmer). Use this when
// building an index for a known-language corpus; the language is persisted so
// Open can pick the same stemmer later.
func OpenLang(path, lang string) (*Index, error) { return open(path, lang) }

// OpenMemory opens an ephemeral in-memory index, for tests and tooling.
func OpenMemory() (*Index, error) { return open(":memory:", "") }

// OpenMemoryLang is OpenMemory with a language set.
func OpenMemoryLang(lang string) (*Index, error) { return open(":memory:", lang) }

func open(dsn, lang string) (*Index, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("search: open %q: %w", dsn, err)
	}
	idx := &Index{db: db}
	if err := idx.init(lang); err != nil {
		db.Close()
		return nil, err
	}
	return idx, nil
}

func (i *Index) init(lang string) error {
	if _, err := i.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS docs USING fts5(
		uri UNINDEXED, act_uri UNINDEXED, kind UNINDEXED, text UNINDEXED, stem,
		tokenize='unicode61'
	)`); err != nil {
		return fmt.Errorf("search: create index: %w", err)
	}
	if _, err := i.db.Exec(`CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT)`); err != nil {
		return fmt.Errorf("search: create meta: %w", err)
	}
	// Resolve language: explicit arg wins and is persisted; otherwise read it back.
	if lang != "" {
		if _, err := i.db.Exec(`INSERT INTO meta(key,value) VALUES('lang',?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, lang); err != nil {
			return fmt.Errorf("search: set lang: %w", err)
		}
		i.lang = lang
	} else {
		_ = i.db.QueryRow(`SELECT value FROM meta WHERE key='lang'`).Scan(&i.lang)
	}
	i.stem = StemmerFor(i.lang)
	return nil
}

// Lang reports the index language ("" if unset).
func (i *Index) Lang() string { return i.lang }

// Close releases the underlying database.
func (i *Index) Close() error { return i.db.Close() }

// AddAct indexes an act's title and its articles' text.
func (i *Index) AddAct(a *schema.Act) error {
	if a == nil || a.Expression == nil {
		return fmt.Errorf("search: nil act or expression")
	}
	actURI := a.ResourceURI()
	exprURI := a.ExpressionURI()
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO docs(uri, act_uri, kind, text, stem) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	add := func(uri, kind, text string) error {
		_, err := stmt.Exec(uri, actURI, kind, text, stemColumn(i.stem, text))
		return err
	}
	if err := add(exprURI, KindTitle, a.Expression.Title); err != nil {
		return fmt.Errorf("search: index title: %w", err)
	}
	for _, art := range a.Expression.Articles {
		body := art.Label
		if art.Text != "" {
			body += "\n" + art.Text
		}
		if err := add(schema.ArticleURI(exprURI, art.Number), KindArticle, body); err != nil {
			return fmt.Errorf("search: index article %s: %w", art.Number, err)
		}
	}
	return tx.Commit()
}

// RemoveAct deletes all indexed documents of an act (idempotent).
func (i *Index) RemoveAct(actURI string) error {
	if _, err := i.db.Exec(`DELETE FROM docs WHERE act_uri = ?`, actURI); err != nil {
		return fmt.Errorf("search: remove %s: %w", actURI, err)
	}
	return nil
}

// ReplaceAct re-indexes an act idempotently (remove then add). For re-imports.
func (i *Index) ReplaceAct(a *schema.Act) error {
	if a == nil || a.Expression == nil {
		return fmt.Errorf("search: nil act or expression")
	}
	if err := i.RemoveAct(a.ResourceURI()); err != nil {
		return err
	}
	return i.AddAct(a)
}

// Count returns the number of indexed documents.
func (i *Index) Count() (int, error) {
	var n int
	if err := i.db.QueryRow(`SELECT count(*) FROM docs`).Scan(&n); err != nil {
		return 0, fmt.Errorf("search: count: %w", err)
	}
	return n, nil
}

// Search runs a stemmed full-text query and returns ranked hits (best first).
func (i *Index) Search(query string, limit int) ([]Hit, error) {
	stems := i.queryStems(query)
	if len(stems) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := i.db.Query(`SELECT uri, act_uri, kind, text FROM docs
		WHERE docs MATCH ? ORDER BY rank LIMIT ?`, toMatch(stems), limit)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()

	want := make(map[string]bool, len(stems))
	for _, s := range stems {
		want[s] = true
	}
	var hits []Hit
	for rows.Next() {
		var h Hit
		var text string
		if err := rows.Scan(&h.URI, &h.ActURI, &h.Kind, &text); err != nil {
			return nil, err
		}
		h.Snippet = i.snippet(text, want, 14)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// queryStems tokenizes and stems the user query.
func (i *Index) queryStems(query string) []string {
	toks := tokenize(query)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if s := i.stem.Stem(strings.ToLower(t)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// toMatch quotes each stem and ANDs them into a safe FTS5 MATCH expression.
func toMatch(stems []string) string {
	q := make([]string, len(stems))
	for n, s := range stems {
		q[n] = `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return strings.Join(q, " ")
}

// snippet builds an excerpt around the first matching word, highlighting the
// original (inflected) forms whose stem the query matched. Done in Go because
// the indexed column holds stems, not the original text.
func (i *Index) snippet(text string, wantStems map[string]bool, window int) string {
	words := strings.Fields(text)
	hit := -1
	for n, w := range words {
		if wantStems[i.stem.Stem(strings.ToLower(strings.Trim(w, ".,;:()[]«»\"'")))] {
			hit = n
			break
		}
	}
	start, end := 0, len(words)
	if hit >= 0 {
		start = max(0, hit-window/2)
		end = min(len(words), hit+window/2+1)
	} else if len(words) > window {
		end = window
	}
	out := make([]string, 0, end-start)
	for _, w := range words[start:end] {
		if wantStems[i.stem.Stem(strings.ToLower(strings.Trim(w, ".,;:()[]«»\"'")))] {
			out = append(out, "["+w+"]")
		} else {
			out = append(out, w)
		}
	}
	s := strings.Join(out, " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(words) {
		s = s + "…"
	}
	return s
}
