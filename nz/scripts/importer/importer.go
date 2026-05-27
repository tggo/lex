// Package importer fetches New Zealand legislation from legislation.govt.nz
// (the Parliamentary Counsel Office XML export) and loads it into a lex Badger
// triplestore. Network access lives here; parsing/mapping is in package lenz
// and is tested offline. New Zealand Acts are not objects of copyright; the
// XML is published by the PCO under CC BY 4.0 (NZGOAL). See ADR-0025.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/nz/scripts/lenz"
)

// Defaults for the live legislation.govt.nz XML export.
const (
	DefaultBase = "https://www.legislation.govt.nz"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 2.0
	maxRetries        = 4
)

// Config controls an import run.
type Config struct {
	BaseURL      string       // legislation.govt.nz base, e.g. https://www.legislation.govt.nz
	ListURL      string       // legislation index (XML listing) URL
	OutDir       string       // Badger store directory
	IndexPath    string       // FTS index file; if empty, no index is built
	Lang         string       // search language for stemming (e.g. "en")
	UA           string       // HTTP User-Agent
	Client       *http.Client // defaults to http.DefaultClient if nil
	Now          time.Time    // retrieval timestamp recorded on each act
	FromYear     int          // inclusive lower bound (0 = no bound)
	ToYear       int          // inclusive upper bound (0 = no bound)
	WithArticles bool         // also parse section text from whole.xml
	RatePerSec   float64      // request rate limit; <=0 disables throttling
}

// Run fetches the listed acts and writes them to the store. It returns the
// number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if cfg.ListURL == "" {
		cfg.ListURL = cfg.BaseURL + "/legislation-index.xml"
	}

	c := &client{cfg: cfg, limiter: newLimiter(cfg.RatePerSec)}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	if cfg.IndexPath != "" {
		idx, err := search.OpenLang(cfg.IndexPath, cfg.Lang)
		if err != nil {
			return 0, err
		}
		defer idx.Close()
		c.idx = idx
	}

	lb, err := c.fetch(ctx, cfg.ListURL)
	if err != nil {
		return 0, err
	}
	list, err := lenz.ParseList(lb)
	if err != nil {
		return 0, err
	}

	total := 0
	for _, item := range list.Items {
		if cfg.FromYear != 0 && item.Year < cfg.FromYear {
			continue
		}
		if cfg.ToYear != 0 && item.Year > cfg.ToYear {
			continue
		}
		if err := c.importAct(ctx, st, item); err != nil {
			return total, fmt.Errorf("import %s/%d/%s: %w", item.Category, item.Year, item.Number, err)
		}
		total++
	}
	return total, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
	idx     *search.Index // optional full-text index; nil if disabled
}

// actURL builds the per-act whole.xml URL for a list item.
func (c *client) actURL(item lenz.ListItem) string {
	cat := item.Category
	if cat == "" {
		cat = "public"
	}
	return fmt.Sprintf("%s/act/%s/%d/%s/latest/whole.xml", c.cfg.BaseURL, cat, item.Year, item.Number)
}

// importAct fetches one act's whole.xml and stores it.
func (c *client) importAct(ctx context.Context, st *store.Store, item lenz.ListItem) error {
	b, err := c.fetch(ctx, c.actURL(item))
	if err != nil {
		return err
	}
	whole, err := lenz.ParseAct(b)
	if err != nil {
		return err
	}
	if !c.cfg.WithArticles {
		whole.Body = lenz.Body{} // drop section text when not requested
	}
	act, err := lenz.ToAct(item, whole, c.cfg.Now)
	if err != nil {
		return err
	}
	if err := st.AddAct(act); err != nil {
		return err
	}
	if c.idx != nil {
		if err := c.idx.ReplaceAct(act); err != nil {
			return fmt.Errorf("index act %s: %w", act.Number, err)
		}
	}
	return nil
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
		req.Header.Set("Accept", "application/xml")
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
