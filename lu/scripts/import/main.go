// Command import fetches Luxembourg's legislation from the Legilux SPARQL
// endpoint and loads it into a lex Badger triplestore. Thin shim over package
// importer (tested).
//
//	go run ./lu/scripts/import -out lu/data
//	go run ./lu/scripts/import -out /tmp/lu -limit 100
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/lu/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	flag.StringVar(&cfg.Endpoint, "endpoint", importer.DefaultEndpoint, "Legilux SPARQL endpoint URL")
	root := flag.String("out", "lu/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.IntVar(&cfg.Limit, "limit", 0, "max acts to import (0 = no bound)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch each act's French HTML manifestation and parse article text")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "fr"

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
