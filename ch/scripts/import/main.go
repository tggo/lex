// Command import fetches Switzerland's federal legislation from the Fedlex
// SPARQL endpoint and loads it into a lex Badger triplestore. Thin shim over
// package importer (tested).
//
//	go run ./ch/scripts/import -out ch/data/graph -sr 210,220,311.0
//	go run ./ch/scripts/import -out /tmp/ch -limit 500
package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"github.com/tggo/lex/ch/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	var srNotations string
	flag.StringVar(&cfg.Endpoint, "endpoint", importer.DefaultEndpoint, "Fedlex SPARQL endpoint URL")
	flag.StringVar(&cfg.OutDir, "out", "ch/data/graph", "Badger store directory")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&srNotations, "sr", "", "comma-separated SR notations to import (empty = all)")
	flag.IntVar(&cfg.Limit, "limit", 0, "SPARQL result limit (0 = no limit)")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	for _, sr := range strings.Split(srNotations, ",") {
		if sr = strings.TrimSpace(sr); sr != "" {
			cfg.SRNotations = append(cfg.SRNotations, sr)
		}
	}

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s", n, cfg.OutDir)
}
