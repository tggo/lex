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

// TestMCPQueries_golden is the full stack as a test: import the fedlex SPARQL
// fixtures into a real Badger store + FTS index, then drive the MCP Service
// exactly as a client would (search, get_act, list_amendments, find_related) and
// assert the answers against a golden file. No binary, no network. The fedlex
// fixtures carry act metadata (no article bodies / relations), so this exercises
// title search across the three SR acts plus get_act.
func TestMCPQueries_golden(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	root := t.TempDir()
	cfg := baseCfg(t, srv)
	cfg.OutDir = filepath.Join(root, "graph")
	cfg.IndexPath = filepath.Join(root, "index.fts")
	cfg.Lang = "de"
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
	// SR 210 — Schweizerisches Zivilgesetzbuch (ZGB).
	zgb := schema.ResourceURI("ch", "gesetzbuch", 1912, "210")

	results := map[string]any{}
	q := func(label string, v any, err error) {
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		results[label] = v
	}

	// Full-text search: a word from the ZGB act TITLE.
	r1, e1 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "Zivilgesetzbuch"})
	q("search_zivilgesetzbuch", r1, e1)
	// A word from the StGB act TITLE.
	r2, e2 := svc.SearchLaws(ctx, &mcp.SearchIn{Query: "Strafgesetzbuch", Limit: 5})
	q("search_strafgesetzbuch", r2, e2)
	// Retrieve the ZGB act with its as-of date.
	r3, e3 := svc.GetAct(ctx, &mcp.ActIn{URI: zgb})
	q("get_act_zgb", r3, e3)
	// Amend/repeal edges (none in the fixtures).
	r4, e4 := svc.ListAmendments(ctx, &mcp.RelIn{URI: zgb})
	q("list_amendments_zgb", r4, e4)
	// Citation edges (none in the fixtures).
	r5, e5 := svc.FindRelated(ctx, &mcp.RelIn{URI: zgb})
	q("find_related_zgb", r5, e5)

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
