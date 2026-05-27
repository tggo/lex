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
	"os"
	"path/filepath"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tggo/lex/internal/mcp"
	"github.com/tggo/lex/internal/release"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

const repo = "tggo/lex"

func main() {
	root := flag.String("data", "ua/data", "dataset root directory (holds graph/ and index.fts)")
	allowMixed := flag.Bool("allow-mixed", false, "allow a dataset containing more than one country (answers may span countries)")
	country := flag.String("country", "", "country code for the prebuilt dataset (default: inferred from -data)")
	releaseURL := flag.String("release-url", "", "override the dataset download URL")
	noPull := flag.Bool("no-pull", false, "do not download a prebuilt dataset from Releases when missing")
	flag.Parse()

	graphDir := filepath.Join(*root, "graph")
	indexPath := filepath.Join(*root, "index.fts")

	// If there is no local dataset, fetch a prebuilt one from Releases so users
	// don't have to re-scrape official sources.
	if _, err := os.Stat(graphDir); os.IsNotExist(err) {
		if *noPull {
			log.Fatalf("lex: no dataset at %s and -no-pull set; build one with the country importer", *root)
		}
		cc := *country
		if cc == "" {
			cc = filepath.Base(filepath.Dir(filepath.Clean(*root))) // ua/data -> ua
		}
		url := *releaseURL
		if url == "" {
			url = release.AssetURL(repo, cc)
		}
		log.Printf("lex: no local dataset, downloading %s …", url)
		if err := release.Download(context.Background(), nil, url, *root); err != nil {
			log.Fatalf("lex: %v\n(build locally with the %q importer, or check %s/releases)", err, cc, repo)
		}
		log.Printf("lex: dataset ready at %s", *root)
	}

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
	log.Printf("lex MCP server ready (data=%s, search lang=%q)", *root, idx.Lang())
	if err := srv.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		log.Fatalf("lex: serve: %v", err)
	}
}
