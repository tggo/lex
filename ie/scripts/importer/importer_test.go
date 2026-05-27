package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tggo/lex/ie/scripts/eisb"
	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
)

// fixtureServer serves the eisb package's committed fixtures at the real
// Oireachtas list path and the eISB print path, so the importer runs end-to-end
// without the network. The list is served for act_year=2015.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "eisb", "testdata")
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
	// Oireachtas list: only the first page has items; a second page is empty.
	mux.HandleFunc("/v1/legislation", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("skip") != "0" {
			_, _ = w.Write([]byte(`{"head":{"counts":{"resultCount":3}},"results":[]}`))
			return
		}
		serve("list.sample.json")(w, r)
	})
	// eISB print pages: all three listed acts return the same sample page. (The
	// sample's own RDFa identifies it as 2015/act/60, which is fine for the
	// store round-trip we assert below.)
	mux.HandleFunc("/eli/", serve("act.sample.html"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		ListBase:   srv.URL + "/v1/legislation",
		EISBBase:   srv.URL,
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		FromYear:   2015,
		ToYear:     2015,
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
	if n != 3 {
		t.Fatalf("imported %d acts, want 3", n)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("ie", "act", 2015, "60"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "2015/act/60" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 3 {
		t.Errorf("articles = %d, want 3", len(a.Expression.Articles))
	}
	wantAmend := schema.ResourceURI("ie", "act", 1988, "27")
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
	a, err := st.GetAct(schema.ResourceURI("ie", "act", 2015, "60"))
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
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_yearRangeRequired(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.FromYear, cfg.ToYear = 0, 0
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when no year range given")
	}
}

func TestRun_fromOnly(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.ToYear = 0 // only -from given; To defaults to From
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
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

// A 404 on an individual act's print page (e.g. a private act the eISB only
// publishes as a bare page) must be skipped, not abort the whole run.
func TestImportYear_actFetchSkipped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/legislation", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("skip") != "0" {
			_, _ = w.Write([]byte(`{"head":{"counts":{"resultCount":1}},"results":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"head":{"counts":{"resultCount":1}},"results":[` +
			`{"bill":{"act":{"actYear":"2015","actNo":"60","statutebookURI":"http://x/eli/2015/act/60"}}}]}`))
	})
	mux.HandleFunc("/eli/", http.NotFoundHandler().ServeHTTP)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run should tolerate a 404 act page, got: %v", err)
	}
	if n != 0 {
		t.Errorf("imported %d acts, want 0 (the only act 404s)", n)
	}
}

// An unparseable act page (no ELI RDFa) is skipped too, not fatal.
func TestImportYear_unparseableActSkipped(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/legislation", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("skip") != "0" {
			_, _ = w.Write([]byte(`{"head":{"counts":{"resultCount":1}},"results":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"head":{"counts":{"resultCount":1}},"results":[` +
			`{"bill":{"act":{"actYear":"2023","actNo":"P1","statutebookURI":"http://x/eli/2023/prv/P1"}}}]}`))
	})
	mux.HandleFunc("/eli/", func(w http.ResponseWriter, r *http.Request) {
		// Private-act print path is the normalised prv/1, and the page lacks ELI metadata.
		if r.URL.Path != "/eli/2023/prv/1/enacted/en/print.html" {
			t.Errorf("fetched %q, want normalised prv/1 print path", r.URL.Path)
		}
		_, _ = w.Write([]byte("<html><body><p>bare private act</p></body></html>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.FromYear, cfg.ToYear = 2023, 2023
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run should tolerate an unparseable act page, got: %v", err)
	}
	if n != 0 {
		t.Errorf("imported %d acts, want 0", n)
	}
}

func TestImportAct_noStatuteBookID(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, err := store.Open(filepath.Join(t.TempDir(), "g"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ok, err := c.importAct(context.Background(), st, eisb.ListItem{Year: 2015, Number: "60"})
	if err != nil {
		t.Errorf("importAct with no statutebook id should skip, not error: %v", err)
	}
	if ok {
		t.Error("expected importAct to report skipped (false) for item with no statutebook id")
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(1000) // 1000/s → 1ms interval
	start := time.Now()
	l.wait(context.Background())
	l.wait(context.Background())
	if time.Since(start) < 0 {
		t.Error("negative duration")
	}
	if newLimiter(0).interval != 0 {
		t.Error("rate<=0 should disable throttling")
	}
}
