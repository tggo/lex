package importer

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/tggo/lex/internal/mcp"
	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

var update = flag.Bool("update", false, "update golden files")

// TestMCPQueries_golden is the full stack as a test: import the clml fixture
// into a real Badger store + FTS index, then drive the MCP Service exactly as a
// client would (search, get_act, get_article, list_amendments, find_related)
// and assert the answers against a golden file. No binary, no network.
func TestMCPQueries_golden(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	root := t.TempDir()
	cfg := baseCfg(t, srv)
	cfg.OutDir = filepath.Join(root, "graph")
	cfg.IndexPath = filepath.Join(root, "index.fts")
	cfg.Lang = "en"
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
	act := schema.ResourceURI("uk", "ukpga", 2023, "57")

	results := map[string]any{}
	q := func(label string, v any, err error) {
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		results[label] = v
	}

	// Title word.
	r1, e1 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "insurance", Limit: 5})
	q("search_insurance", r1, e1)
	// Article-text word.
	r2, e2 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "regulation", Limit: 5})
	q("search_regulation", r2, e2)
	// Retrieve the act with its articles.
	r3, e3 := svc.GetAct(ctx, &mcp.ActIn{URI: act})
	q("get_act", r3, e3)
	// A single article (section).
	r4, e4 := svc.GetArticle(ctx, &mcp.ArticleIn{ActURI: act, Number: "1"})
	q("get_article_1", r4, e4)
	// Amend/repeal edges.
	r5, e5 := svc.ListAmendments(ctx, &mcp.RelIn{URI: act})
	q("list_amendments", r5, e5)
	// Citation edges (includes ukpga/2024/5).
	r6, e6 := svc.FindRelated(ctx, &mcp.RelIn{URI: act})
	q("find_related", r6, e6)

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
