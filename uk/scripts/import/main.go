// Command import fetches the UK's legislation from legislation.gov.uk and loads
// it into a lex Badger triplestore. Thin shim over package importer (tested).
//
//	go run ./uk/scripts/import -out uk/data/graph -types ukpga -from 2023 -to 2023
//	go run ./uk/scripts/import -out /tmp/uk -types ukpga,uksi -from 2000 -to 2024
package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"github.com/tggo/lex/uk/scripts/importer"
)

func main() {
	cfg := importer.Config{}
	var types string
	flag.StringVar(&cfg.BaseURL, "base", importer.DefaultBase, "legislation.gov.uk base URL")
	flag.StringVar(&cfg.OutDir, "out", "uk/data/graph", "Badger store directory")
	flag.StringVar(&cfg.UA, "ua", importer.DefaultUA, "HTTP User-Agent")
	flag.StringVar(&types, "types", "ukpga", "comma-separated legislation types (ukpga,uksi,asp,…)")
	flag.IntVar(&cfg.FromYear, "from", 0, "earliest year to import (0 = current year)")
	flag.IntVar(&cfg.ToYear, "to", 0, "latest year to import (0 = same as -from)")
	flag.Float64Var(&cfg.RatePerSec, "rps", importer.DefaultRatePerSec, "request rate limit per second")
	flag.Parse()

	for _, t := range strings.Split(types, ",") {
		if t = strings.TrimSpace(t); t != "" {
			cfg.Types = append(cfg.Types, t)
		}
	}

	n, err := importer.Run(context.Background(), cfg)
	if err != nil {
		log.Fatalf("import: %v", err)
	}
	log.Printf("imported %d acts into %s", n, cfg.OutDir)
}
