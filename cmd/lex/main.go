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

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tggo/lex/internal/mcp"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

func main() {
	dataDir := flag.String("data", "ua/data/graph", "Badger triplestore directory")
	flag.Parse()

	// Logs go to stderr; stdout is the MCP protocol channel.
	st, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("lex: open store %q: %v", *dataDir, err)
	}
	defer st.Close()

	idx, err := search.OpenMemory()
	if err != nil {
		log.Fatalf("lex: open index: %v", err)
	}
	defer idx.Close()

	if err := mcp.BuildIndex(st, idx); err != nil {
		log.Fatalf("lex: build index: %v", err)
	}

	srv := mcp.NewServer(mcp.NewService(st, idx))
	log.Printf("lex MCP server ready (data=%s)", *dataDir)
	if err := srv.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		log.Fatalf("lex: serve: %v", err)
	}
}
