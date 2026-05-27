// Command import fetches Luxembourg's legislation from the Legilux SPARQL
// endpoint and loads it into a lex Badger triplestore. Thin shim over package
// importer (tested).
//
//	go run ./lu/scripts/import -out lu/data/graph
//	go run ./lu/scripts/import -out /tmp/lu -limit 100
package main

import (
	"context"
	"flag"
	"log"

	"github.com/tggo/lex/lu/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	flag.StringVar(&cfg.Endpoint, "endpoint", importer.DefaultEndpoint, "Legilux SPARQL endpoint URL")
	flag.StringVar(&cfg.OutDir, "out", "lu/data/graph", "Badger store directory")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.IntVar(&cfg.Limit, "limit", 0, "max acts to import (0 = no bound)")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s", n, cfg.OutDir)
}
