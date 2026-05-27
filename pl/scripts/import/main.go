// Command import fetches Poland's legislation from the Sejm ELI API and loads
// it into a lex Badger triplestore. Thin shim over package importer (tested).
//
//	go run ./pl/scripts/import -out pl/data/graph -publishers DU,MP -articles
//	go run ./pl/scripts/import -out /tmp/pl -publishers DU -from 1964 -to 1964
package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"github.com/tggo/lex/pl/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	var publishers string
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "Sejm ELI acts base URL")
	flag.StringVar(&cfg.OutDir, "out", "pl/data/graph", "Badger store directory")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&publishers, "publishers", "DU,MP", "comma-separated publishers (DU,MP)")
	flag.IntVar(&cfg.FromYear, "from", 0, "earliest year to import (0 = no bound)")
	flag.IntVar(&cfg.ToYear, "to", 0, "latest year to import (0 = no bound)")
	flag.BoolVar(&cfg.WithArticles, "articles", false, "also fetch and parse article text")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	for _, p := range strings.Split(publishers, ",") {
		if p = strings.TrimSpace(p); p != "" {
			cfg.Publishers = append(cfg.Publishers, p)
		}
	}

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s", n, cfg.OutDir)
}
