package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

// fixtureServer serves the ogd package's committed fixtures at the real OGD
// paths, so the importer runs end-to-end without the network.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "ogd", "testdata")
	routes := map[string]string{
		"/perv/cards.json":         "cards.sample.json",
		"/perv/texts.json":         "texts.sample.json",
		"/laws/data/csv/perv1.txt": "perv1.sample.txt",
		"/laws/data/csv/perv0.txt": "perv0.sample.txt",
		"/laws/data/csv/perv2.txt": "perv2.sample.txt", // absent → empty list
	}
	mux := http.NewServeMux()
	for route, file := range routes {
		path := filepath.Join(fxDir, file)
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(path)
			if err != nil {
				w.WriteHeader(http.StatusOK) // missing fixture = empty body
				return
			}
			_, _ = w.Write(b)
		})
	}
	// Act HTML bodies: only act 4840-20 (dokid 553665) has the article fixture;
	// the others return an empty (article-less) body.
	articleFixture := filepath.Join(fxDir, "act_articles.sample.htm")
	mux.HandleFunc("/perv/text/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "d553665.htm") {
			b, _ := os.ReadFile(articleFixture)
			_, _ = w.Write(b)
			return
		}
		_, _ = w.Write([]byte(`<html><body></body></html>`))
	})
	return httptest.NewServer(mux)
}

func TestRun_endToEnd(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "graph")
	cfg := Config{
		BaseURL: srv.URL,
		OutDir:  dir,
		UA:      "lex-test",
		Client:  srv.Client(),
		Now:     time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
	}

	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 3 {
		t.Fatalf("imported %d acts, want 3", n)
	}

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("ua", "zakon", 2026, "4840-20"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.Expression.Title != "Про основні засади державного нагляду (контролю)" {
		t.Errorf("title = %q", a.Expression.Title)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if a.IDLocal != "4840-IX" {
		t.Errorf("idLocal = %q, want 4840-IX", a.IDLocal)
	}
}

func TestRun_withArticles(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "graph")
	cfg := Config{
		BaseURL:      srv.URL,
		OutDir:       dir,
		UA:           "lex-test",
		Client:       srv.Client(),
		Now:          time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		WithArticles: true,
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("ua", "zakon", 2026, "4840-20"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Expression.Articles) != 2 {
		t.Fatalf("got %d articles, want 2", len(a.Expression.Articles))
	}
	if a.Expression.Articles[0].Number != "1" {
		t.Errorf("article[0].Number = %q, want 1", a.Expression.Articles[0].Number)
	}
}

func TestRun_buildsPersistentIndex(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	root := t.TempDir()
	cfg := Config{
		BaseURL:   srv.URL,
		OutDir:    filepath.Join(root, "graph"),
		IndexPath: filepath.Join(root, "index.fts"),
		UA:        "lex-test",
		Client:    srv.Client(),
		Now:       time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The persistent index is searchable on its own (no rebuild).
	idx, err := search.Open(cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	n1, _ := idx.Count()
	if hits, _ := idx.Search("нагляду", 10); len(hits) == 0 {
		t.Error("expected a hit in the persisted index")
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

func TestRun_fetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := Config{BaseURL: srv.URL, OutDir: t.TempDir(), UA: "x", Client: srv.Client()}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when fetch returns 404")
	}
}

func TestRun_defaultsClientAndNow(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	// Client and Now left zero → Run fills defaults; must still succeed.
	cfg := Config{BaseURL: srv.URL, OutDir: filepath.Join(t.TempDir(), "g"), UA: "x"}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestUnion(t *testing.T) {
	got := union(map[string]bool{"a": true}, map[string]bool{"b": true})
	if !got["a"] || !got["b"] || len(got) != 2 {
		t.Errorf("union = %v", got)
	}
}
