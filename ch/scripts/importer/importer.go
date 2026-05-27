// Package importer fetches Swiss federal legislation from the Fedlex SPARQL
// endpoint (https://fedlex.data.admin.ch/sparqlendpoint) and loads it into a
// lex Badger triplestore. Network access lives here; parsing/mapping is in
// package fedlex and is tested offline. Swiss official federal legislation is
// not protected by copyright; we attribute Fedlex / the Swiss Confederation.
// See ADR-0017.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tggo/lex/ch/scripts/fedlex"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live Fedlex SPARQL endpoint.
const (
	DefaultEndpoint = "https://fedlex.data.admin.ch/sparqlendpoint"
	DefaultUA       = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 2.0
	maxRetries        = 4
)

// query is the SPARQL that selects current Classified Compilation (SR) works
// with their German title, short title, SR notation, and in-force dates. The
// %s is replaced by an optional VALUES/FILTER clause restricting SR notations;
// when empty the whole SR is enumerated (large — use -limit and rate limiting).
const query = `PREFIX jolux: <http://data.legilux.public.lu/resource/ontology/jolux#>
PREFIX skos: <http://www.w3.org/2004/02/skos/core#>
SELECT ?cc ?srNotation (SAMPLE(?t) AS ?title) (SAMPLE(?ts) AS ?titleShort)
       (SAMPLE(?eif) AS ?dateEntryInForce) (SAMPLE(?nlf) AS ?dateNoLongerInForce) WHERE {
  ?cc a jolux:ConsolidationAbstract ;
      jolux:classifiedByTaxonomyEntry ?tax .
  ?tax skos:notation ?srNotation .
  FILTER(datatype(?srNotation) = <https://fedlex.data.admin.ch/vocabulary/notation-type/id-systematique>)
  %s
  FILTER NOT EXISTS { ?cc jolux:dateNoLongerInForce ?repealed }
  ?cc jolux:isRealizedBy ?expr .
  ?expr jolux:language <` + fedlex.LanguageDEU + `> ;
        jolux:title ?t .
  OPTIONAL { ?expr jolux:titleShort ?ts }
  OPTIONAL { ?cc jolux:dateEntryInForce ?eif }
  OPTIONAL { ?cc jolux:dateNoLongerInForce ?nlf }
} GROUP BY ?cc ?srNotation ORDER BY ?srNotation %s`

// Config controls an import run.
type Config struct {
	Endpoint string       // SPARQL endpoint URL
	OutDir   string       // Badger store directory
	UA       string       // HTTP User-Agent
	Client   *http.Client // defaults to http.DefaultClient if nil
	Now      time.Time    // retrieval timestamp recorded on each act

	// SRNotations, when non-empty, restricts the import to these exact SR
	// numbers (e.g. ["210","220"]). Empty enumerates everything (subject to
	// Limit).
	SRNotations []string
	Limit       int     // SPARQL LIMIT (0 = no limit)
	RatePerSec  float64 // request rate limit; <=0 disables throttling
}

// Run fetches the configured SR works and writes acts to the store. It returns
// the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}

	c := &client{cfg: cfg, limiter: newLimiter(cfg.RatePerSec)}

	body, err := c.fetch(ctx, c.buildQuery())
	if err != nil {
		return 0, err
	}
	bindings, err := fedlex.ParseResults(body)
	if err != nil {
		return 0, err
	}
	acts := fedlex.ToActs(bindings, cfg.Now)

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	n := 0
	for _, a := range acts {
		if err := st.AddAct(a); err != nil {
			return n, fmt.Errorf("store %s: %w", a.IDLocal, err)
		}
		n++
	}
	return n, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
}

// buildQuery assembles the SPARQL query string from the config's SR filter and
// limit.
func (c *client) buildQuery() string {
	var filter string
	if len(c.cfg.SRNotations) > 0 {
		quoted := make([]string, len(c.cfg.SRNotations))
		for i, sr := range c.cfg.SRNotations {
			quoted[i] = `"` + sr + `"`
		}
		filter = "FILTER(STR(?srNotation) IN (" + strings.Join(quoted, ", ") + "))"
	}
	limit := ""
	if c.cfg.Limit > 0 {
		limit = fmt.Sprintf("LIMIT %d", c.cfg.Limit)
	}
	return fmt.Sprintf(query, filter, limit)
}

// fetch POSTs a SPARQL query to the endpoint, throttled and retried on
// transient errors (429 / 5xx). It asks for SPARQL 1.1 JSON results.
func (c *client) fetch(ctx context.Context, sparql string) ([]byte, error) {
	form := url.Values{"query": {sparql}}.Encode()
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		c.limiter.wait(ctx)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, strings.NewReader(form))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", c.cfg.UA)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/sparql-results+json")
		resp, err := c.cfg.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetch sparql: %w", err)
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
			lastErr = fmt.Errorf("fetch sparql: status %d", resp.StatusCode)
			if !sleepBackoff(ctx, attempt) {
				return nil, lastErr
			}
		default:
			return nil, fmt.Errorf("fetch sparql: status %d", resp.StatusCode)
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
