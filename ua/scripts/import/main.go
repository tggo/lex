// Command import fetches Ukraine's open-data legislation and loads it into a
// lex Badger triplestore. Thin shim over package importer (which is tested).
//
//	go run ./ua/scripts/import -out ua/data/graph
package main

import (
	"context"
	"flag"
	"log"

	"github.com/tggo/lex/ua/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "OGD base URL (…/ogd/zak)")
	flag.StringVar(&cfg.OutDir, "out", "ua/data/graph", "Badger store directory")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.Parse()

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s", n, cfg.OutDir)
}
