// Command import fetches Australia's legislation from the Federal Register of
// Legislation OData API and loads it into a lex Badger triplestore. Thin shim
// over package importer (tested).
//
//	go run ./au/scripts/import -out au/data -from 2024 -to 2024
//	go run ./au/scripts/import -out /tmp/au -collection Act -from 1901 -to 1901
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/au/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	root := flag.String("out", "au/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "FRL OData base URL")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&cfg.Collection, "collection", "Act", "FRL collection (Act, LegislativeInstrument, …)")
	flag.IntVar(&cfg.FromYear, "from", 0, "earliest year to import (required)")
	flag.IntVar(&cfg.ToYear, "to", 0, "latest year to import (required)")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "en"

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
