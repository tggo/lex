// Package importer fetches French legislation from the DILA LEGI open-data
// dataset and loads it into a lex Badger triplestore. Network access lives
// here; parsing/mapping is in package legi and is tested offline. French
// legislative texts are not objects of copyright; LEGI is published under the
// Licence Ouverte / Open Licence (Etalab), attribution to DILA. See ADR-0016.
//
// LEGI is a bulk XML corpus, not a query API: each object is one XML file under
// a sharded path (e.g. LEGI/TEXT/00/00/06/07/07/LEGITEXT000006070721.xml,
// LEGI/ARTI/00/00/06/41/92/LEGIARTI000006419280.xml). This importer fetches the
// individual XML files for a requested set of text CIDs from an HTTP-served
// extraction of the dataset (DILA publishes a browsable directory; a local
// mirror can be served with any static file server). The TEXTELR "struct" of
// each text lists its member articles, which are then fetched and parsed.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tggo/lex/fr/scripts/legi"
	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live DILA LEGI open data.
const (
	// DefaultBase is the root under which the sharded LEGI XML tree is served.
	// DILA publishes the bulk tarballs at echanges.dila.gouv.fr/OPENDATA/LEGI/;
	// extract one and serve its LEGI/ directory (or point at any mirror).
	DefaultBase = "https://echanges.dila.gouv.fr/OPENDATA/LEGI"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	// DefaultRatePerSec throttles requests to be a polite client.
	DefaultRatePerSec = 5.0
	maxRetries        = 4
)

// Config controls an import run.
type Config struct {
	BaseURL      string       // root of the served LEGI XML tree
	OutDir       string       // Badger store directory
	IndexPath    string       // FTS index file; if empty, no index is built
	Lang         string       // search stemming language
	UA           string       // HTTP User-Agent
	Client       *http.Client // defaults to http.DefaultClient if nil
	Now          time.Time    // retrieval timestamp recorded on each act
	TextCIDs     []string     // LEGITEXT… ids to import
	WithArticles bool         // also fetch each text's articles and parse text
	RatePerSec   float64      // request rate limit; <=0 disables throttling
}

// Run fetches the configured texts and writes acts to the store. It returns the
// number of acts written.
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
	for _, cid := range cfg.TextCIDs {
		if err := c.importText(ctx, st, cid); err != nil {
			return total, fmt.Errorf("import %s: %w", cid, err)
		}
		total++
	}
	return total, nil
}

// client bundles the run config with a rate limiter.
type client struct {
	cfg     Config
	limiter *limiter
	idx     *search.Index
}

// shardPath builds the sharded relative path for a LEGI id. LEGI stores objects
// under <kind>/<2-digit groups of the 12-digit suffix>/<id>.xml, e.g.
// LEGITEXT000006070721 → TEXT/00/00/06/07/07/LEGITEXT000006070721.xml.
func shardPath(kind, id string) string {
	// The numeric suffix is the trailing 18 chars; LEGI shards on the first
	// 10 of the 12-significant digits as five 2-digit groups.
	n := len(id)
	if n < 10 {
		return kind + "/" + id + ".xml"
	}
	digits := id[n-12:] // 12 trailing digits
	p := kind + "/" +
		digits[0:2] + "/" + digits[2:4] + "/" + digits[4:6] + "/" +
		digits[6:8] + "/" + digits[8:10] + "/" + id + ".xml"
	return p
}

// importText fetches one text's version + struct (+ member articles) and stores
// it as a schema.Act.
func (c *client) importText(ctx context.Context, st *store.Store, cid string) error {
	vb, err := c.fetch(ctx, c.cfg.BaseURL+"/"+shardPath("TEXT", cid))
	if err != nil {
		return err
	}
	tv, err := legi.ParseTexteVersion(vb)
	if err != nil {
		return err
	}

	sb, err := c.fetch(ctx, c.cfg.BaseURL+"/"+shardPath("TEXTELR", cid))
	if err != nil {
		return err
	}
	tstruct, err := legi.ParseTexteStruct(sb)
	if err != nil {
		return err
	}

	var arts []schema.Article
	var liens []legi.Lien
	if c.cfg.WithArticles {
		byID := map[string]*legi.Article{}
		for _, la := range tstruct.Liens {
			ab, err := c.fetch(ctx, c.cfg.BaseURL+"/"+shardPath("ARTI", la.ID))
			if err != nil {
				return err
			}
			a, err := legi.ParseArticle(ab)
			if err != nil {
				return err
			}
			byID[a.Common.ID] = a
		}
		arts, liens = legi.BuildArticles(tstruct, byID)
	}

	act, err := legi.ToAct(tv, arts, liens, c.cfg.Now)
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
