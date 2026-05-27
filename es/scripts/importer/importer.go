// Package importer fetches Spanish legislation from the BOE "Legislación
// Consolidada" open-data API (www.boe.es/datosabiertos) and loads it into a lex
// Badger triplestore. Network access lives here; parsing/mapping is in package
// boe and is tested offline. Spanish normative acts are not objects of
// copyright and BOE open-data reuse is permitted with attribution to the
// Agencia Estatal BOE — the API is the official machine channel (no HTML
// crawling). See ADR-0021.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tggo/lex/es/scripts/boe"
	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live BOE open-data API.
const (
	DefaultBase = "https://www.boe.es/datosabiertos/api/legislacion-consolidada"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	pageLimit         = 100
	maxRetries        = 4
)

// Config controls an import run.
type Config struct {
	BaseURL      string       // listing base, e.g. the DefaultBase
	OutDir       string       // Badger store directory
	UA           string       // HTTP User-Agent
	Client       *http.Client // defaults to http.DefaultClient if nil
	Now          time.Time    // retrieval timestamp recorded on each act
	Limit        int          // max acts to import (0 = no bound)
	WithArticles bool         // also fetch each norm's texto and parse articles
	RatePerSec   float64      // request rate limit; <=0 disables throttling
}

// Run fetches the consolidated-legislation list and writes acts to the store.
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

	total := 0
	for offset := 0; ; {
		url := fmt.Sprintf("%s?limit=%d&offset=%d", c.cfg.BaseURL, pageLimit, offset)
		b, err := c.fetch(ctx, url, "application/json")
		if err != nil {
			return total, err
		}
		items, err := boe.ParseList(b)
		if err != nil {
			return total, err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if err := c.importNorm(ctx, st, item.Identificador); err != nil {
				return total, fmt.Errorf("import %s: %w", item.Identificador, err)
			}
			total++
			if cfg.Limit > 0 && total >= cfg.Limit {
				return total, nil
			}
		}
		offset += len(items)
		if len(items) < pageLimit {
			break
		}
	}
	return total, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
}

// importNorm fetches one norm's metadatos (+analisis, +texto) and stores it.
func (c *client) importNorm(ctx context.Context, st *store.Store, id string) error {
	base := c.cfg.BaseURL + "/id/" + id

	mb, err := c.fetch(ctx, base+"/metadatos", "application/json")
	if err != nil {
		return err
	}
	meta, err := boe.ParseMetadatos(mb)
	if err != nil {
		return err
	}

	ab, err := c.fetch(ctx, base+"/analisis", "application/json")
	if err != nil {
		return err
	}
	an, err := boe.ParseAnalisis(ab)
	if err != nil {
		return err
	}

	var arts []schema.Article
	if c.cfg.WithArticles {
		tb, err := c.fetch(ctx, base+"/texto", "application/xml")
		if err != nil {
			return err
		}
		if arts, err = boe.ParseArticles(tb); err != nil {
			return err
		}
	}

	act, err := boe.ToAct(meta, an, arts, c.cfg.Now)
	if err != nil {
		return err
	}
	return st.AddAct(act)
}

// fetch GETs url with the configured User-Agent and the given Accept type,
// throttled and retried on transient errors (429 / 5xx).
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
