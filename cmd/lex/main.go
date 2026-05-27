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

	srv := mcp.NewServer(mcp.NewService(st, idx))
	log.Printf("lex MCP server ready (data=%s)", *root)
	if err := srv.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		log.Fatalf("lex: serve: %v", err)
	}
}
