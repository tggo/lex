// Command import fetches Spain's consolidated legislation from the BOE
// open-data API and loads it into a lex Badger triplestore. Thin shim over
// package importer (tested).
//
//	go run ./es/scripts/import -out es/data/graph -limit 50 -articles
//	go run ./es/scripts/import -out es/data/graph -articles
package main

import (
	"context"
	"flag"
	"log"

	"github.com/tggo/lex/es/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "BOE consolidated-legislation base URL")
	flag.StringVar(&cfg.OutDir, "out", "es/data/graph", "Badger store directory")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.IntVar(&cfg.Limit, "limit", 0, "max number of acts to import (0 = no bound)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch and parse article text")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s", n, cfg.OutDir)
}
