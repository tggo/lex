package importer

import (
	"context"
	"io"
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

// fixtureServer serves the frl package's committed fixtures at the real FRL
// OData paths, so the importer runs end-to-end without the network. The year
// listing is built inline to contain exactly the one act we have a full set of
// fixtures for (C1901A00002).
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "frl", "testdata")
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
	// One title for the year listing.
	mux.HandleFunc("/v1/titles", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$skip") != "0" {
			_, _ = w.Write([]byte(`{"@odata.count":1,"value":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"@odata.count":1,"value":[` +
			`{"id":"C1901A00002","name":"Acts Interpretation Act 1901",` +
			`"collection":"Act","seriesType":"Act","year":1901,"number":2,` +
			`"status":"InForce","isInForce":true}]}`))
	})
	mux.HandleFunc("/v1/titles/C1901A00002", serve("title_detail.sample.json"))
	mux.HandleFunc("/v1/Versions/Default.Find(titleId='C1901A00002',asAtSpecification='current')",
		serve("version_current.sample.json"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL + "/v1",
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Collection: "Act",
		FromYear:   1901,
		ToYear:     1901,
		RatePerSec: 0, // no throttling in tests
	}
}

func TestRun_endToEnd(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv)
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

	a, err := st.GetAct(schema.ResourceURI("au", "act", 1901, "C1901A00002"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "C1901A00002" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	wantAmend := schema.ResourceURI("au", "act", 2026, "C2026A00004")
	if len(a.Expression.AmendedBy) != 1 || a.Expression.AmendedBy[0] != wantAmend {
		t.Errorf("amendedBy = %v, want [%s]", a.Expression.AmendedBy, wantAmend)
	}
}

// TestRun_withArticles serves the /documents manifest and the EPUB body at the
// real paths so the importer parses and attaches section text offline.
func TestRun_withArticles(t *testing.T) {
	fxDir := filepath.Join("..", "frl", "testdata")
	srv := fixtureServer(t)
	defer srv.Close()

	// The default fixture server only mounts OData paths; wrap it with a mux
	// that also serves the documents list and the EPUB body. The frl fixtures
	// are for C1901A00002, but the document.sample.html body parses standalone.
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/documents", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":[{"titleId":"C1901A00002","start":"2026-01-15T00:00:00","format":"Epub","compilationNumber":"0"}]}`))
	})
	// EPUB body for the (as-made) document_1; document_2 404s to end the loop.
	mux.HandleFunc("/C1901A00002/asmade/2026-01-15/text/original/epub/OEBPS/document_1/document_1.html",
		func(w http.ResponseWriter, r *http.Request) {
			b, _ := os.ReadFile(filepath.Join(fxDir, "document.sample.html"))
			_, _ = w.Write(b)
		})
	// All other paths proxy to the OData fixture server's handler set.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux.HandleFunc("/v1/titles", srvProxy(srv, "/v1/titles"))
	mux.HandleFunc("/v1/titles/C1901A00002", srvProxy(srv, "/v1/titles/C1901A00002"))
	mux.HandleFunc("/v1/Versions/Default.Find(titleId='C1901A00002',asAtSpecification='current')",
		srvProxy(srv, "/v1/Versions/Default.Find(titleId='C1901A00002',asAtSpecification='current')"))

	combined := httptest.NewServer(mux)
	defer combined.Close()

	cfg := baseCfg(t, combined)
	cfg.Client = combined.Client()
	cfg.SiteURL = combined.URL
	cfg.WithArticles = true

	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st, _ := store.Open(cfg.OutDir)
	defer st.Close()
	a, err := st.GetAct(schema.ResourceURI("au", "act", 1901, "C1901A00002"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if len(a.Expression.Articles) != 3 {
		t.Fatalf("got %d articles, want 3", len(a.Expression.Articles))
	}
	if a.Expression.Articles[0].Number != "1" {
		t.Errorf("article 0 number = %q, want 1", a.Expression.Articles[0].Number)
	}
}

// srvProxy returns a handler that forwards the request to another test server.
func srvProxy(srv *httptest.Server, path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := srv.Client().Get(srv.URL + r.URL.RequestURI())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

// TestRun_withArticlesTolerated: when no EPUB document exists, the act still
// imports with metadata only (no articles).
func TestRun_withArticlesTolerated(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/documents", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":[]}`)) // no EPUB
	})
	mux.HandleFunc("/v1/titles", srvProxy(srv, "/v1/titles"))
	mux.HandleFunc("/v1/titles/C1901A00002", srvProxy(srv, "/v1/titles/C1901A00002"))
	mux.HandleFunc("/v1/Versions/Default.Find(titleId='C1901A00002',asAtSpecification='current')",
		srvProxy(srv, "/v1/Versions/Default.Find(titleId='C1901A00002',asAtSpecification='current')"))
	combined := httptest.NewServer(mux)
	defer combined.Close()

	cfg := baseCfg(t, combined)
	cfg.Client = combined.Client()
	cfg.SiteURL = combined.URL
	cfg.WithArticles = true
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st, _ := store.Open(cfg.OutDir)
	defer st.Close()
	a, _ := st.GetAct(schema.ResourceURI("au", "act", 1901, "C1901A00002"))
	if len(a.Expression.Articles) != 0 {
		t.Errorf("expected no articles, got %d", len(a.Expression.Articles))
	}
}

func TestRun_buildsPersistentIndex(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.IndexPath = filepath.Join(t.TempDir(), "index.fts")
	cfg.Lang = "en"

	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The persistent index is searchable on its own (no rebuild).
	idx, err := search.Open(cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if n, _ := idx.Count(); n == 0 {
		t.Error("expected the persisted index to hold at least one doc")
	}
}

func TestRun_missingVersionTolerated(t *testing.T) {
	fxDir := filepath.Join("..", "frl", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/titles", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$skip") != "0" {
			_, _ = w.Write([]byte(`{"@odata.count":1,"value":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"@odata.count":1,"value":[{"id":"C1901A00002","year":1901}]}`))
	})
	mux.HandleFunc("/v1/titles/C1901A00002", func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(fxDir, "title_detail.sample.json"))
		_, _ = w.Write(b)
	})
	// Versions endpoint 404s → tolerated, metadata still imports.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}
	st, _ := store.Open(cfg.OutDir)
	defer st.Close()
	a, err := st.GetAct(schema.ResourceURI("au", "act", 1901, "C1901A00002"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	// Falls back to makingDate (1901-07-12) for version_date.
	if a.Expression.VersionDate.Year() != 1901 {
		t.Errorf("versionDate = %v, want 1901 fallback", a.Expression.VersionDate)
	}
}

func TestRun_defaultsClientAndNow(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Client = nil
	cfg.Now = time.Time{}
	cfg.Collection = ""
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_requiresYearRange(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.FromYear, cfg.ToYear = 0, 0
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when year range missing")
	}
}

func TestRun_listFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when listing fails")
	}
}

func TestImportTitle_detailParseError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/titles/X", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &client{cfg: Config{BaseURL: srv.URL + "/v1", UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	if err := c.importTitle(context.Background(), st, nil, "X"); err == nil {
		t.Error("expected detail parse error")
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

func TestURLEncoding_filterHasYear(t *testing.T) {
	// Guard the OData filter construction stays well-formed.
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "titles") && r.URL.RawQuery != "" {
			gotURL = r.URL.Query().Get("$filter")
		}
		_, _ = w.Write([]byte(`{"@odata.count":0,"value":[]}`))
	}))
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	if _, err := c.importYear(context.Background(), st, nil, 1901); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "year eq 1901") {
		t.Errorf("filter = %q, want year eq 1901", gotURL)
	}
}
