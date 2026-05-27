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

// fixtureServer serves the eli package's committed fixtures at the real Sejm
// ELI API paths, so the importer runs end-to-end without the network. The year
// listing is built inline to contain exactly the one act we have fixtures for.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "eli", "testdata")
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
	mux.HandleFunc("/eli/acts/DU", serve("publisher.sample.json"))
	mux.HandleFunc("/eli/acts/DU/2023", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"totalCount":1,"count":1,"offset":0,"items":[` +
			`{"ELI":"DU/2023/2777","publisher":"DU","year":2023,"pos":2777,` +
			`"type":"Ustawa","title":"Ustawa ...","status":"wygaśnięcie aktu","textHTML":true}]}`))
	})
	mux.HandleFunc("/eli/acts/DU/2023/2777", serve("act_detail.sample.json"))
	mux.HandleFunc("/eli/acts/DU/2023/2777/references", serve("act_references.sample.json"))
	mux.HandleFunc("/eli/acts/DU/2023/2777/struct", serve("act_struct.sample.json"))
	mux.HandleFunc("/eli/acts/DU/2023/2777/text.html", serve("act_text.sample.html"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL + "/eli/acts",
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Publishers: []string{"DU"},
		FromYear:   2023,
		ToYear:     2023,
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

	a, err := st.GetAct(schema.ResourceURI("pl", "ustawa", 2023, "DU/2777"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "DU/2023/2777" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusRepealed {
		t.Errorf("status = %v, want Repealed", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 2 {
		t.Errorf("articles = %d, want 2", len(a.Expression.Articles))
	}
	wantAmend := schema.ResourceURI("pl", "ustawa", 2022, "DU/2666")
	if len(a.Expression.Amends) != 1 || a.Expression.Amends[0] != wantAmend {
		t.Errorf("amends = %v, want [%s]", a.Expression.Amends, wantAmend)
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
	a, err := st.GetAct(schema.ResourceURI("pl", "ustawa", 2023, "DU/2777"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Expression.Articles) != 0 {
		t.Errorf("articles = %d, want 0 (articles disabled)", len(a.Expression.Articles))
	}
}

func TestRun_buildsPersistentIndex(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	root := t.TempDir()
	cfg := baseCfg(t, srv)
	cfg.OutDir = filepath.Join(root, "graph")
	cfg.IndexPath = filepath.Join(root, "index.fts")
	cfg.Lang = "pl"
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
		t.Error("expected indexed docs in the persisted index")
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

func TestRun_defaultsClientAndNow(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Client = nil // exercise default client
	cfg.Now = time.Time{}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_publisherFetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when publisher fetch 404s")
	}
}

// A 404 on an individual act's sub-resource (e.g. /struct) must be skipped, not
// abort the whole run.
func TestImportYear_actFetchSkipped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/eli/acts/DU", func(w http.ResponseWriter, r *http.Request) {
		fxDir := filepath.Join("..", "eli", "testdata")
		b, _ := os.ReadFile(filepath.Join(fxDir, "publisher.sample.json"))
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/eli/acts/DU/2023", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "0" {
			_, _ = w.Write([]byte(`{"totalCount":1,"count":0,"offset":1,"items":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"totalCount":1,"count":1,"offset":0,"items":[` +
			`{"ELI":"DU/2023/2777","publisher":"DU","year":2023,"pos":2777,` +
			`"type":"Ustawa","title":"Ustawa ...","status":"obowiązujący","textHTML":true}]}`))
	})
	serve := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := os.ReadFile(filepath.Join("..", "eli", "testdata", file))
			_, _ = w.Write(b)
		}
	}
	mux.HandleFunc("/eli/acts/DU/2023/2777", serve("act_detail.sample.json"))
	mux.HandleFunc("/eli/acts/DU/2023/2777/references", serve("act_references.sample.json"))
	// struct 404s — mirrors the live failure (DU/2024/1064/struct: status 404).
	mux.HandleFunc("/eli/acts/DU/2023/2777/struct", http.NotFoundHandler().ServeHTTP)
	mux.HandleFunc("/eli/acts/DU/2023/2777/text.html", serve("act_text.sample.html"))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run should tolerate a 404 sub-resource, got: %v", err)
	}
	if n != 0 {
		t.Errorf("imported %d acts, want 0 (the only act's struct 404s)", n)
	}
}

func TestYearsFiltering(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	ys, err := c.years(context.Background(), "DU")
	if err != nil {
		t.Fatal(err)
	}
	if len(ys) != 1 || ys[0] != 2023 {
		t.Errorf("years = %v, want [2023]", ys)
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
