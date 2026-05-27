// Package importer fetches Irish legislation and loads it into a lex Badger
// triplestore. Network access lives here; parsing/mapping is in package eisb and
// is tested offline. Acts are enumerated from the Houses of the Oireachtas
// open-data API (api.oireachtas.ie/v1/legislation); each act's metadata, native
// ELI facts, and section text come from its electronic Irish Statute Book
// (irishstatutebook.ie) print page. Re-use is governed by the Oireachtas (Open
// Data) PSI Licence, incorporating CC BY 4.0. See ADR-0022.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tggo/lex/ie/scripts/eisb"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live Irish sources.
const (
	// DefaultListBase is the Oireachtas open-data legislation endpoint.
	DefaultListBase = "https://api.oireachtas.ie/v1/legislation"
	// DefaultEISBBase is the electronic Irish Statute Book host.
	DefaultEISBBase = "http://www.irishstatutebook.ie"
	DefaultUA       = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	pageLimit         = 500
	maxRetries        = 4
)

// Config controls an import run.
type Config struct {
	ListBase     string       // Oireachtas legislation API base
	EISBBase     string       // Irish Statute Book host (overrides eisb default host for tests)
	OutDir       string       // Badger store directory
	UA           string       // HTTP User-Agent
	Client       *http.Client // defaults to http.DefaultClient if nil
	Now          time.Time    // retrieval timestamp recorded on each act
	FromYear     int          // inclusive lower bound (required: at least one of From/To)
	ToYear       int          // inclusive upper bound
	WithArticles bool         // also fetch each act's print page and parse section text
	RatePerSec   float64      // request rate limit; <=0 disables throttling
}

// Run fetches the configured year range and writes acts to the store. It
// returns the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if cfg.ListBase == "" {
		cfg.ListBase = DefaultListBase
	}
	if cfg.EISBBase == "" {
		cfg.EISBBase = DefaultEISBBase
	}
	from, to := cfg.FromYear, cfg.ToYear
	if from == 0 {
		from = to
	}
	if to == 0 {
		to = from
	}
	if from == 0 || to == 0 {
		return 0, fmt.Errorf("importer: a year range (-from/-to) is required")
	}

	c := &client{cfg: cfg, limiter: newLimiter(cfg.RatePerSec)}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	total := 0
	for y := from; y <= to; y++ {
		n, err := c.importYear(ctx, st, y)
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

// importYear enumerates and imports every act of a year.
func (c *client) importYear(ctx context.Context, st *store.Store, year int) (int, error) {
	n := 0
	for skip := 0; ; {
		url := fmt.Sprintf("%s?act_year=%d&lang=en&limit=%d&skip=%d", c.cfg.ListBase, year, pageLimit, skip)
		b, err := c.fetch(ctx, url, "application/json")
		if err != nil {
			return n, err
		}
		list, err := eisb.ParseActList(b)
		if err != nil {
			return n, err
		}
		if len(list.Items) == 0 {
			break
		}
		for _, item := range list.Items {
			if err := c.importAct(ctx, st, item); err != nil {
				return n, fmt.Errorf("import %d/act/%s: %w", item.Year, item.Number, err)
			}
			n++
		}
		skip += len(list.Items)
		if list.ResultCount > 0 && skip >= list.ResultCount {
			break
		}
	}
	return n, nil
}

// importAct fetches one act's eISB print page and stores it.
func (c *client) importAct(ctx context.Context, st *store.Store, item eisb.ListItem) error {
	id := item.StatuteBookID
	if id == "" {
		return fmt.Errorf("item has no statutebook id")
	}
	url := c.cfg.EISBBase + eisb.PrintPath(id)

	b, err := c.fetch(ctx, url, "text/html")
	if err != nil {
		return err
	}
	act, err := eisb.ParseAct(b, c.cfg.Now)
	if err != nil {
		return err
	}
	if !c.cfg.WithArticles {
		act.Expression.Articles = nil
	}
	return st.AddAct(act)
}

// fetch GETs url with the configured User-Agent, throttled and retried on
// transient errors (429 / 5xx).
func (c *client) fetch(ctx context.Context, url, accept string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.limiter.wait(ctx)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.cfg.UA)
		req.Header.Set("Accept", accept)
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
