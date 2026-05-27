// Package importer fetches the Verkhovna Rada open-data "primary acts" datasets
// and loads them into a lex Badger triplestore. Network access lives here; the
// parsing/mapping is in package ogd and is tested offline. Source data is
// CC BY 4.0 (attribution: data.rada.gov.ua / Verkhovna Rada). See ADR-0009.
package importer

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/text/encoding/charmap"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
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
	BaseURL       string       // OGD base, e.g. https://data.rada.gov.ua/ogd/zak
	OutDir        string       // Badger store directory
	IndexPath     string       // FTS index file; if empty, no index is built
	UA            string       // HTTP User-Agent
	Client        *http.Client // defaults to http.DefaultClient if nil
	Now           time.Time    // retrieval timestamp recorded on each act
	WithArticles  bool         // also fetch each act's HTML body and parse articles
	WithRelations bool         // fetch the global doc index and resolve amend/cite edges
	CacheDir      string       // if set, cache act HTML bodies here (keyed by file+version)

	// Resilience (matters when scraping many bodies, e.g. from CI).
	Retries      int           // attempts per request on transient failures (default 4)
	RetryBackoff time.Duration // base backoff, doubled each retry (default 500ms)
	RequestDelay time.Duration // pause before each request, to be polite (default 0)
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

	recs, err := ogd.BuildRecords(cards, texts, si, cfg.Now)
	if err != nil {
		return 0, err
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	// Optional full-text index, built incrementally alongside the store.
	var idx *search.Index
	if cfg.IndexPath != "" {
		idx, err = search.OpenLang(cfg.IndexPath, "uk") // Ukrainian stemming
		if err != nil {
			return 0, err
		}
		defer idx.Close()
	}

	// Optional relation resolution: fetch the global document index once and
	// turn each act's `links` into amend/repeal/cite edges.
	var docIdx map[int]ogd.DocRef
	if cfg.WithRelations {
		if docIdx, err = fetchDocIndex(ctx, cfg); err != nil {
			return 0, err
		}
	}

	for _, r := range recs {
		if cfg.WithArticles && r.TextFile != "" {
			tag := r.Act.Expression.VersionDate.Format("20060102")
			arts, err := fetchArticles(ctx, cfg, r.TextFile, tag)
			if err != nil {
				return 0, fmt.Errorf("articles for %s: %w", r.Act.Number, err)
			}
			r.Act.Expression.Articles = arts
		}
		if docIdx != nil {
			r.Act.Expression.Amends, r.Act.Expression.Repeals, r.Act.Expression.Cites =
				ogd.ResolveRelations(r.Links, docIdx)
		}
		if err := st.AddAct(r.Act); err != nil {
			return 0, fmt.Errorf("add act %s: %w", r.Act.Number, err)
		}
		if idx != nil {
			if err := idx.ReplaceAct(r.Act); err != nil {
				return 0, fmt.Errorf("index act %s: %w", r.Act.Number, err)
			}
		}
	}
	return len(recs), nil
}

// fetchArticles downloads (or reads from cache) an act's HTML body and parses
// its articles. versionTag (the act's redaction date) is part of the cache key
// so an amended act re-fetches rather than serving a stale body.
func fetchArticles(ctx context.Context, cfg Config, file, versionTag string) ([]schema.Article, error) {
	b, err := fetchBody(ctx, cfg, file, versionTag)
	if err != nil {
		return nil, err
	}
	return ogd.ParseArticles(b)
}

func fetchBody(ctx context.Context, cfg Config, file, versionTag string) ([]byte, error) {
	if cfg.CacheDir == "" {
		return fetch(ctx, cfg, "/perv/text/"+file)
	}
	cachePath := filepath.Join(cfg.CacheDir, file+"@"+versionTag)
	if b, err := os.ReadFile(cachePath); err == nil {
		return b, nil // cache hit — no network
	}
	b, err := fetch(ctx, cfg, "/perv/text/"+file)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err == nil {
		_ = os.WriteFile(cachePath, b, 0o644) // best-effort cache write
	}
	return b, nil
}

// fetchDocIndex downloads the global document-cards file (CP1251-encoded) and
// parses it into a dokid → DocRef map used for relation resolution.
func fetchDocIndex(ctx context.Context, cfg Config) (map[int]ogd.DocRef, error) {
	b, err := fetch(ctx, cfg, "/laws/data/csv/doc.txt")
	if err != nil {
		return nil, err
	}
	utf8, err := io.ReadAll(charmap.Windows1251.NewDecoder().Reader(bytes.NewReader(b)))
	if err != nil {
		return nil, fmt.Errorf("decode doc index: %w", err)
	}
	return ogd.ParseDocIndex(bytes.NewReader(utf8))
}

// fetch GETs baseURL+path with retries (exponential backoff) on transient
// failures — network errors, 5xx, and 429 — so a long scrape survives blips.
// 4xx (other than 429) fail fast.
func fetch(ctx context.Context, cfg Config, path string) ([]byte, error) {
	url := cfg.BaseURL + path
	attempts := cfg.Retries
	if attempts < 1 {
		attempts = 4
	}
	base := cfg.RetryBackoff
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	var lastErr error
	for a := 0; a < attempts; a++ {
		if a > 0 {
			select {
			case <-time.After(base << (a - 1)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		body, retryable, err := tryFetch(ctx, cfg, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("fetch %s: gave up after %d attempts: %w", url, attempts, lastErr)
}

func tryFetch(ctx context.Context, cfg Config, url string) (body []byte, retryable bool, err error) {
	if cfg.RequestDelay > 0 {
		select {
		case <-time.After(cfg.RequestDelay):
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", cfg.UA)
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("fetch %s: %w", url, err) // network → transient
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		b, err := io.ReadAll(resp.Body)
		return b, err != nil, err
	}
	retry := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
	return nil, retry, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
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
