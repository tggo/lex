// Package importer fetches Luxembourg legislation from the Legilux SPARQL
// endpoint (data.legilux.public.lu) and loads it into a lex Badger triplestore.
// Network access lives here; parsing/mapping is in package legilux and is tested
// offline. Luxembourg normative acts are open data (CC BY) attributed to
// Legilux / État du Grand-Duché de Luxembourg; the SPARQL endpoint is the
// official, no-auth machine channel (no HTML crawling). See ADR-0018.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/lu/scripts/legilux"
)

// Defaults for the live Legilux SPARQL endpoint.
const (
	DefaultEndpoint = "https://data.legilux.public.lu/sparqlendpoint"
	DefaultUA       = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 2.0
	pageLimit         = 500
	maxRetries        = 4
)

// Config controls an import run.
type Config struct {
	Endpoint   string       // SPARQL endpoint, e.g. https://data.legilux.public.lu/sparqlendpoint
	OutDir     string       // Badger store directory
	IndexPath  string       // FTS index file; if empty, no index is built
	Lang       string       // search language for stemming (e.g. "fr")
	UA         string       // HTTP User-Agent
	Client     *http.Client // defaults to http.DefaultClient if nil
	Now        time.Time    // retrieval timestamp recorded on each act
	Limit      int          // max acts to import (0 = no bound)
	RatePerSec float64      // request rate limit; <=0 disables throttling
}

// Run pages the acts query and writes acts to the store. It returns the number
// of acts written.
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
		page := pageLimit
		if cfg.Limit > 0 && cfg.Limit-total < page {
			page = cfg.Limit - total
		}
		rows, err := c.actsPage(ctx, offset, page)
		if err != nil {
			return total, err
		}
		if len(rows) == 0 {
			break
		}
		if cfg.Limit > 0 && len(rows) > cfg.Limit-total {
			rows = rows[:cfg.Limit-total] // honour limit even if the endpoint over-returns
		}
		for _, r := range rows {
			if err := c.importAct(ctx, st, r); err != nil {
				return total, fmt.Errorf("import %s: %w", r.WorkURI, err)
			}
			total++
		}
		offset += len(rows)
		if cfg.Limit > 0 && total >= cfg.Limit {
			break
		}
	}
	return total, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
	idx     *search.Index // optional full-text index; nil if disabled
}

// actsQuery is the paged acts metadata query: every JOLux Act, its French
// expression's title, type, and dates.
const actsQuery = `PREFIX jolux: <` + legilux.NSjolux + `>
PREFIX rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#>
SELECT ?work ?type ?title ?lang ?dateDoc ?entry ?noLonger ?status WHERE {
  ?work rdf:type jolux:Act ;
        jolux:typeDocument ?type ;
        jolux:dateDocument ?dateDoc ;
        jolux:isRealizedBy ?expr .
  ?expr jolux:title ?title ; jolux:language ?lang .
  OPTIONAL { ?work jolux:dateEntryInForce ?entry }
  OPTIONAL { ?work jolux:dateNoLongerInForce ?noLonger }
  OPTIONAL { ?work jolux:inForceStatus ?status }
}
ORDER BY ?work
LIMIT %d OFFSET %d`

// relationsQuery returns the modifies/repeals/cites/consolidates edges of one
// work.
const relationsQuery = `PREFIX jolux: <` + legilux.NSjolux + `>
SELECT ?rel ?target WHERE {
  VALUES ?rel { jolux:modifies jolux:repeals jolux:cites jolux:consolidates }
  <%s> ?rel ?target .
}
ORDER BY ?rel ?target`

// actsPage fetches one page of act rows.
func (c *client) actsPage(ctx context.Context, offset, limit int) ([]legilux.ActRow, error) {
	q := fmt.Sprintf(actsQuery, limit, offset)
	b, err := c.query(ctx, q)
	if err != nil {
		return nil, err
	}
	res, err := legilux.ParseResults(b)
	if err != nil {
		return nil, err
	}
	return legilux.ParseActRows(res), nil
}

// importAct fetches one act's relations and stores it.
func (c *client) importAct(ctx context.Context, st *store.Store, r legilux.ActRow) error {
	rb, err := c.query(ctx, fmt.Sprintf(relationsQuery, r.WorkURI))
	if err != nil {
		return err
	}
	relRes, err := legilux.ParseResults(rb)
	if err != nil {
		return err
	}
	amends, repeals, cites, consolidates := legilux.ParseRelations(relRes)

	act := legilux.ToAct(r, amends, repeals, cites, consolidates, c.cfg.Now)
	if act == nil {
		return nil // no version date → skip (ontology invariant)
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

// query issues a SPARQL SELECT and returns the JSON results body, throttled and
// retried on transient errors (429 / 5xx).
func (c *client) query(ctx context.Context, sparql string) ([]byte, error) {
	endpoint := c.cfg.Endpoint
	form := url.Values{}
	form.Set("query", sparql)
	full := endpoint + "?" + form.Encode()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.limiter.wait(ctx)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.cfg.UA)
		req.Header.Set("Accept", "application/sparql-results+json")
		resp, err := c.cfg.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("query: %w", err)
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
			lastErr = fmt.Errorf("query: status %d", resp.StatusCode)
			if !sleepBackoff(ctx, attempt) {
				return nil, lastErr
			}
		default:
			return nil, fmt.Errorf("query: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
