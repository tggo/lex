// Command import fetches Japan's legislation from the e-Gov 法令API v2 and
// loads it into a lex Badger triplestore. Thin shim over package importer
// (which is tested).
//
//	go run ./jp/scripts/import -out jp/data/graph -limit 50 -articles
package main

import (
	"context"
	"flag"
	"log"

	"github.com/tggo/lex/jp/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "e-Gov 法令API v2 base URL")
	flag.StringVar(&cfg.OutDir, "out", "jp/data/graph", "Badger store directory")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch each act's full text and parse 条")
	flag.IntVar(&cfg.Limit, "limit", 0, "stop after N acts (0 = all)")
	flag.Parse()

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s", n, cfg.OutDir)
}
