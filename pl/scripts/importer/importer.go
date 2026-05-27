// Package importer fetches Polish legislation from the Sejm ELI API
// (api.sejm.gov.pl/eli) and loads it into a lex Badger triplestore. Network
// access lives here; parsing/mapping is in package eli and is tested offline.
// Polish normative acts are not objects of copyright; the API is the official
// machine channel (no HTML crawling). See ADR-0012.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/pl/scripts/eli"
)

// Defaults for the live Sejm ELI API.
const (
	DefaultBase = "https://api.sejm.gov.pl/eli/acts"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	pageLimit         = 500
	maxRetries        = 4
)

// Config controls an import run.
type Config struct {
	BaseURL      string       // ELI acts base, e.g. https://api.sejm.gov.pl/eli/acts
	OutDir       string       // Badger store directory
	IndexPath    string       // FTS index file; if empty, no index is built
	Lang         string       // search language for stemming (e.g. "pl")
	UA           string       // HTTP User-Agent
	Client       *http.Client // defaults to http.DefaultClient if nil
	Now          time.Time    // retrieval timestamp recorded on each act
	Publishers   []string     // e.g. ["DU","MP"]; defaults to both
	FromYear     int          // inclusive lower bound (0 = no bound)
	ToYear       int          // inclusive upper bound (0 = no bound)
	WithArticles bool         // also fetch each act's struct + text.html and parse articles
	RatePerSec   float64      // request rate limit; <=0 disables throttling
}

// Run fetches the configured publishers/years and writes acts to the store.
// It returns the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if len(cfg.Publishers) == 0 {
		cfg.Publishers = []string{"DU", "MP"}
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
	for _, pub := range cfg.Publishers {
		years, err := c.years(ctx, pub)
		if err != nil {
			return total, err
		}
		for _, y := range years {
			n, err := c.importYear(ctx, st, pub, y)
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

// years returns the publisher's available years filtered by the configured
// From/To bounds, ascending.
func (c *client) years(ctx context.Context, pub string) ([]int, error) {
	b, err := c.fetch(ctx, c.cfg.BaseURL+"/"+pub)
	if err != nil {
		return nil, err
	}
	info, err := eli.ParsePublisherInfo(b)
	if err != nil {
		return nil, err
	}
	var out []int
	for _, y := range info.Years {
		if c.cfg.FromYear != 0 && y < c.cfg.FromYear {
			continue
		}
		if c.cfg.ToYear != 0 && y > c.cfg.ToYear {
			continue
		}
		out = append(out, y)
	}
	sort.Ints(out)
	return out, nil
}

// importYear pages through one publisher/year and imports every act.
func (c *client) importYear(ctx context.Context, st *store.Store, pub string, year int) (int, error) {
	n := 0
	for offset := 0; ; {
		url := fmt.Sprintf("%s/%s/%d?limit=%d&offset=%d", c.cfg.BaseURL, pub, year, pageLimit, offset)
		b, err := c.fetch(ctx, url)
		if err != nil {
			return n, err
		}
		list, err := eli.ParseActList(b)
		if err != nil {
			return n, err
		}
		if len(list.Items) == 0 {
			break
		}
		for _, item := range list.Items {
			if err := c.importAct(ctx, st, item); err != nil {
				return n, fmt.Errorf("import %s: %w", item.ELI, err)
			}
			n++
		}
		offset += len(list.Items)
		if list.TotalCount > 0 && offset >= list.TotalCount {
			break
		}
	}
	return n, nil
}

// importAct fetches one act's detail (+references, +articles) and stores it.
func (c *client) importAct(ctx context.Context, st *store.Store, item eli.ListItem) error {
	base := c.cfg.BaseURL + "/" + item.ELI

	db, err := c.fetch(ctx, base)
	if err != nil {
		return err
	}
	detail, err := eli.ParseDetail(db)
	if err != nil {
		return err
	}

	rb, err := c.fetch(ctx, base+"/references")
	if err != nil {
		return err
	}
	refs, err := eli.ParseReferences(rb)
	if err != nil {
		return err
	}

	var arts []schema.Article
	if c.cfg.WithArticles && detail.TextHTML {
		sb, err := c.fetch(ctx, base+"/struct")
		if err != nil {
			return err
		}
		tb, err := c.fetch(ctx, base+"/text.html")
		if err != nil {
			return err
		}
		if arts, err = eli.ParseArticles(sb, tb); err != nil {
			return err
		}
	}

	act, err := eli.ToAct(detail, refs, arts, c.cfg.Now)
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
