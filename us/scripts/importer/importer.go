// Package importer fetches United States Code titles from the Office of the Law
// Revision Counsel's USLM XML bulk channel (uscode.house.gov) and loads them
// into a lex Badger triplestore. Network access lives here; parsing/mapping is
// in package uslm and is tested offline. US Government legal edicts are in the
// public domain (17 U.S.C. § 105). See ADR-0020.
//
// Each release point publishes one zip per title containing a single USLM XML
// document, e.g. usc01@119-4.zip → usc01.xml. The importer fetches the zip,
// extracts the XML, maps it to a schema.Act, and stores it.
package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/us/scripts/uslm"
)

// Defaults for the live OLRC USLM bulk channel.
const (
	// DefaultBase is the release-point directory holding per-title zips. The
	// release point ("119/4" etc.) is part of the base so callers point at a
	// concrete published snapshot.
	DefaultBase    = "https://uscode.house.gov/download/releasepoints/us/pl/119/4"
	DefaultRelease = "119-4"
	DefaultUA      = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 2.0
	maxRetries        = 4
	// DefaultTimeout bounds a single fetch attempt. The OLRC per-title zips
	// range from a few KB (title 1) to tens of MB, and the host is often slow,
	// so this is generous; a stalled connection trips the deadline and the
	// attempt is retried with backoff rather than hanging forever.
	DefaultTimeout = 5 * time.Minute
)

// Config controls an import run.
type Config struct {
	BaseURL    string       // release-point dir, e.g. https://uscode.house.gov/download/releasepoints/us/pl/119/4
	Release    string       // release tag used in the filename, e.g. "119-4"
	OutDir     string       // Badger store directory
	IndexPath  string       // FTS index file; if empty, no index is built
	Lang       string       // search language for stemming (e.g. "en")
	UA         string       // HTTP User-Agent
	Client     *http.Client // defaults to http.DefaultClient if nil
	Now        time.Time    // retrieval timestamp recorded on each act
	Titles     []int        // USC title numbers to fetch; defaults to 1..54
	RatePerSec float64      // request rate limit; <=0 disables throttling
}

// allTitles is the full set of USC titles (1..54).
func allTitles() []int {
	out := make([]int, 0, 54)
	for i := 1; i <= 54; i++ {
		out = append(out, i)
	}
	return out
}

// Run fetches the configured titles and writes acts to the store. It returns
// the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: DefaultTimeout}
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}
	if cfg.Release == "" {
		cfg.Release = DefaultRelease
	}
	if len(cfg.Titles) == 0 {
		cfg.Titles = allTitles()
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
	for _, n := range cfg.Titles {
		if err := c.importTitle(ctx, st, n); err != nil {
			return total, fmt.Errorf("import title %d: %w", n, err)
		}
		total++
	}
	return total, nil
}

// client bundles the run config with a rate limiter and optional FTS index.
type client struct {
	cfg     Config
	limiter *limiter
	idx     *search.Index // nil if no index is built
}

// titleZipURL builds the per-title release-point zip URL, e.g.
// .../119/4/xml_usc01@119-4.zip.
func (c *client) titleZipURL(n int) string {
	return fmt.Sprintf("%s/xml_usc%02d@%s.zip", c.cfg.BaseURL, n, c.cfg.Release)
}

// importTitle fetches one title's zip, extracts the USLM XML, maps and stores.
func (c *client) importTitle(ctx context.Context, st *store.Store, n int) error {
	url := c.titleZipURL(n)
	zb, err := c.fetch(ctx, url)
	if err != nil {
		return err
	}
	xmlBytes, err := extractXML(zb)
	if err != nil {
		return fmt.Errorf("extract %s: %w", url, err)
	}
	doc, err := uslm.ParseDocument(xmlBytes)
	if err != nil {
		return err
	}
	act, err := uslm.ToAct(doc, url, c.cfg.Now)
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

// extractXML returns the first *.xml entry from a USLM per-title zip.
func extractXML(zb []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(zb), int64(len(zb)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".xml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	return nil, fmt.Errorf("no .xml entry in zip")
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
