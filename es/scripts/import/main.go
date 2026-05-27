// Command import fetches Spain's consolidated legislation from the BOE
// open-data API and loads it into a lex Badger triplestore. Thin shim over
// package importer (tested).
//
//	go run ./es/scripts/import -out es/data -limit 50 -articles
//	go run ./es/scripts/import -out es/data -articles
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/es/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	root := flag.String("out", "es/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "BOE consolidated-legislation base URL")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.IntVar(&cfg.Limit, "limit", 0, "max number of acts to import (0 = no bound)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch and parse article text")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "es"

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
