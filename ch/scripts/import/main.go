// Command import fetches Switzerland's federal legislation from the Fedlex
// SPARQL endpoint and loads it into a lex Badger triplestore. Thin shim over
// package importer (tested).
//
//	go run ./ch/scripts/import -out ch/data -sr 210,220,311.0
//	go run ./ch/scripts/import -out /tmp/ch -limit 500
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"strings"

	"github.com/tggo/lex/ch/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	var srNotations string
	root := flag.String("out", "ch/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.Endpoint, "endpoint", importer.DefaultEndpoint, "Fedlex SPARQL endpoint URL")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&srNotations, "sr", "", "comma-separated SR notations to import (empty = all)")
	flag.IntVar(&cfg.Limit, "limit", 0, "SPARQL result limit (0 = no limit)")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch each act's full text and parse article-level text")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "de"

	for _, sr := range strings.Split(srNotations, ",") {
		if sr = strings.TrimSpace(sr); sr != "" {
			cfg.SRNotations = append(cfg.SRNotations, sr)
		}
	}

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
