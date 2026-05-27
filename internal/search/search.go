// Package search is the lex full-text index: a SQLite FTS5 table, decoupled
// from the RDF triplestore (ADR-0010). It indexes act titles and article text
// and returns ranked hits that point back into the graph by URI. It depends
// only on schema, so the store/graph can be rebuilt or swapped independently.
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
	Snippet string // matched text excerpt
}

// Index is an FTS5 full-text index.
type Index struct {
	db *sql.DB
}

// Open opens (creating if needed) a file-backed index.
func Open(path string) (*Index, error) { return open(path) }

// OpenMemory opens an ephemeral in-memory index, for tests and tooling.
func OpenMemory() (*Index, error) { return open(":memory:") }

func open(dsn string) (*Index, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("search: open %q: %w", dsn, err)
	}
	idx := &Index{db: db}
	if err := idx.init(); err != nil {
		db.Close()
		return nil, err
	}
	return idx, nil
}

func (i *Index) init() error {
	_, err := i.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS docs USING fts5(
		uri UNINDEXED, act_uri UNINDEXED, kind UNINDEXED, text,
		tokenize='unicode61'
	)`)
	if err != nil {
		return fmt.Errorf("search: create index: %w", err)
	}
	return nil
}

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

	stmt, err := tx.Prepare(`INSERT INTO docs(uri, act_uri, kind, text) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	if _, err := stmt.Exec(exprURI, actURI, KindTitle, a.Expression.Title); err != nil {
		return fmt.Errorf("search: index title: %w", err)
	}
	for _, art := range a.Expression.Articles {
		body := art.Label
		if art.Text != "" {
			body += "\n" + art.Text
		}
		if _, err := stmt.Exec(schema.ArticleURI(exprURI, art.Number), actURI, KindArticle, body); err != nil {
			return fmt.Errorf("search: index article %s: %w", art.Number, err)
		}
	}
	return tx.Commit()
}

// RemoveAct deletes all indexed documents (title + articles) of an act, so a
// re-import can replace them. Safe to call when the act is absent.
func (i *Index) RemoveAct(actURI string) error {
	if _, err := i.db.Exec(`DELETE FROM docs WHERE act_uri = ?`, actURI); err != nil {
		return fmt.Errorf("search: remove %s: %w", actURI, err)
	}
	return nil
}

// ReplaceAct re-indexes an act idempotently: removes any prior docs for it, then
// adds the current ones. Use this for incremental (re-)imports.
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

// Search runs a full-text query and returns ranked hits (best first).
func (i *Index) Search(query string, limit int) ([]Hit, error) {
	match := toMatch(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := i.db.Query(`SELECT uri, act_uri, kind,
		snippet(docs, 3, '[', ']', '…', 12) FROM docs
		WHERE docs MATCH ? ORDER BY rank LIMIT ?`, match, limit)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.URI, &h.ActURI, &h.Kind, &h.Snippet); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// toMatch turns free user input into a safe FTS5 MATCH expression: each term is
// quoted (so punctuation can't break the query) and ANDed together.
func toMatch(query string) string {
	fields := strings.Fields(query)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		quoted = append(quoted, `"`+f+`"`)
	}
	return strings.Join(quoted, " ")
}
