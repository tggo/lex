// Package importer fetches the Verkhovna Rada open-data "primary acts" datasets
// and loads them into a lex Badger triplestore. Network access lives here; the
// parsing/mapping is in package ogd and is tested offline. Source data is
// CC BY 4.0 (attribution: data.rada.gov.ua / Verkhovna Rada). See ADR-0009.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/ua/scripts/ogd"
)

// Defaults for the live Verkhovna Rada open-data portal.
const (
	DefaultBase = "https://data.rada.gov.ua/ogd/zak"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
)

// Config controls an import run.
type Config struct {
	BaseURL string       // OGD base, e.g. https://data.rada.gov.ua/ogd/zak
	OutDir  string       // Badger store directory
	UA      string       // HTTP User-Agent
	Client  *http.Client // defaults to http.DefaultClient if nil
	Now     time.Time    // retrieval timestamp recorded on each act
}

// Run fetches the datasets, builds acts, and writes them to the store. It
// returns the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}

	cards, err := fetch(ctx, cfg, "/perv/cards.json")
	if err != nil {
		return 0, err
	}
	texts, err := fetch(ctx, cfg, "/perv/texts.json")
	if err != nil {
		return 0, err
	}
	perv1, err := fetch(ctx, cfg, "/laws/data/csv/perv1.txt")
	if err != nil {
		return 0, err
	}
	perv2, err := fetch(ctx, cfg, "/laws/data/csv/perv2.txt")
	if err != nil {
		return 0, err
	}
	perv0, err := fetch(ctx, cfg, "/laws/data/csv/perv0.txt")
	if err != nil {
		return 0, err
	}

	inForce := union(ogd.ParseIDList(perv1), ogd.ParseIDList(perv2))
	si := ogd.NewStatusIndex(inForce, ogd.ParseIDList(perv0))

	acts, err := ogd.BuildActs(cards, texts, si, cfg.Now)
	if err != nil {
		return 0, err
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	for _, a := range acts {
		if err := st.AddAct(a); err != nil {
			return 0, fmt.Errorf("add act %s: %w", a.Number, err)
		}
	}
	return len(acts), nil
}

// fetch GETs baseURL+path with the configured User-Agent.
func fetch(ctx context.Context, cfg Config, path string) ([]byte, error) {
	url := cfg.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", cfg.UA)
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func union(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}
