// Command import fetches Austria's federal legislation from the RIS OGD API and
// loads it into a lex Badger triplestore. Thin shim over package importer
// (tested). Laws are selected by Gesetzesnummer (RIS law work id).
//
//	go run ./at/scripts/import -out at/data -gn 10001622 -articles
//	go run ./at/scripts/import -out /tmp/at -gn 10007061,10001622
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"strings"

	"github.com/tggo/lex/at/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	var gns string
	root := flag.String("out", "at/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "RIS Bundesrecht endpoint")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&gns, "gn", "", "comma-separated Gesetzesnummer law ids to import")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch and parse article (§) text")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "de"

	for _, g := range strings.Split(gns, ",") {
		if g = strings.TrimSpace(g); g != "" {
			cfg.Gesetzesnummer = append(cfg.Gesetzesnummer, g)
		}
	}

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
