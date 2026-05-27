// Package importer fetches Australian legislation from the Federal Register of
// Legislation OData API (api.prod.legislation.gov.au) and loads it into a lex
// Badger triplestore. Network access lives here; parsing/mapping is in package
// frl and is tested offline. Commonwealth legislative material is published
// under CC BY 4.0 (attribution to the Federal Register of Legislation); the
// OData API is the official machine channel. See ADR-0024.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/tggo/lex/au/scripts/frl"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live FRL OData API.
const (
	DefaultBase = "https://api.prod.legislation.gov.au/v1"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	pageLimit         = 200
	maxRetries        = 4
)

// Config controls an import run.
type Config struct {
	BaseURL    string       // OData base, e.g. https://api.prod.legislation.gov.au/v1
	OutDir     string       // Badger store directory
	IndexPath  string       // FTS index file; if empty, no index is built
	Lang       string       // stemming language for the FTS index (e.g. "en")
	UA         string       // HTTP User-Agent
	Client     *http.Client // defaults to http.DefaultClient if nil
	Now        time.Time    // retrieval timestamp recorded on each act
	Collection string       // FRL collection filter, e.g. "Act" (default "Act")
	FromYear   int          // inclusive lower bound (0 = no bound)
	ToYear     int          // inclusive upper bound (0 = no bound)
	RatePerSec float64      // request rate limit; <=0 disables throttling
}

// Run fetches the configured years and writes acts to the store. It returns the
// number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if cfg.Collection == "" {
		cfg.Collection = "Act"
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
	for y := cfg.FromYear; y <= cfg.ToYear; y++ {
		if cfg.FromYear == 0 || cfg.ToYear == 0 {
			return total, fmt.Errorf("importer: -from and -to years are required")
		}
		n, err := c.importYear(ctx, st, idx, y)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
}

// importYear pages through one year's titles and imports every act.
func (c *client) importYear(ctx context.Context, st *store.Store, idx *search.Index, year int) (int, error) {
	n := 0
	for skip := 0; ; {
		filter := fmt.Sprintf("year eq %d and collection eq '%s'", year, c.cfg.Collection)
		q := url.Values{}
		q.Set("$filter", filter)
		q.Set("$top", fmt.Sprintf("%d", pageLimit))
		q.Set("$skip", fmt.Sprintf("%d", skip))
		q.Set("$count", "true")
		u := c.cfg.BaseURL + "/titles?" + q.Encode()

		b, err := c.fetch(ctx, u)
		if err != nil {
			return n, err
		}
		list, err := frl.ParseTitleList(b)
		if err != nil {
			return n, err
		}
		if len(list.Value) == 0 {
			break
		}
		for _, item := range list.Value {
			if err := c.importTitle(ctx, st, idx, item.ID); err != nil {
				return n, fmt.Errorf("import %s: %w", item.ID, err)
			}
			n++
		}
		skip += len(list.Value)
		if list.Count > 0 && skip >= list.Count {
			break
		}
	}
	return n, nil
}

// importTitle fetches one title's detail + current version and stores it.
func (c *client) importTitle(ctx context.Context, st *store.Store, idx *search.Index, id string) error {
	db, err := c.fetch(ctx, c.cfg.BaseURL+"/titles/"+url.PathEscape(id))
	if err != nil {
		return err
	}
	detail, err := frl.ParseDetail(db)
	if err != nil {
		return err
	}

	// The current point-in-time compilation: version_date, status, and the
	// amend/repeal edges that affected this title.
	vURL := fmt.Sprintf("%s/Versions/Default.Find(titleId='%s',asAtSpecification='current')",
		c.cfg.BaseURL, id)
	var version *frl.Version
	vb, err := c.fetch(ctx, vURL)
	if err == nil {
		if version, err = frl.ParseVersion(vb); err != nil {
			return err
		}
	} else if err != errNotFound {
		return err
	}

	act, err := frl.ToAct(detail, version, c.cfg.Now)
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

// errNotFound marks a 404 so a missing current version is tolerated (some
// titles have no compilation; metadata still imports).
var errNotFound = fmt.Errorf("not found")

// fetch GETs url with the configured User-Agent, throttled and retried on
// transient errors (429 / 5xx). A 404 returns errNotFound.
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
		case resp.StatusCode == http.StatusNotFound:
			return nil, errNotFound
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
