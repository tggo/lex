package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
)

// articlesServer serves three shapes for an articles-enabled run:
//   - POST whose query mentions ConsolidationAbstract+notation but NOT "?file":
//     the metadata SPARQL fixture (sr_acts.sample.json).
//   - POST whose query selects ?file (the text-resolution query): a one-row
//     SPARQL result whose ?file points at this server's /file path.
//   - GET /file: the committed Akoma Ntoso XML fixture.
func articlesServer(t *testing.T) *httptest.Server {
	t.Helper()
	meta := filepath.Join("..", "fedlex", "testdata", "sr_acts.sample.json")
	akn := filepath.Join("..", "fedlex", "testdata", "cc_akn.sample.xml")
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			b, err := os.ReadFile(akn)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write(b)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		q := r.Form.Get("query")
		if strings.Contains(q, "?file") {
			fileURL := srv.URL + "/file"
			w.Header().Set("Content-Type", "application/sparql-results+json")
			_, _ = w.Write([]byte(`{"head":{"vars":["file","dateApp"]},"results":{"bindings":[` +
				`{"file":{"type":"uri","value":"` + fileURL + `"},"dateApp":{"type":"literal","value":"2026-01-01"}}]}}`))
			return
		}
		b, err := os.ReadFile(meta)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write(b)
	}))
	return srv
}

func TestRun_withArticles(t *testing.T) {
	srv := articlesServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("ch", "gesetzbuch", 1912, "210"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if len(a.Expression.Articles) != 3 {
		t.Fatalf("got %d articles, want 3", len(a.Expression.Articles))
	}
	if a.Expression.Articles[0].Number != "1" {
		t.Errorf("article[0].Number = %q, want 1", a.Expression.Articles[0].Number)
	}
	if a.Expression.Articles[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
}

// TestRun_withArticles_resolveFailsNonFatal proves a failed text resolution
// (here: the text query 500s) does not abort the run; acts still import with
// metadata and no articles.
func TestRun_withArticles_resolveFailsNonFatal(t *testing.T) {
	meta := filepath.Join("..", "fedlex", "testdata", "sr_acts.sample.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if strings.Contains(r.Form.Get("query"), "?file") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		b, _ := os.ReadFile(meta)
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run should not fail on article-resolution error: %v", err)
	}
	if n != 3 {
		t.Fatalf("imported %d acts, want 3", n)
	}
	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := st.GetAct(schema.ResourceURI("ch", "gesetzbuch", 1912, "210"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if len(a.Expression.Articles) != 0 {
		t.Errorf("got %d articles, want 0 when resolution failed", len(a.Expression.Articles))
	}
}

// TestRun_withArticles_noFileNonFatal covers an empty text-resolution result
// (the SR has no XML manifestation): logged and skipped, run succeeds.
func TestRun_withArticles_noFileNonFatal(t *testing.T) {
	meta := filepath.Join("..", "fedlex", "testdata", "sr_acts.sample.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/sparql-results+json")
		if strings.Contains(r.Form.Get("query"), "?file") {
			_, _ = w.Write([]byte(`{"head":{"vars":["file"]},"results":{"bindings":[]}}`))
			return
		}
		b, _ := os.ReadFile(meta)
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestResolveTextURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write([]byte(`{"head":{"vars":["file"]},"results":{"bindings":[{"file":{"type":"uri","value":"https://x/y.xml"}}]}}`))
	}))
	defer srv.Close()
	c := &client{cfg: Config{Endpoint: srv.URL, UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	got, err := c.resolveTextURL(context.Background(), "210")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://x/y.xml" {
		t.Errorf("got %q", got)
	}
}

func TestFetchGET_status(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := &client{cfg: Config{UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	if _, err := c.fetchGET(context.Background(), srv.URL); err == nil {
		t.Error("expected error for 404")
	}
}
