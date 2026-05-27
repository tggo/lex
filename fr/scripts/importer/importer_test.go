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

const sampleCID = "LEGITEXT000006070721"

// fixtureServer serves the legi package's committed XML fixtures at the sharded
// LEGI paths, so the importer runs end-to-end without the network.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "legi", "testdata")
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
	mux.HandleFunc("/"+shardPath("TEXT", sampleCID), serve("texte_version.sample.xml"))
	mux.HandleFunc("/"+shardPath("TEXTELR", sampleCID), serve("texte_struct.sample.xml"))
	mux.HandleFunc("/"+shardPath("ARTI", "LEGIARTI000006419280"), serve("article_1.sample.xml"))
	mux.HandleFunc("/"+shardPath("ARTI", "LEGIARTI000006419281"), serve("article_2.sample.xml"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL,
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		TextCIDs:   []string{sampleCID},
		RatePerSec: 0,
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

	a, err := st.GetAct(schema.ResourceURI("fr", "code", 1804, sampleCID))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != sampleCID {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 2 {
		t.Errorf("articles = %d, want 2", len(a.Expression.Articles))
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
	a, err := st.GetAct(schema.ResourceURI("fr", "code", 1804, sampleCID))
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
	cfg.Client = nil
	cfg.Now = time.Time{}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_versionFetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when text fetch 404s")
	}
}

func TestRun_structFetchError(t *testing.T) {
	fxDir := filepath.Join("..", "legi", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/"+shardPath("TEXT", sampleCID), func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(fxDir, "texte_version.sample.xml"))
		_, _ = w.Write(b)
	})
	// TEXTELR not registered → 404.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected struct fetch error")
	}
}

func TestRun_articleFetchError(t *testing.T) {
	fxDir := filepath.Join("..", "legi", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/"+shardPath("TEXT", sampleCID), func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(fxDir, "texte_version.sample.xml"))
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/"+shardPath("TEXTELR", sampleCID), func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(fxDir, "texte_struct.sample.xml"))
		_, _ = w.Write(b)
	})
	// ARTI paths not registered → 404.
	srv := httptest.NewServer(mux)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected article fetch error")
	}
}

func TestShardPath(t *testing.T) {
	got := shardPath("TEXT", sampleCID)
	want := "TEXT/00/00/06/07/07/LEGITEXT000006070721.xml"
	if got != want {
		t.Errorf("shardPath = %q, want %q", got, want)
	}
	if p := shardPath("ARTI", "SHORT"); p != "ARTI/SHORT.xml" {
		t.Errorf("short id shardPath = %q", p)
	}
}
