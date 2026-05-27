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

// TestMCPQueries_golden is the full stack as a test: import the Legilux SPARQL
// fixtures into a real Badger store + FTS index, then drive the MCP Service
// exactly as a client would (search, get_act, list_amendments, find_related)
// and assert the answers against a golden file. No binary, no network.
//
// The Legilux fixtures carry no article text, so there is no get_article query.
func TestMCPQueries_golden(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.IndexPath = filepath.Join(t.TempDir(), "index.fts")
	cfg.Lang = "fr"
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
	// First act's work URI carries the relations fixture (amends + cites).
	act := schema.ResourceURI("lu", "arrete", 1854, "etat/adm/a/1854/04/21/n1/jo")

	results := map[string]any{}
	q := func(label string, v any, err error) {
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		results[label] = v
	}

	// Title word from the first act's title.
	r1, e1 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "Publication"})
	q("search_publication", r1, e1)
	// Title word from another act's title.
	r2, e2 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "milice", Limit: 5})
	q("search_milice", r2, e2)
	// Retrieve the act with its as-of date.
	r3, e3 := svc.GetAct(ctx, &mcp.ActIn{URI: act})
	q("get_act_1854", r3, e3)
	// Amend edges resolved from the relations fixture.
	r4, e4 := svc.ListAmendments(ctx, &mcp.RelIn{URI: act})
	q("list_amendments_1854", r4, e4)
	// Citation edges.
	r5, e5 := svc.FindRelated(ctx, &mcp.RelIn{URI: act})
	q("find_related_1854", r5, e5)

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
