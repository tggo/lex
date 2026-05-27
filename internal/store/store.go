// Package store wraps a goRDFlib graph as the lex triplestore. The persistent
// backend is Badger (a directory-based KV store); an in-memory variant backs
// tests. Acts are written as ELI/RDF triples (see internal/schema and
// docs/ontology.md) and read back with SPARQL. Full-text/vector search is a
// separate concern (a sibling index), not this package.
package store

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	rdf "github.com/tggo/goRDFlib"
	"github.com/tggo/goRDFlib/sparql"
	"github.com/tggo/goRDFlib/store/badgerstore"

	"github.com/tggo/lex/internal/schema"
)

const (
	nsRDF   = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	rdfType = nsRDF + "type"
)

// Store is a lex triplestore over a goRDFlib graph.
type Store struct {
	g      *rdf.Graph
	closer io.Closer // backend; nil for the in-memory store
}

// OpenMemory returns an ephemeral in-memory store, for tests and tooling.
func OpenMemory() (*Store, error) {
	return &Store{g: rdf.NewGraph()}, nil
}

// Open opens (creating if needed) a Badger-backed store rooted at dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("store: mkdir %q: %w", dir, err)
	}
	bs, err := badgerstore.New(badgerstore.WithDir(dir))
	if err != nil {
		return nil, fmt.Errorf("store: open badger %q: %w", dir, err)
	}
	return &Store{g: rdf.NewGraph(rdf.WithStore(bs)), closer: bs}, nil
}

// Close releases the backend. Safe to call on an in-memory store.
func (s *Store) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

// Len reports the number of triples held.
func (s *Store) Len() int { return s.g.Len() }

// term constructors ---------------------------------------------------------

func uri(s string) rdf.URIRef         { return rdf.NewURIRefUnsafe(s) }
func langLit(s, l string) rdf.Literal { return rdf.NewLiteral(s, rdf.WithLang(l)) }
func dateLit(t time.Time) rdf.Literal {
	return rdf.NewLiteral(t.Format("2006-01-02"), rdf.WithDatatype(rdf.XSDDate))
}
func dtLit(t time.Time) rdf.Literal {
	return rdf.NewLiteral(t.UTC().Format(time.RFC3339), rdf.WithDatatype(rdf.XSDDateTime))
}
func plainLit(s string) rdf.Literal { return rdf.NewLiteral(s) }

func typeDocURI(a *schema.Act) string {
	return schema.NSid + url.PathEscape(string(a.Country)) + "/type/" + url.PathEscape(a.TypeSlug)
}

// AddAct writes an act and its current expression as triples.
func (s *Store) AddAct(a *schema.Act) error {
	if a == nil || a.Expression == nil {
		return fmt.Errorf("store: nil act or expression")
	}
	e := a.Expression
	if e.VersionDate.IsZero() {
		return fmt.Errorf("store: expression missing version_date (ontology invariant)")
	}
	res, expr := a.ResourceURI(), a.ExpressionURI()
	g := s.g

	// Resource (work).
	g.Add(uri(res), uri(rdfType), uri(schema.ClassLegalResource))
	g.Add(uri(res), uri(schema.PredTypeDocument), uri(typeDocURI(a)))
	idLocal := a.IDLocal
	if idLocal == "" {
		idLocal = a.Number
	}
	g.Add(uri(res), uri(schema.PredIdLocal), plainLit(idLocal))
	g.Add(uri(res), uri(schema.PredIsRealizedBy), uri(expr))

	// Expression (current consolidated version).
	g.Add(uri(expr), uri(rdfType), uri(schema.ClassLegalExpression))
	g.Add(uri(expr), uri(schema.PredRealizes), uri(res))
	g.Add(uri(expr), uri(schema.PredTitle), langLit(e.Title, e.LangTag))
	if e.LangAlpha3 != "" {
		g.Add(uri(expr), uri(schema.PredLanguage), uri(schema.LanguageURI(e.LangAlpha3)))
	}
	g.Add(uri(expr), uri(schema.PredVersionDate), dateLit(e.VersionDate))
	if !e.FirstInForceDate.IsZero() {
		g.Add(uri(expr), uri(schema.PredFirstDateEntryInForce), dateLit(e.FirstInForceDate))
	}
	if u := e.Status.InForceURI(); u != "" {
		g.Add(uri(expr), uri(schema.PredInForce), uri(u))
	}
	if e.Status == schema.StatusRepealed && !e.NoLongerInForce.IsZero() {
		g.Add(uri(expr), uri(schema.PredDateNoLongerInForce), dateLit(e.NoLongerInForce))
	}
	if e.SourceURL != "" {
		g.Add(uri(expr), uri(schema.PredSourceURL), uri(e.SourceURL))
	}
	if !e.RetrievedAt.IsZero() {
		g.Add(uri(expr), uri(schema.PredRetrievedAt), dtLit(e.RetrievedAt))
	}

	// Articles.
	for _, art := range e.Articles {
		au := schema.ArticleURI(expr, art.Number)
		g.Add(uri(expr), uri(schema.PredHasArticle), uri(au))
		g.Add(uri(au), uri(rdfType), uri(schema.ClassArticle))
		g.Add(uri(au), uri(schema.PredArticleNum), plainLit(art.Number))
		if art.Label != "" {
			g.Add(uri(au), uri(schema.PredPrefLabel), langLit(art.Label, e.LangTag))
		}
		g.Add(uri(au), uri(schema.PredArticleText), langLit(art.Text, e.LangTag))
	}

	// Relationships.
	addRel := func(pred string, targets []string) {
		for _, t := range targets {
			g.Add(uri(expr), uri(pred), uri(t))
		}
	}
	addRel(schema.PredAmends, e.Amends)
	addRel(schema.PredRepeals, e.Repeals)
	addRel(schema.PredCites, e.Cites)
	addRel(schema.PredConsolidates, e.Consolidates)
	return nil
}

const prefixes = `PREFIX eli: <` + schema.NSeli + `>
PREFIX dct: <` + schema.NSdct + `>
PREFIX skos: <` + schema.NSskos + `>
PREFIX lex: <` + schema.NSlex + `>
`

// GetAct reconstructs an act from its resource URI. Returns an error if no such
// act is stored.
func (s *Store) GetAct(resURI string) (*schema.Act, error) {
	cc, slug, year, number, err := schema.ParseResourceURI(resURI)
	if err != nil {
		return nil, err
	}

	core := prefixes + fmt.Sprintf(`SELECT ?expr ?idlocal ?title ?lang ?vdate ?first ?inforce ?nolonger ?src ?retrieved WHERE {
  <%s> eli:is_realized_by ?expr .
  OPTIONAL { <%[1]s> eli:id_local ?idlocal }
  ?expr dct:title ?title ; eli:version_date ?vdate .
  OPTIONAL { ?expr eli:language ?lang }
  OPTIONAL { ?expr eli:first_date_entry_in_force ?first }
  OPTIONAL { ?expr eli:in_force ?inforce }
  OPTIONAL { ?expr eli:date_no_longer_in_force ?nolonger }
  OPTIONAL { ?expr lex:sourceURL ?src }
  OPTIONAL { ?expr lex:retrievedAt ?retrieved }
}`, resURI)

	res, err := sparql.Query(s.g, core)
	if err != nil {
		return nil, fmt.Errorf("store: core query: %w", err)
	}
	if len(res.Bindings) == 0 {
		return nil, fmt.Errorf("store: act %q not found", resURI)
	}
	row := res.Bindings[0]

	exprURI := text(row["expr"])
	e := &schema.Expression{
		Title:      text(row["title"]),
		LangTag:    lastSegment(exprURI),
		LangAlpha3: lastSegment(text(row["lang"])),
		SourceURL:  text(row["src"]),
		Status:     statusFromURI(text(row["inforce"])),
	}
	if e.VersionDate, err = parseDate(text(row["vdate"])); err != nil {
		return nil, fmt.Errorf("store: version_date: %w", err)
	}
	if v := text(row["first"]); v != "" {
		if e.FirstInForceDate, err = parseDate(v); err != nil {
			return nil, fmt.Errorf("store: first_in_force: %w", err)
		}
	}
	if v := text(row["nolonger"]); v != "" {
		if e.NoLongerInForce, err = parseDate(v); err != nil {
			return nil, fmt.Errorf("store: no_longer_in_force: %w", err)
		}
	}
	if v := text(row["retrieved"]); v != "" {
		if e.RetrievedAt, err = time.Parse(time.RFC3339, v); err != nil {
			return nil, fmt.Errorf("store: retrievedAt: %w", err)
		}
	}

	if e.Articles, err = s.articles(exprURI); err != nil {
		return nil, err
	}
	e.Amends = s.relTargets(exprURI, schema.PredAmends)
	e.Repeals = s.relTargets(exprURI, schema.PredRepeals)
	e.Cites = s.relTargets(exprURI, schema.PredCites)
	e.Consolidates = s.relTargets(exprURI, schema.PredConsolidates)

	return &schema.Act{
		Country: cc, TypeSlug: slug, Year: year, Number: number,
		IDLocal: text(row["idlocal"]), Expression: e,
	}, nil
}

func (s *Store) articles(exprURI string) ([]schema.Article, error) {
	q := prefixes + fmt.Sprintf(`SELECT ?num ?label ?text WHERE {
  <%s> lex:hasArticle ?a .
  ?a lex:number ?num ; lex:text ?text .
  OPTIONAL { ?a skos:prefLabel ?label }
}`, exprURI)
	res, err := sparql.Query(s.g, q)
	if err != nil {
		return nil, fmt.Errorf("store: articles query: %w", err)
	}
	arts := make([]schema.Article, 0, len(res.Bindings))
	for _, row := range res.Bindings {
		arts = append(arts, schema.Article{
			Number: text(row["num"]),
			Label:  text(row["label"]),
			Text:   text(row["text"]),
		})
	}
	sort.Slice(arts, func(i, j int) bool { return numLess(arts[i].Number, arts[j].Number) })
	return arts, nil
}

func (s *Store) relTargets(exprURI, pred string) []string {
	q := fmt.Sprintf(`SELECT ?t WHERE { <%s> <%s> ?t }`, exprURI, pred)
	res, err := sparql.Query(s.g, q)
	if err != nil || len(res.Bindings) == 0 {
		return nil
	}
	out := make([]string, 0, len(res.Bindings))
	for _, row := range res.Bindings {
		out = append(out, text(row["t"]))
	}
	sort.Strings(out)
	return out
}

// DumpSorted writes all triples as sorted N-Triples — deterministic output for
// golden tests.
func (s *Store) DumpSorted(w io.Writer) error {
	var lines []string
	for tr := range s.g.Triples(nil, nil, nil) {
		lines = append(lines, fmt.Sprintf("%s %s %s .", tr.Subject.N3(), tr.Predicate.N3(), tr.Object.N3()))
	}
	sort.Strings(lines)
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	return nil
}

// helpers -------------------------------------------------------------------

func text(t rdf.Term) string {
	switch v := t.(type) {
	case nil:
		return ""
	case rdf.Literal:
		return v.Lexical()
	case rdf.URIRef:
		return v.Value()
	default:
		return v.String()
	}
}

func lastSegment(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func parseDate(s string) (time.Time, error) { return time.Parse("2006-01-02", s) }

func statusFromURI(u string) schema.Status {
	switch u {
	case schema.InForceInForce:
		return schema.StatusInForce
	case schema.InForceNotInForce:
		return schema.StatusRepealed
	default:
		return schema.StatusUnknown
	}
}

// numLess orders article numbers numerically when possible, lexically otherwise.
func numLess(a, b string) bool {
	ai, aerr := strconv.Atoi(a)
	bi, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return ai < bi
	}
	return a < b
}
