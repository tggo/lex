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

const sampleID = "BOE-A-2021-6945"

// fixtureServer serves the boe package's committed fixtures at the real BOE
// open-data API paths, so the importer runs end-to-end without the network. The
// listing is built inline to contain exactly the one norm we have fixtures for.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "boe", "testdata")
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
	base := "/datosabiertos/api/legislacion-consolidada"
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "0" {
			_, _ = w.Write([]byte(`{"status":{"code":"200"},"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":{"code":"200"},"data":[{"identificador":"` + sampleID + `"}]}`))
	})
	mux.HandleFunc(base+"/id/"+sampleID+"/metadatos", serve("norma_metadatos.sample.json"))
	mux.HandleFunc(base+"/id/"+sampleID+"/analisis", serve("norma_analisis.sample.json"))
	mux.HandleFunc(base+"/id/"+sampleID+"/texto", serve("norma_texto.sample.xml"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL + "/datosabiertos/api/legislacion-consolidada",
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
	if n != 1 {
		t.Fatalf("imported %d acts, want 1", n)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("es", "ley", 2021, sampleID))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != sampleID {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 2 {
		t.Errorf("articles = %d, want 2", len(a.Expression.Articles))
	}
	wantRepeal := schema.ResourceURI("es", "norma", 1988, "BOE-A-1988-29622")
	if len(a.Expression.Repeals) != 1 || a.Expression.Repeals[0] != wantRepeal {
		t.Errorf("repeals = %v, want [%s]", a.Expression.Repeals, wantRepeal)
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
	a, err := st.GetAct(schema.ResourceURI("es", "ley", 2021, sampleID))
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
		t.Errorf("imported %d, want 1 (limit)", n)
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
