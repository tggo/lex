// Command lex is the MCP server: it loads a lex triplestore, builds a full-text
// index over it, and serves the lex tools over stdio. Country-agnostic — it
// serves whatever acts the store contains.
//
//	lex -data ua/data/graph
//
// Add to an MCP client (e.g. Claude Code) as a stdio server.
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tggo/lex/internal/mcp"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

func main() {
	root := flag.String("data", "ua/data", "dataset root directory (holds graph/ and index.fts)")
	allowMixed := flag.Bool("allow-mixed", false, "allow a dataset containing more than one country (answers may span countries)")
	flag.Parse()

	graphDir := filepath.Join(*root, "graph")
	indexPath := filepath.Join(*root, "index.fts")

	// Logs go to stderr; stdout is the MCP protocol channel.
	st, err := store.Open(graphDir)
	if err != nil {
		log.Fatalf("lex: open store %q: %v", graphDir, err)
	}
	defer st.Close()

	idx, err := search.Open(indexPath)
	if err != nil {
		log.Fatalf("lex: open index %q: %v", indexPath, err)
	}
	defer idx.Close()

	// Fall back to building the index from the store if it wasn't built at
	// import time (e.g. an older dataset).
	if n, err := idx.Count(); err != nil {
		log.Fatalf("lex: index count: %v", err)
	} else if n == 0 {
		log.Printf("lex: empty index, building from store…")
		if err := mcp.BuildIndex(st, idx); err != nil {
			log.Fatalf("lex: build index: %v", err)
		}
	}

	// Keep one instance to one country so answers are never mixed.
	countries, err := mcp.Countries(st)
	if err != nil {
		log.Fatalf("lex: detect countries: %v", err)
	}
	switch {
	case len(countries) == 0:
		log.Printf("lex: warning: dataset %s is empty", *root)
	case len(countries) > 1 && !*allowMixed:
		log.Fatalf("lex: dataset %s mixes %d countries %v; serve one country per instance "+
			"(import each into its own dataset) or pass -allow-mixed", *root, len(countries), countries)
	case len(countries) > 1:
		log.Printf("lex: warning: serving MIXED countries %v — answers may span countries", countries)
	default:
		log.Printf("lex: serving country %q", countries[0])
	}

	srv := mcp.NewServer(mcp.NewService(st, idx))
	log.Printf("lex MCP server ready (data=%s)", *root)
	if err := srv.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		log.Fatalf("lex: serve: %v", err)
	}
}
