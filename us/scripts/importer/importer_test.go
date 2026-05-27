package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
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

// titleXML reads the committed USLM fixture used across the uslm package.
func titleXML(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "uslm", "testdata", "usc01.sample.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

// zipOf wraps xml bytes into a USLM-style per-title zip (one .xml entry).
func zipOf(t *testing.T, name string, xmlBytes []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(xmlBytes); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fixtureServer serves the title-1 zip at the real OLRC release-point path so
// the importer runs end-to-end without the network.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	zb := zipOf(t, "usc01.xml", titleXML(t))
	mux := http.NewServeMux()
	mux.HandleFunc("/dl/us/pl/119/4/xml_usc01@119-4.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zb)
	})
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL + "/dl/us/pl/119/4",
		Release:    "119-4",
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Titles:     []int{1},
		RatePerSec: 0,
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

	a, err := st.GetAct(schema.ResourceURI("us", "usc-title", 2024, "title-1"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "usc/title-1" {
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
	cfg.Client = nil
	cfg.Now = time.Time{}
	cfg.Release = "" // exercise default release tag
	cfg.BaseURL = srv.URL + "/dl/us/pl/119/4"
	// With default release "119-4" the URL still matches the fixture path.
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_fetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when title fetch 404s")
	}
}

func TestRun_allTitlesDefault(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Titles = nil // triggers allTitles(); first fetch 404s and errors out
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error from first title")
	}
	if len(allTitles()) != 54 {
		t.Errorf("allTitles len = %d, want 54", len(allTitles()))
	}
}

func TestTitleZipURL(t *testing.T) {
	c := &client{cfg: Config{BaseURL: "https://x/119/4", Release: "119-4"}}
	if got := c.titleZipURL(1); got != "https://x/119/4/xml_usc01@119-4.zip" {
		t.Errorf("titleZipURL(1) = %q", got)
	}
	if got := c.titleZipURL(26); got != "https://x/119/4/xml_usc26@119-4.zip" {
		t.Errorf("titleZipURL(26) = %q", got)
	}
}

func TestExtractXML(t *testing.T) {
	zb := zipOf(t, "usc01.xml", []byte("<uslm/>"))
	b, err := extractXML(zb)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "<uslm/>" {
		t.Errorf("extracted %q", b)
	}
	if _, err := extractXML([]byte("not a zip")); err == nil {
		t.Error("expected error for non-zip")
	}
	noXML := zipOf(t, "readme.txt", []byte("hi"))
	if _, err := extractXML(noXML); err == nil {
		t.Error("expected error for zip without xml")
	}
}

func TestImportTitle_badXML(t *testing.T) {
	zb := zipOf(t, "usc01.xml", []byte("<not-uslm/>"))
	mux := http.NewServeMux()
	mux.HandleFunc("/119/4/xml_usc01@119-4.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(zb)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &client{cfg: Config{BaseURL: srv.URL + "/119/4", Release: "119-4", UA: "x", Client: srv.Client(), Now: time.Now()}, limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	if err := c.importTitle(context.Background(), st, 1); err == nil {
		t.Error("expected parse error for non-uslm xml")
	}
}

func TestImportTitle_badZip(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/119/4/xml_usc01@119-4.zip", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "not a zip")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &client{cfg: Config{BaseURL: srv.URL + "/119/4", Release: "119-4", UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	if err := c.importTitle(context.Background(), st, 1); err == nil {
		t.Error("expected zip extract error")
	}
}
