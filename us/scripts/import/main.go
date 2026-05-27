// Command import fetches United States Code titles from the Office of the Law
// Revision Counsel's USLM XML bulk channel and loads them into a lex Badger
// triplestore. Thin shim over package importer (tested).
//
//	go run ./us/scripts/import -out us/data
//	go run ./us/scripts/import -out /tmp/us -titles 1,5,26 -release 119-4
package main

import (
	"context"
	"flag"
	"log"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tggo/lex/us/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	var titles string
	root := flag.String("out", "us/data", "dataset root directory (holds graph/ and index.fts)")
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "OLRC release-point directory URL")
	flag.StringVar(&cfg.Release, "release", importer.DefaultRelease, "release tag in zip filenames, e.g. 119-4")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&titles, "titles", "", "comma-separated USC title numbers (empty = all 1..54)")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	cfg.OutDir = filepath.Join(*root, "graph")
	cfg.IndexPath = filepath.Join(*root, "index.fts")
	cfg.Lang = "en"

	for _, p := range strings.Split(titles, ",") {
		if p = strings.TrimSpace(p); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil {
				log.Fatalf("import: bad title number %q: %v", p, err)
			}
			cfg.Titles = append(cfg.Titles, n)
		}
	}

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d titles into %s (index %s)", n, cfg.OutDir, cfg.IndexPath)
}
