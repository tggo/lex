// Command import fetches Ukraine's open-data legislation and loads it into a
// lex Badger triplestore. Thin shim over package importer (which is tested).
//
//	go run ./ua/scripts/import -out ua/data/graph
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/ua/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	root := flag.String("out", "ua/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "OGD base URL (…/ogd/zak)")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch each act's HTML body and parse articles (one request per act)")
	flag.BoolVar(&cfg.WithRelations, "relations", false, "fetch the global doc index (~48MB) and resolve amend/repeal/cite edges")
	cache := flag.String("cache", "ua/.cache", "cache directory for fetched act bodies (\"\" to disable); survives dataset rebuilds")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.CacheDir = *cache

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
