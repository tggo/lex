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
	"github.com/tggo/lex/internal/store"
)

// fixtureServer serves the akn package's committed fixtures at the real Finlex
// API paths, so the importer runs end-to-end without the network. The list
// returns both fixture acts; the full-expression endpoints serve the
// single-act documents (which carry the body sections).
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "akn", "testdata")
	serve := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join(fxDir, file))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write(b)
		}
	}
	base := "/finlex/avoindata/v1/" + collection
	mux := http.NewServeMux()
	mux.HandleFunc(base, serve("list.sample.xml"))
	mux.HandleFunc(base+"/2019/469/fin@", serve("act.sample.xml"))
	mux.HandleFunc(base+"/2025/51/fin@", serve("act_decree.sample.xml"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL + "/finlex/avoindata/v1",
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
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
	if n != 2 {
		t.Fatalf("imported %d acts, want 2", n)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("fi", "laki", 2019, "2019/469"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "http://data.finlex.fi/eli/sd/2019/469/ajantasa" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 15 {
		t.Errorf("articles = %d, want 15", len(a.Expression.Articles))
	}
	wantAmend := schema.ResourceURI("fi", "statute", 2022, "2022/1099")
	if len(a.Expression.AmendedBy) != 1 || a.Expression.AmendedBy[0] != wantAmend {
		t.Errorf("amendedBy = %v, want [%s]", a.Expression.AmendedBy, wantAmend)
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
	a, err := st.GetAct(schema.ResourceURI("fi", "laki", 2019, "2019/469"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Expression.Articles) != 0 {
		t.Errorf("articles = %d, want 0 (articles disabled)", len(a.Expression.Articles))
	}
}

func TestRun_limit(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Limit = 1
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Errorf("imported %d acts, want 1 (limit)", n)
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

func TestRun_listFetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when list fetch 404s")
	}
}

func TestRun_fullFetchError(t *testing.T) {
	fxDir := filepath.Join("..", "akn", "testdata")
	base := "/finlex/avoindata/v1/" + collection
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(fxDir, "list.sample.xml"))
		_, _ = w.Write(b)
	})
	// full-expression endpoints intentionally 404.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when full-expression fetch 404s")
	}
}
