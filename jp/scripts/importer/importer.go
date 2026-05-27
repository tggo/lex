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
	"net/http"
	"net/url"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/jp/scripts/egov"
)

// Defaults for the live e-Gov 法令API v2.
const (
	DefaultBase = "https://laws.e-gov.go.jp/api/2"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	pageSize    = 100 // laws per /laws page request
)

// Config controls an import run.
type Config struct {
	BaseURL      string       // API base, e.g. https://laws.e-gov.go.jp/api/2
	OutDir       string       // Badger store directory
	UA           string       // HTTP User-Agent
	Client       *http.Client // defaults to http.DefaultClient if nil
	Now          time.Time    // retrieval timestamp recorded on each act
	WithArticles bool         // also fetch each act's full text and parse 条
	Limit        int          // stop after this many acts (0 = all)
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
	resolveAmendments(recs)

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	for _, r := range recs {
		if cfg.WithArticles && r.RevisionID != "" {
			arts, err := fetchArticles(ctx, cfg, r.RevisionID)
			if err != nil {
				return 0, fmt.Errorf("articles for %s: %w", r.Act.Number, err)
			}
			r.Act.Expression.Articles = arts
		}
		if err := st.AddAct(r.Act); err != nil {
			return 0, fmt.Errorf("add act %s: %w", r.Act.Number, err)
		}
	}
	return len(recs), nil
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

// resolveAmendments turns each record's AmendedByLawID into an eli:amended_by
// edge, but only when the amending law is itself in the listed set — so every
// edge points at a real act node in the graph. Unresolvable targets (e.g. an
// amending law not in the current listing) are dropped rather than asserted.
func resolveAmendments(recs []egov.Record) {
	byLawID := make(map[string]string, len(recs)) // law_id -> resource URI
	for _, r := range recs {
		byLawID[r.Act.IDLocal] = r.Act.ResourceURI()
	}
	for _, r := range recs {
		if r.AmendedByLawID == "" {
			continue
		}
		if target, ok := byLawID[r.AmendedByLawID]; ok {
			r.Act.Expression.AmendedBy = append(r.Act.Expression.AmendedBy, target)
		}
	}
}

// fetchArticles downloads an act's full text and parses its 条 into articles.
func fetchArticles(ctx context.Context, cfg Config, revisionID string) ([]schema.Article, error) {
	b, err := fetch(ctx, cfg, "/law_data/"+url.PathEscape(revisionID))
	if err != nil {
		return nil, err
	}
	return egov.ParseArticles(b)
}

// fetch GETs baseURL+path with the configured User-Agent.
func fetch(ctx context.Context, cfg Config, path string) ([]byte, error) {
	u := cfg.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", cfg.UA)
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
