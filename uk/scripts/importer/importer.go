// Package importer fetches UK legislation from legislation.gov.uk (The National
// Archives) and loads it into a lex Badger triplestore. Network access lives
// here; parsing/mapping is in package clml and is tested offline. The data is
// published under the Open Government Licence v3.0 (attribute legislation.gov.uk
// / The National Archives); the site exposes native ELI identifiers and a CLML
// XML channel, so no HTML crawling is needed. See ADR-0015.
package importer

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/uk/scripts/clml"
)

// Defaults for the live legislation.gov.uk channel.
const (
	DefaultBase = "https://www.legislation.gov.uk"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	maxRetries        = 4
	maxPages          = 1000 // safety cap on feed pagination
)

// Config controls an import run.
type Config struct {
	BaseURL    string       // site base, e.g. https://www.legislation.gov.uk
	OutDir     string       // Badger store directory
	IndexPath  string       // FTS index file; if empty, no index is built
	Lang       string       // search language for stemming (e.g. "en")
	UA         string       // HTTP User-Agent
	Client     *http.Client // defaults to http.DefaultClient if nil
	Now        time.Time    // retrieval timestamp recorded on each act
	Types      []string     // e.g. ["ukpga","uksi"]; defaults to ["ukpga"]
	FromYear   int          // inclusive lower bound (required: >0)
	ToYear     int          // inclusive upper bound (required: >0)
	RatePerSec float64      // request rate limit; <=0 disables throttling
}

// Run fetches the configured types/years and writes acts to the store. It
// returns the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if len(cfg.Types) == 0 {
		cfg.Types = []string{"ukpga"}
	}
	if cfg.FromYear == 0 {
		cfg.FromYear = time.Now().Year()
	}
	if cfg.ToYear == 0 {
		cfg.ToYear = cfg.FromYear
	}

	c := &client{cfg: cfg, limiter: newLimiter(cfg.RatePerSec)}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	// Optional full-text index, built incrementally alongside the store.
	if cfg.IndexPath != "" {
		idx, err := search.OpenLang(cfg.IndexPath, cfg.Lang)
		if err != nil {
			return 0, err
		}
		defer idx.Close()
		c.idx = idx
	}

	total := 0
	for _, typ := range cfg.Types {
		years := make([]int, 0)
		for y := cfg.FromYear; y <= cfg.ToYear; y++ {
			years = append(years, y)
		}
		sort.Ints(years)
		for _, y := range years {
			n, err := c.importYear(ctx, st, typ, y)
			if err != nil {
				return total, err
			}
			total += n
		}
	}
	return total, nil
}

// client bundles the run config with a rate limiter and optional FTS index.
type client struct {
	cfg     Config
	limiter *limiter
	idx     *search.Index // nil if no index is built
}

// importYear pages through one type/year feed and imports every listed act.
func (c *client) importYear(ctx context.Context, st *store.Store, typ string, year int) (int, error) {
	n := 0
	for page := 1; page <= maxPages; page++ {
		url := fmt.Sprintf("%s/%s/%d/data.feed?page=%d", c.cfg.BaseURL, typ, year, page)
		b, err := c.fetch(ctx, url)
		if err != nil {
			return n, err
		}
		feed, err := clml.ParseFeed(b)
		if err != nil {
			return n, err
		}
		if len(feed.Entries) == 0 {
			break
		}
		for _, e := range feed.Entries {
			path, ok := e.Path()
			if !ok {
				continue
			}
			stored, err := c.importAct(ctx, st, path)
			if err != nil {
				// Store/index failures are fatal: they indicate a broken sink,
				// not a quirk of one source page.
				return n, fmt.Errorf("import %s: %w", path, err)
			}
			if stored {
				n++
			}
		}
		if !feed.HasMore() {
			break
		}
	}
	return n, nil
}

// importAct fetches one act's CLML XML (…/data.xml) and stores it. It returns
// (true, nil) when the act was stored, (false, nil) when the act was skipped
// because its source resource is missing or unparseable (a non-fatal source
// quirk), and (false, err) only for fatal store/index failures.
//
// legislation.gov.uk lists some acts (notably older ones cited by regnal year,
// e.g. ukpga/Geo3/41) whose data.xml resource 404s or fails to parse. A missing
// or unparseable resource must not abort an otherwise-healthy run, so those are
// logged and skipped. Context cancellation stays fatal.
func (c *client) importAct(ctx context.Context, st *store.Store, path string) (bool, error) {
	b, err := c.fetch(ctx, c.cfg.BaseURL+"/"+path+"/data.xml")
	if err != nil {
		if ctx.Err() != nil {
			return false, err // cancellation/timeout is fatal
		}
		log.Printf("uk import: skipping %s: fetch: %v", path, err)
		return false, nil
	}
	l, err := clml.ParseLegislation(b)
	if err != nil {
		log.Printf("uk import: skipping %s: parse: %v", path, err)
		return false, nil
	}
	act, err := clml.ToAct(l, c.cfg.Now)
	if err != nil {
		log.Printf("uk import: skipping %s: map: %v", path, err)
		return false, nil
	}
	if err := st.AddAct(act); err != nil {
		return false, err
	}
	if c.idx != nil {
		if err := c.idx.ReplaceAct(act); err != nil {
			return false, fmt.Errorf("index act %s: %w", act.Number, err)
		}
	}
	return true, nil
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
