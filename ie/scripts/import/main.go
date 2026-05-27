// Command import fetches Ireland's legislation and loads it into a lex Badger
// triplestore. Acts are enumerated from the Houses of the Oireachtas open-data
// API and their text/metadata fetched from the electronic Irish Statute Book.
// Thin shim over package importer (tested).
//
//	go run ./ie/scripts/import -out ie/data -from 2015 -to 2015 -articles
//	go run ./ie/scripts/import -out /tmp/ie -from 2020 -to 2024
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/ie/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	flag.StringVar(&cfg.ListBase, "list-base", importer.DefaultListBase, "Oireachtas legislation API base URL")
	flag.StringVar(&cfg.EISBBase, "eisb-base", importer.DefaultEISBBase, "Irish Statute Book host")
	root := flag.String("out", "ie/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.IntVar(&cfg.FromYear, "from", 0, "earliest act year to import (required)")
	flag.IntVar(&cfg.ToYear, "to", 0, "latest act year to import (defaults to -from)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch and parse section text")
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
