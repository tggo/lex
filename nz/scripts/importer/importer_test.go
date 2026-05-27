package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

// fixtureServer serves the lenz package's committed fixtures at the real
// legislation.govt.nz paths, so the importer runs end-to-end without the
// network. The listing is trimmed to the one act we have fixtures for.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "lenz", "testdata")
	serve := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join(fxDir, file))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(b)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/legislation-index.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><legislation>` +
			`<item category="public" year="2020" number="0038" title="Sample Reform Act 2020"/>` +
			`</legislation>`))
	})
	mux.HandleFunc("/act/public/2020/0038/latest/whole.xml", serve("act_whole.sample.xml"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL,
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		FromYear:   2020,
		ToYear:     2020,
		RatePerSec: 0, // no throttling in tests
	}
}

func TestRun_endToEnd(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true

	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d acts, want 1", n)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("nz", "public-act", 2020, "0038"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "act/public/2020/0038" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 3 {
		t.Errorf("articles = %d, want 3", len(a.Expression.Articles))
	}
}

func TestRun_buildsPersistentIndex(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	root := t.TempDir()
	cfg := baseCfg(t, srv)
	cfg.OutDir = filepath.Join(root, "graph")
	cfg.IndexPath = filepath.Join(root, "index.fts")
	cfg.Lang = "en"
	cfg.WithArticles = true

	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The persistent index is searchable on its own (no rebuild).
	idx, err := search.Open(cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	n1, _ := idx.Count()
	if n1 == 0 {
		t.Error("expected documents in the persisted index")
	}
	idx.Close()

	// Re-running the import must not duplicate index docs (incremental replace).
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	idx2, _ := search.Open(cfg.IndexPath)
	defer idx2.Close()
	n2, _ := idx2.Count()
	if n1 != n2 {
		t.Errorf("index doc count changed on re-import: %d -> %d", n1, n2)
	}
}

func TestRun_withoutArticles(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv) // WithArticles defaults false
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := st.GetAct(schema.ResourceURI("nz", "public-act", 2020, "0038"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Expression.Articles) != 0 {
		t.Errorf("articles = %d, want 0 (articles disabled)", len(a.Expression.Articles))
	}
}

func TestRun_defaultsClientAndNow(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Client = nil // exercise default client
	cfg.Now = time.Time{}
	cfg.ListURL = srv.URL + "/legislation-index.xml"
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_listFetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when list fetch 404s")
	}
}

func TestRun_yearFiltering(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.FromYear = 2099 // filter out the only act
	cfg.ToYear = 2099
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 0 {
		t.Errorf("imported %d, want 0 (filtered)", n)
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(1000) // 1000/s → 1ms interval
	start := time.Now()
	l.wait(context.Background())
	l.wait(context.Background())
	if time.Since(start) < 0 { // smoke: must not panic, monotonic
		t.Error("negative duration")
	}
	if newLimiter(0).interval != 0 {
		t.Error("rate<=0 should disable throttling")
	}
}
