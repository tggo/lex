// Package importer fetches Austrian federal legislation from the RIS OGD API
// (data.bka.gv.at/ris/api) and loads it into a lex Badger triplestore. Network
// access lives here; parsing/mapping is in package ris and is tested offline.
// RIS open data is CC BY 4.0 (attribution: RIS / Bundeskanzleramt); the API is
// the official machine channel (no HTML crawling). See ADR-0023.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/tggo/lex/at/scripts/ris"
	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live RIS OGD API.
const (
	DefaultBase = "https://data.bka.gv.at/ris/api/v2.6/Bundesrecht"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	// application selects consolidated federal law within Bundesrecht.
	application = "BrKons"
	pageSize    = "OneHundred" // RIS enum; 100 documents per page
	pageSizeN   = 100
	maxRetries  = 4
)

// Config controls an import run.
type Config struct {
	BaseURL        string       // RIS Bundesrecht endpoint
	OutDir         string       // Badger store directory
	IndexPath      string       // FTS index file; if empty, no index is built
	Lang           string       // stemming language for the FTS index (e.g. "de")
	UA             string       // HTTP User-Agent
	Client         *http.Client // defaults to http.DefaultClient if nil
	Now            time.Time    // retrieval timestamp recorded on each act
	Gesetzesnummer []string     // law work ids to import (e.g. ["10001622"])
	WithArticles   bool         // also fetch each §'s content XML and parse article text
	RatePerSec     float64      // request rate limit; <=0 disables throttling
}

// Run fetches the configured laws (by Gesetzesnummer) and writes them to the
// store. It returns the number of acts (laws) written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}

	c := &client{cfg: cfg, limiter: newLimiter(cfg.RatePerSec)}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	// Optional full-text index, built incrementally alongside the store.
	var idx *search.Index
	if cfg.IndexPath != "" {
		idx, err = search.OpenLang(cfg.IndexPath, cfg.Lang)
		if err != nil {
			return 0, err
		}
		defer idx.Close()
	}

	total := 0
	for _, gn := range cfg.Gesetzesnummer {
		if err := c.importLaw(ctx, st, idx, gn); err != nil {
			return total, fmt.Errorf("import law %s: %w", gn, err)
		}
		total++
	}
	return total, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
}

// importLaw fetches all § documents of one law (by Gesetzesnummer), optionally
// their article text, and stores the assembled act.
func (c *client) importLaw(ctx context.Context, st *store.Store, idx *search.Index, gn string) error {
	docs, err := c.fetchLawDocs(ctx, gn)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("no documents for Gesetzesnummer %s", gn)
	}

	articles := map[string]schema.Article{}
	if c.cfg.WithArticles {
		for _, d := range docs {
			if d.IsHead() {
				continue
			}
			u := d.XMLURL()
			if u == "" {
				continue
			}
			xb, err := c.fetch(ctx, u)
			if err != nil {
				return err
			}
			art, ok, err := ris.ParseArticleText(xb)
			if err != nil {
				return err
			}
			if ok {
				articles[d.Data.Metadaten.Technisch.ID] = art
			}
		}
	}

	act, err := ris.ToAct(docs, articles, c.cfg.Now)
	if err != nil {
		return err
	}
	if err := st.AddAct(act); err != nil {
		return err
	}
	if idx != nil {
		if err := idx.ReplaceAct(act); err != nil {
			return fmt.Errorf("index act %s: %w", act.Number, err)
		}
	}
	return nil
}

// fetchLawDocs pages the search endpoint for every § document of one law.
func (c *client) fetchLawDocs(ctx context.Context, gn string) ([]ris.DocumentReference, error) {
	var all []ris.DocumentReference
	for page := 1; ; page++ {
		q := url.Values{}
		q.Set("Applikation", application)
		q.Set("Gesetzesnummer", gn)
		q.Set("DokumenteProSeite", pageSize)
		q.Set("Seitennummer", strconv.Itoa(page))
		b, err := c.fetch(ctx, c.cfg.BaseURL+"?"+q.Encode())
		if err != nil {
			return nil, err
		}
		_, docs, err := ris.ParseSearchResult(b)
		if err != nil {
			return nil, err
		}
		if len(docs) == 0 {
			break
		}
		all = append(all, docs...)
		if len(docs) < pageSizeN {
			break
		}
	}
	return all, nil
}

// fetch GETs url with the configured User-Agent, throttled and retried on
// transient errors (429 / 5xx).
func (c *client) fetch(ctx context.Context, url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.limiter.wait(ctx)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.cfg.UA)
		req.Header.Set("Accept", "application/json")
		resp, err := c.cfg.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetch %s: %w", url, err)
			if !sleepBackoff(ctx, attempt) {
				return nil, lastErr
			}
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK:
			return body, nil
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
			if !sleepBackoff(ctx, attempt) {
				return nil, lastErr
			}
		default:
			return nil, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
		}
	}
	return nil, lastErr
}

// sleepBackoff waits an exponential interval before the next retry. It returns
// false if the context is cancelled.
func sleepBackoff(ctx context.Context, attempt int) bool {
	d := time.Duration(1<<attempt) * 200 * time.Millisecond
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// limiter enforces a minimum interval between requests.
type limiter struct {
	interval time.Duration
	next     time.Time
}

func newLimiter(ratePerSec float64) *limiter {
	if ratePerSec <= 0 {
		return &limiter{}
	}
	return &limiter{interval: time.Duration(float64(time.Second) / ratePerSec)}
}

func (l *limiter) wait(ctx context.Context) {
	if l.interval == 0 {
		return
	}
	now := time.Now()
	if l.next.After(now) {
		t := time.NewTimer(l.next.Sub(now))
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
		}
	}
	l.next = time.Now().Add(l.interval)
}
