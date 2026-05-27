// Command import fetches New Zealand's legislation from legislation.govt.nz
// (the PCO XML export) and loads it into a lex Badger triplestore. Thin shim
// over package importer (tested).
//
//	go run ./nz/scripts/import -out nz/data -articles
//	go run ./nz/scripts/import -out /tmp/nz -from 1990 -to 1990
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/nz/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "legislation.govt.nz base URL")
	flag.StringVar(&cfg.ListURL, "list", "", "legislation index XML URL (default <base>/legislation-index.xml)")
	root := flag.String("out", "nz/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.IntVar(&cfg.FromYear, "from", 0, "earliest year to import (0 = no bound)")
	flag.IntVar(&cfg.ToYear, "to", 0, "latest year to import (0 = no bound)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also parse section text")
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
