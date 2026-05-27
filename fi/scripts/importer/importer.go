// Package importer fetches Finnish legislation from the Finlex open-data REST
// API (opendata.finlex.fi) and loads it into a lex Badger triplestore. Network
// access lives here; parsing/mapping is in package akn and is tested offline.
// Finnish normative acts are not objects of copyright and the open dataset is
// CC BY 4.0 (attribution: Finlex / Ministry of Justice, Edita Publishing); the
// API is the official machine channel (no HTML crawling). See ADR-0019.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tggo/lex/fi/scripts/akn"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live Finlex open-data API.
const (
	DefaultBase = "https://opendata.finlex.fi/finlex/avoindata/v1"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	// collection is the AKN collection path for consolidated ("ajantasa") statutes.
	collection = "akn/fi/act/statute-consolidated"
	// pageLimit is the per-request page size. The Finlex open-data API caps
	// "limit" at 10 (a higher value yields HTTP 400), so we page in tens.
	pageLimit  = 10
	maxRetries = 4
)

// Config controls an import run.
type Config struct {
	BaseURL      string       // API base, e.g. https://opendata.finlex.fi/finlex/avoindata/v1
	OutDir       string       // Badger store directory
	IndexPath    string       // FTS index file; if empty, no index is built
	Lang         string       // search stemming language
	UA           string       // HTTP User-Agent
	Client       *http.Client // defaults to http.DefaultClient if nil
	Now          time.Time    // retrieval timestamp recorded on each act
	Limit        int          // total number of statutes to import (0 = all available)
	WithArticles bool         // parse each act's body sections (§) into lex:Article
	RatePerSec   float64      // request rate limit; <=0 disables throttling
}

// Run pages the consolidated-statute collection and writes acts to the store.
// It returns the number of acts written.
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
	if cfg.IndexPath != "" {
		idx, err := search.OpenLang(cfg.IndexPath, cfg.Lang)
		if err != nil {
			return 0, err
		}
		defer idx.Close()
		c.idx = idx
	}

	total := 0
	for offset := 0; ; {
		want := pageLimit
		if cfg.Limit > 0 && cfg.Limit-total < want {
			want = cfg.Limit - total
		}
		url := fmt.Sprintf("%s/%s?limit=%d&offset=%d", cfg.BaseURL, collection, want, offset)
		b, err := c.fetch(ctx, url)
		if err != nil {
			return total, err
		}
		docs, err := akn.ParseList(b)
		if err != nil {
			return total, err
		}
		if len(docs) == 0 {
			break
		}
		for _, d := range docs {
			n, err := c.store(st, d)
			if err != nil {
				return total, err
			}
			total += n
			if cfg.Limit > 0 && total >= cfg.Limit {
				return total, nil
			}
		}
		offset += len(docs)
		if len(docs) < want {
			break
		}
	}
	return total, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
	idx     *search.Index
}

// store maps one parsed document into a schema.Act and writes it. The list
// envelope carries only metadata (no body), so when articles are requested the
// full expression document is fetched separately. Returns 1 on a successful
// write, 0 when the document is skipped (e.g. no version date).
func (c *client) store(st *store.Store, d *akn.Document) (int, error) {
	doc := d
	if c.cfg.WithArticles {
		full, err := c.fetchFull(context.Background(), d)
		if err != nil {
			return 0, err
		}
		doc = full
	}
	act, err := doc.ToAct(c.cfg.Now, c.cfg.WithArticles)
	if err != nil {
		// A missing version date is a contract violation: drop the record
		// rather than guess (see docs/ontology.md invariant 1).
		return 0, nil
	}
	if err := st.AddAct(act); err != nil {
		return 0, err
	}
	if c.idx != nil {
		if err := c.idx.ReplaceAct(act); err != nil {
			return 0, fmt.Errorf("index act %s: %w", act.Number, err)
		}
	}
	return 1, nil
}

// fetchFull retrieves the full Finnish expression document for d, which
// includes the body sections absent from the list envelope.
func (c *client) fetchFull(ctx context.Context, d *akn.Document) (*akn.Document, error) {
	year, num, err := d.Identity()
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/%s/%d/%s/fin@", c.cfg.BaseURL, collection, year, num)
	b, err := c.fetch(ctx, url)
	if err != nil {
		return nil, err
	}
	return akn.Parse(b)
}

// fetch GETs url with the configured User-Agent, throttled and retried on
// transient errors (429 / 5xx). The Finlex API serves AKN as XML.
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
