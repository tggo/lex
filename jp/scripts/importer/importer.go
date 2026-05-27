// Package importer fetches Japanese legislation from the e-Gov 法令API v2 and
// loads it into a lex Badger triplestore. Network access lives here; the
// parsing/mapping is in package egov and is tested offline. Source data is
// e-Gov 法令検索 (CC BY 4.0-compatible). See ADR-0011 and jp/README.md.
package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/jp/scripts/egov"
)

// Defaults for the live e-Gov 法令API v2.
const (
	DefaultBase = "https://laws.e-gov.go.jp/api/2"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	pageSize    = 100 // laws per /laws page request
	maxRetries  = 4   // transient-error retries per request
)

// Config controls an import run.
type Config struct {
	BaseURL       string       // API base, e.g. https://laws.e-gov.go.jp/api/2
	OutDir        string       // Badger store directory
	IndexPath     string       // FTS index file; if empty, no index is built
	Lang          string       // search language for the FTS index (e.g. "ja")
	UA            string       // HTTP User-Agent
	Client        *http.Client // defaults to http.DefaultClient if nil
	Now           time.Time    // retrieval timestamp recorded on each act
	WithArticles  bool         // also fetch each act's full text and parse 条
	WithRevisions bool         // also fetch each act's full revision timeline
	Limit         int          // stop after this many acts (0 = all)
}

// Run lists laws, builds acts (optionally with article text), and writes them
// to the store. It returns the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}

	recs, err := listAll(ctx, cfg)
	if err != nil {
		return 0, err
	}
	index := lawIDIndex(recs)
	resolveAmendments(recs, index)

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

	for _, r := range recs {
		// Per-act article/revision sub-resource failures are non-fatal: the
		// e-Gov API sometimes 404s an act's law_data/law_revisions (e.g. a
		// revision_id that no longer resolves). Log and store the act without
		// that sub-resource rather than aborting the whole run. Context
		// cancellation stays fatal; store/index writes below stay fatal.
		if cfg.WithArticles && r.RevisionID != "" {
			arts, err := fetchArticles(ctx, cfg, r.RevisionID)
			if err != nil {
				if ctx.Err() != nil {
					return 0, err // cancellation/timeout is fatal
				}
				log.Printf("jp import: skipping articles for %s: %v", r.Act.Number, err)
			} else {
				r.Act.Expression.Articles = arts
			}
		}
		if cfg.WithRevisions {
			revs, err := fetchRevisions(ctx, cfg, r.Act, index)
			if err != nil {
				if ctx.Err() != nil {
					return 0, err // cancellation/timeout is fatal
				}
				log.Printf("jp import: skipping revisions for %s: %v", r.Act.Number, err)
			} else {
				r.Act.Revisions = revs
			}
		}
		if err := st.AddAct(r.Act); err != nil {
			return 0, fmt.Errorf("add act %s: %w", r.Act.Number, err)
		}
		if idx != nil {
			if err := idx.ReplaceAct(r.Act); err != nil {
				return 0, fmt.Errorf("index act %s: %w", r.Act.Number, err)
			}
		}
	}
	return len(recs), nil
}

// lawIDIndex maps every listed act's law_id to its resource URI, for resolving
// amendment/repeal/revision targets against the set actually ingested.
func lawIDIndex(recs []egov.Record) map[string]string {
	m := make(map[string]string, len(recs))
	for _, r := range recs {
		m[r.Act.IDLocal] = r.Act.ResourceURI()
	}
	return m
}

// lawsPage is the paging envelope of GET /api/2/laws.
type lawsPage struct {
	Count      int `json:"count"`
	NextOffset int `json:"next_offset"`
}

// listAll walks the paginated /laws endpoint, mapping each page into records,
// stopping at cfg.Limit (0 = no limit) or when a page returns no laws.
func listAll(ctx context.Context, cfg Config) ([]egov.Record, error) {
	var out []egov.Record
	offset := 0
	for {
		limit := pageSize
		if cfg.Limit > 0 {
			if rem := cfg.Limit - len(out); rem <= 0 {
				break
			} else if rem < limit {
				limit = rem
			}
		}
		body, err := fetch(ctx, cfg, fmt.Sprintf("/laws?limit=%d&offset=%d", limit, offset))
		if err != nil {
			return nil, err
		}
		recs, err := egov.BuildRecords(body, cfg.Now)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)

		var page lawsPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("egov: parse page: %w", err)
		}
		if page.Count == 0 || page.NextOffset <= offset {
			break // no more pages
		}
		offset = page.NextOffset
	}
	if cfg.Limit > 0 && len(out) > cfg.Limit {
		out = out[:cfg.Limit]
	}
	return out, nil
}

// resolveAmendments turns each record's amending/repealing law_id into an
// eli:amended_by / eli:repealed_by edge, but only when that law is itself in
// the listed set — so every edge points at a real act node in the graph.
// Unresolvable targets (a law not in the current listing) are dropped rather
// than asserted.
func resolveAmendments(recs []egov.Record, byLawID map[string]string) {
	for _, r := range recs {
		if target, ok := byLawID[r.AmendedByLawID]; ok && r.AmendedByLawID != "" {
			r.Act.Expression.AmendedBy = append(r.Act.Expression.AmendedBy, target)
		}
		if target, ok := byLawID[r.RepealedByLawID]; ok && r.RepealedByLawID != "" {
			r.Act.Expression.RepealedBy = append(r.Act.Expression.RepealedBy, target)
		}
	}
}

// fetchRevisions pulls an act's full revision timeline and maps it to metadata
// revisions, resolving each revision's producing law against the listed set
// (byLawID) into amended_by / repealed_by edges. The current enforced revision
// is skipped — it is already the act's Expression.
func fetchRevisions(ctx context.Context, cfg Config, act *schema.Act, byLawID map[string]string) ([]schema.Revision, error) {
	b, err := fetch(ctx, cfg, "/law_revisions/"+url.PathEscape(act.IDLocal))
	if err != nil {
		return nil, err
	}
	metas, err := egov.ParseRevisions(b)
	if err != nil {
		return nil, err
	}
	out := make([]schema.Revision, 0, len(metas))
	for _, m := range metas {
		if m.VersionDate.Equal(act.Expression.VersionDate) {
			continue // this is the current expression, not a separate revision
		}
		rv := schema.Revision{VersionDate: m.VersionDate, Status: m.Status}
		if target, ok := byLawID[m.ProducedBy]; ok && m.ProducedBy != "" {
			if m.IsRepeal {
				rv.RepealedBy = []string{target}
			} else {
				rv.AmendedBy = []string{target}
			}
		}
		out = append(out, rv)
	}
	return out, nil
}

// fetchArticles downloads an act's full text and parses its 条 into articles.
func fetchArticles(ctx context.Context, cfg Config, revisionID string) ([]schema.Article, error) {
	b, err := fetch(ctx, cfg, "/law_data/"+url.PathEscape(revisionID))
	if err != nil {
		return nil, err
	}
	return egov.ParseArticles(b)
}

// fetch GETs baseURL+path with the configured User-Agent, retried on transient
// errors (network failures, 429, and 5xx) with exponential backoff. Other 4xx
// fail fast, and context cancellation aborts the retry loop.
func fetch(ctx context.Context, cfg Config, path string) ([]byte, error) {
	u := cfg.BaseURL + path
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", cfg.UA)
		resp, err := cfg.Client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("fetch %s: %w", u, err) // network → transient
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
			lastErr = fmt.Errorf("fetch %s: status %d", u, resp.StatusCode)
			if !sleepBackoff(ctx, attempt) {
				return nil, lastErr
			}
		default:
			return nil, fmt.Errorf("fetch %s: status %d", u, resp.StatusCode)
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
