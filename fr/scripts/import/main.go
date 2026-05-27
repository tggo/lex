// Command import fetches France's legislation from the DILA LEGI open-data
// dataset and loads it into a lex Badger triplestore. Thin shim over package
// importer (tested).
//
//	go run ./fr/scripts/import -out fr/data -cids LEGITEXT000006070721 -articles
//
// LEGI is a bulk XML corpus; -base must point at an HTTP-served extraction of
// the dataset's sharded LEGI/ tree (or the DILA open-data root). See
// fr/README.md and ADR-0016.
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"strings"

	"github.com/tggo/lex/fr/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	var cids string
	root := flag.String("out", "fr/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "root of the served LEGI XML tree")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&cids, "cids", "", "comma-separated LEGITEXT… ids to import")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch and parse article text")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "fr"

	for _, c := range strings.Split(cids, ",") {
		if c = strings.TrimSpace(c); c != "" {
			cfg.TextCIDs = append(cfg.TextCIDs, c)
		}
	}

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
