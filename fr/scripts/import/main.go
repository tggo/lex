// Command import loads France's legislation from the DILA LEGI open-data
// dataset into a lex Badger triplestore. Thin shim over package importer
// (tested).
//
// DILA publishes LEGI only as bulk gzip tarballs (a large global tarball plus
// small daily delta tarballs), never per-text URLs. This command downloads a
// tarball, walks it, parses the requested texts and writes them to the store.
//
//	# A daily delta by date (small — good for a smoke test):
//	go run ./fr/scripts/import -out /tmp/fr-test -delta 20240115
//
//	# An explicit tarball URL, importing all texts found in it:
//	go run ./fr/scripts/import -out /tmp/fr-test -dump https://echanges.dila.gouv.fr/OPENDATA/LEGI/LEGI_20240115-....tar.gz -articles
//
//	# A locally-downloaded tarball, filtered to specific CIDs:
//	go run ./fr/scripts/import -out fr/data -dump-file legi.tar.gz -cids LEGITEXT000006070721 -articles
//
// See fr/README.md and ADR-0016.
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
	var cids, delta string
	root := flag.String("out", "fr/data", "dataset root directory (holds graph/ and index.fts)")
	base := flag.String("base", importer.DefaultBase, "DILA OPENDATA/LEGI directory (used to build -delta URLs)")
	flag.StringVar(&delta, "delta", "", "daily delta tarball name or YYYYMMDD date under -base (e.g. 20240115 or LEGI_20240115-....tar.gz)")
	flag.StringVar(&cfg.DumpURL, "dump", "", "absolute URL of a .tar.gz to download")
	flag.StringVar(&cfg.DumpPath, "dump-file", "", "path to an already-downloaded .tar.gz (skips network)")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&cids, "cids", "", "comma-separated LEGITEXT… ids to import (default: all texts in the tarball)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also parse article text")
	flag.Parse()

	// Resolve -delta into a DumpURL if it was given.
	if delta != "" && cfg.DumpURL == "" && cfg.DumpPath == "" {
		name := delta
		if !strings.HasSuffix(name, ".tar.gz") {
			// A bare date: caller must still supply the full filename for the
			// exact tarball; we cannot guess the publish suffix. Treat the
			// value as a complete filename otherwise.
			name = "LEGI_" + delta + ".tar.gz"
		}
		cfg.DumpURL = strings.TrimRight(*base, "/") + "/" + name
	}

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
