// Command import fetches Finland's legislation from the Finlex open-data API
// and loads it into a lex Badger triplestore. Thin shim over package importer
// (tested).
//
//	go run ./fi/scripts/import -out fi/data -articles
//	go run ./fi/scripts/import -out /tmp/fi -limit 50 -articles
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/fi/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	root := flag.String("out", "fi/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "Finlex open-data API base URL")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.IntVar(&cfg.Limit, "limit", 0, "max statutes to import (0 = all available)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch and parse article (§) text")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "fi"

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
