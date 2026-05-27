package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

func sampleAct() *schema.Act {
	return &schema.Act{
		Country: "ua", TypeSlug: "kodeks", Year: 2003, Number: "435-15",
		Expression: &schema.Expression{
			Title:   "Цивільний кодекс України",
			LangTag: "uk", LangAlpha3: "UKR",
			VersionDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Status:      schema.StatusInForce,
			SourceURL:   "https://zakon.rada.gov.ua/laws/show/435-15",
			Articles: []schema.Article{
				{Number: "1", Label: "Стаття 1", Text: "Цивільні відносини регулюються цим Кодексом."},
			},
			Cites:     []string{schema.ResourceURI("ua", "konstytutsiya", 1996, "254к/96-вр")},
			AmendedBy: []string{schema.ResourceURI("ua", "zakon", 2025, "1-25")},
		},
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	st, err := store.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	idx, err := search.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { idx.Close() })
	if err := st.AddAct(sampleAct()); err != nil {
		t.Fatal(err)
	}
	if err := idx.AddAct(sampleAct()); err != nil {
		t.Fatal(err)
	}
	return NewService(st, idx)
}

func TestBuildIndex(t *testing.T) {
	st, _ := store.OpenMemory()
	defer st.Close()
	if err := st.AddAct(sampleAct()); err != nil {
		t.Fatal(err)
	}
	idx, _ := search.OpenMemory()
	defer idx.Close()
	if err := BuildIndex(st, idx); err != nil {
		t.Fatal(err)
	}
	hits, _ := idx.Search("кодекс", 5)
	if len(hits) == 0 {
		t.Error("BuildIndex produced no searchable docs")
	}
}

func TestCountries(t *testing.T) {
	st, _ := store.OpenMemory()
	defer st.Close()
	_ = st.AddAct(sampleAct()) // ua

	if ccs, err := Countries(st); err != nil || len(ccs) != 1 || ccs[0] != "ua" {
		t.Fatalf("Countries = %v, err = %v; want [ua]", ccs, err)
	}

	// Add a second country: detection must report both (a mixed dataset).
	jp := &schema.Act{
		Country: "jp", TypeSlug: "act", Year: 2020, Number: "129AC0000000089",
		Expression: &schema.Expression{
			Title: "Civil Code", LangTag: "ja",
			VersionDate: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	_ = st.AddAct(jp)
	ccs, err := Countries(st)
	if err != nil {
		t.Fatal(err)
	}
	if len(ccs) != 2 || ccs[0] != "jp" || ccs[1] != "ua" {
		t.Errorf("Countries = %v, want [jp ua]", ccs)
	}
}

func TestSearchLaws(t *testing.T) {
	svc := newTestService(t)
	out, err := svc.SearchLaws(context.Background(), &SearchIn{Query: "кодекс"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Hits) == 0 {
		t.Fatal("expected hits")
	}
	if out.Hits[0].ActURI != sampleAct().ResourceURI() {
		t.Errorf("hit act_uri = %q", out.Hits[0].ActURI)
	}
}

func TestGetAct(t *testing.T) {
	svc := newTestService(t)
	out, err := svc.GetAct(context.Background(), &ActIn{URI: sampleAct().ResourceURI()})
	if err != nil {
		t.Fatal(err)
	}
	if out.Act.Title != "Цивільний кодекс України" {
		t.Errorf("title = %q", out.Act.Title)
	}
	if out.Act.Status != "in_force" {
		t.Errorf("status = %q, want in_force", out.Act.Status)
	}
	if out.Act.VersionDate != "2026-01-01" {
		t.Errorf("version_date = %q", out.Act.VersionDate)
	}
	if len(out.Act.Articles) != 1 {
		t.Errorf("articles = %d, want 1", len(out.Act.Articles))
	}
	if _, err := svc.GetAct(context.Background(), &ActIn{URI: "https://lex.dev/eli/ua/zakon/1/x"}); err == nil {
		t.Error("expected error for missing act")
	}
}

func TestGetArticle(t *testing.T) {
	svc := newTestService(t)
	out, err := svc.GetArticle(context.Background(), &ArticleIn{ActURI: sampleAct().ResourceURI(), Number: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Article.Number != "1" || out.Article.Text == "" {
		t.Errorf("article = %+v", out.Article)
	}
	if _, err := svc.GetArticle(context.Background(), &ArticleIn{ActURI: sampleAct().ResourceURI(), Number: "999"}); err == nil {
		t.Error("expected error for missing article")
	}
}

func TestRelations(t *testing.T) {
	svc := newTestService(t)
	am, err := svc.ListAmendments(context.Background(), &RelIn{URI: sampleAct().ResourceURI()})
	if err != nil {
		t.Fatal(err)
	}
	if len(am.AmendedBy) != 1 {
		t.Errorf("amended_by = %v", am.AmendedBy)
	}
	rel, err := svc.FindRelated(context.Background(), &RelIn{URI: sampleAct().ResourceURI()})
	if err != nil {
		t.Fatal(err)
	}
	if len(rel.Cites) != 1 {
		t.Errorf("cites = %v", rel.Cites)
	}
}

// TestServer_e2e drives the real MCP server over an in-memory transport.
func TestServer_e2e(t *testing.T) {
	svc := newTestService(t)
	srv := NewServer(svc)

	ctx := context.Background()
	clientT, serverT := sdk.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &sdk.CallToolParams{
		Name:      "get_act",
		Arguments: map[string]any{"uri": sampleAct().ResourceURI()},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("tool reported error: %+v", res.Content)
	}
	// The structured output is also rendered as JSON text content.
	var joined string
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			joined += tc.Text
		}
	}
	if !strings.Contains(joined, "Цивільний кодекс України") {
		t.Errorf("response missing title; content = %q", joined)
	}
}
