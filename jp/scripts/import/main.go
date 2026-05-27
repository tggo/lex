// Command import fetches Japan's legislation from the e-Gov 法令API v2 and
// loads it into a lex Badger triplestore. Thin shim over package importer
// (which is tested).
//
//	go run ./jp/scripts/import -out jp/data -limit 50 -articles
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"

	"github.com/tggo/lex/jp/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	root := flag.String("out", "jp/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "e-Gov 法令API v2 base URL")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch each act's full text and parse 条")
	flag.BoolVar(&cfg.WithRevisions, "revisions", false, "also fetch each act's full revision timeline")
	flag.IntVar(&cfg.Limit, "limit", 0, "stop after N acts (0 = all)")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "ja"

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
