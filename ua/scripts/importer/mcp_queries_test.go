package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tggo/lex/internal/mcp"
	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

var update = flag.Bool("update", false, "update golden files")

// TestMCPQueries_golden is the full stack as a test: import the fixtures into a
// real Badger store + FTS index, then drive the MCP Service exactly as a client
// would (search, get_act, get_article, list_amendments, find_related) and assert
// the answers against a golden file. No binary, no network.
func TestMCPQueries_golden(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	root := t.TempDir()
	cfg := Config{
		BaseURL:       srv.URL,
		OutDir:        filepath.Join(root, "graph"),
		IndexPath:     filepath.Join(root, "index.fts"),
		UA:            "lex-test",
		Client:        srv.Client(),
		Now:           time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		WithArticles:  true,
		WithRelations: true,
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("import: %v", err)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	idx, err := search.Open(cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()

	svc := mcp.NewService(st, idx)
	ctx := context.Background()
	act4840 := schema.ResourceURI("ua", "zakon", 2026, "4840-20")

	// Each query is run through the real MCP Service; results go into the golden.
	results := map[string]any{}
	q := func(label string, v any, err error) {
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		results[label] = v
	}

	// Full-text search: a word that lives in an act's ARTICLE TEXT (not its title).
	r1, e1 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "терміни"})
	q("search_терміни", r1, e1)
	// Search over titles.
	r2, e2 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "нагляду", Limit: 5})
	q("search_нагляду", r2, e2)
	// Retrieve an act with its as-of date and articles.
	r3, e3 := svc.GetAct(ctx, &mcp.ActIn{URI: act4840})
	q("get_act_4840", r3, e3)
	// A single article.
	r4, e4 := svc.GetArticle(ctx, &mcp.ArticleIn{ActURI: act4840, Number: "1"})
	q("get_article_4840_1", r4, e4)
	// Amend/repeal edges resolved from the act's links via the global doc index.
	r5, e5 := svc.ListAmendments(ctx, &mcp.RelIn{URI: act4840})
	q("list_amendments_4840", r5, e5)
	// Citation edges.
	r6, e6 := svc.FindRelated(ctx, &mcp.RelIn{URI: act4840})
	q("find_related_4840", r6, e6)

	got, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "mcp_queries.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("MCP query results differ from golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
