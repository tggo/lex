// Package mcp exposes the lex store and search index as MCP tools. It is the
// serving layer: country-agnostic, it answers over whatever acts are present.
// Handlers are plain methods (offline-testable); server.go wires them to the
// MCP protocol.
package mcp

import (
	"context"
	"fmt"
	"sort"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
)

// actStore is the subset of *store.Store the service needs (kept small so tests
// can use the real store without mocking).
type actStore interface {
	GetAct(resURI string) (*schema.Act, error)
}

// searchIndex is the subset of *search.Index the service needs.
type searchIndex interface {
	Search(query string, limit int) ([]search.Hit, error)
}

// actLister and actIndexer let BuildIndex populate a search index from a store
// without depending on the concrete types (keeps the cmd shim logic-free).
type actLister interface {
	EachAct(func(*schema.Act) error) error
}
type actIndexer interface {
	AddAct(*schema.Act) error
}

// BuildIndex feeds every act in the store into the search index.
func BuildIndex(st actLister, idx actIndexer) error {
	return st.EachAct(idx.AddAct)
}

// resourceLister lists act resource URIs (satisfied by *store.Store).
type resourceLister interface {
	ListResourceURIs() ([]string, error)
}

// Countries returns the distinct country codes present in a store, derived from
// the country-namespaced resource URIs (e.g. eli/ua/…, eli/jp/…). Used to keep a
// single lex instance to one country so answers are never mixed.
func Countries(st resourceLister) ([]string, error) {
	uris, err := st.ListResourceURIs()
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, u := range uris {
		if cc, _, _, _, err := schema.ParseResourceURI(u); err == nil {
			set[string(cc)] = true
		}
	}
	out := make([]string, 0, len(set))
	for cc := range set {
		out = append(out, cc)
	}
	sort.Strings(out)
	return out, nil
}

// Service answers lex queries from the store and search index.
type Service struct {
	store actStore
	index searchIndex
}

// NewService builds a Service. Accepts the concrete *store.Store / *search.Index
// (which satisfy the interfaces) or any compatible implementation.
func NewService(st actStore, idx searchIndex) *Service {
	return &Service{store: st, index: idx}
}

// ---- views (JSON-friendly projections returned to MCP clients) ----

type ArticleView struct {
	Number string `json:"number"`
	Label  string `json:"label,omitempty"`
	Text   string `json:"text,omitempty"`
}

type ActView struct {
	URI         string        `json:"uri"`
	Country     string        `json:"country"`
	Type        string        `json:"type"`
	Year        int           `json:"year"`
	Number      string        `json:"number"`
	Title       string        `json:"title"`
	VersionDate string        `json:"version_date"` // YYYY-MM-DD (as-of date)
	Status      string        `json:"status"`       // in_force | repealed | unknown
	SourceURL   string        `json:"source_url,omitempty"`
	Articles    []ArticleView `json:"articles,omitempty"`
}

func statusString(s schema.Status) string {
	switch s {
	case schema.StatusInForce:
		return "in_force"
	case schema.StatusRepealed:
		return "repealed"
	default:
		return "unknown"
	}
}

func toActView(a *schema.Act) ActView {
	e := a.Expression
	v := ActView{
		URI:         a.ResourceURI(),
		Country:     string(a.Country),
		Type:        a.TypeSlug,
		Year:        a.Year,
		Number:      a.Number,
		Title:       e.Title,
		VersionDate: e.VersionDate.Format("2006-01-02"),
		Status:      statusString(e.Status),
		SourceURL:   e.SourceURL,
	}
	for _, art := range e.Articles {
		v.Articles = append(v.Articles, ArticleView(art))
	}
	return v
}

// ---- tool inputs/outputs ----

type SearchIn struct {
	Query string `json:"query" jsonschema:"full-text query (Ukrainian or other indexed language terms)"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of hits (default 20)"`
}
type SearchHit struct {
	URI     string `json:"uri"`
	ActURI  string `json:"act_uri"`
	Kind    string `json:"kind"` // title | article
	Snippet string `json:"snippet"`
}
type SearchOut struct {
	Hits []SearchHit `json:"hits"`
}

type ActIn struct {
	URI string `json:"uri" jsonschema:"act resource URI (e.g. an 'act_uri' from search_laws)"`
}
type ActOut struct {
	Act ActView `json:"act"`
}

type ArticleIn struct {
	ActURI string `json:"act_uri" jsonschema:"act resource URI"`
	Number string `json:"number" jsonschema:"article number, e.g. '1' or '1-1'"`
}
type ArticleOut struct {
	Article ArticleView `json:"article"`
}

type RelIn struct {
	URI string `json:"uri" jsonschema:"act resource URI"`
}
type AmendOut struct {
	Amends    []string `json:"amends"`
	AmendedBy []string `json:"amended_by"`
	Repeals   []string `json:"repeals"`
}
type RelatedOut struct {
	Cites        []string `json:"cites"`
	Consolidates []string `json:"consolidates"`
}

// ---- handlers ----

// SearchLaws runs a full-text search over titles and article text.
func (s *Service) SearchLaws(_ context.Context, in *SearchIn) (SearchOut, error) {
	hits, err := s.index.Search(in.Query, in.Limit)
	if err != nil {
		return SearchOut{}, err
	}
	out := SearchOut{Hits: make([]SearchHit, 0, len(hits))}
	for _, h := range hits {
		out.Hits = append(out.Hits, SearchHit(h))
	}
	return out, nil
}

// GetAct returns an act's metadata and articles.
func (s *Service) GetAct(_ context.Context, in *ActIn) (ActOut, error) {
	a, err := s.store.GetAct(in.URI)
	if err != nil {
		return ActOut{}, err
	}
	return ActOut{Act: toActView(a)}, nil
}

// GetArticle returns one article of an act.
func (s *Service) GetArticle(_ context.Context, in *ArticleIn) (ArticleOut, error) {
	a, err := s.store.GetAct(in.ActURI)
	if err != nil {
		return ArticleOut{}, err
	}
	for _, art := range a.Expression.Articles {
		if art.Number == in.Number {
			return ArticleOut{Article: ArticleView(art)}, nil
		}
	}
	return ArticleOut{}, fmt.Errorf("article %q not found in %s", in.Number, in.ActURI)
}

// ListAmendments returns the amend/repeal relations of an act.
func (s *Service) ListAmendments(_ context.Context, in *RelIn) (AmendOut, error) {
	a, err := s.store.GetAct(in.URI)
	if err != nil {
		return AmendOut{}, err
	}
	e := a.Expression
	return AmendOut{Amends: e.Amends, AmendedBy: e.AmendedBy, Repeals: e.Repeals}, nil
}

// FindRelated returns citation/consolidation relations of an act.
func (s *Service) FindRelated(_ context.Context, in *RelIn) (RelatedOut, error) {
	a, err := s.store.GetAct(in.URI)
	if err != nil {
		return RelatedOut{}, err
	}
	e := a.Expression
	return RelatedOut{Cites: e.Cites, Consolidates: e.Consolidates}, nil
}
