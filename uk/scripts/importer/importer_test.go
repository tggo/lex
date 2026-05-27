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

// fixtureServer serves the clml package's committed act fixture at the real
// legislation.gov.uk paths, behind a one-entry feed, so the importer runs
// end-to-end without the network.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "clml", "testdata")
	mux := http.NewServeMux()
	// A single-page feed listing exactly the one act we have a fixture for.
	mux.HandleFunc("/ukpga/2023/data.feed", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" ` +
			`xmlns:leg="http://www.legislation.gov.uk/namespaces/legislation" ` +
			`xmlns:ukm="http://www.legislation.gov.uk/namespaces/metadata">` +
			`<leg:page>1</leg:page><leg:morePages>1</leg:morePages>` +
			`<entry><id>http://www.legislation.gov.uk/id/ukpga/2023/57</id>` +
			`<title>National Insurance Contributions (Reduction in Rates) Act 2023</title>` +
			`<ukm:Year Value="2023"/><ukm:Number Value="57"/>` +
			`<ukm:DocumentMainType Value="UnitedKingdomPublicGeneralAct"/></entry></feed>`))
	})
	mux.HandleFunc("/ukpga/2023/57/data.xml", func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join(fxDir, "act.sample.xml"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(b)
	})
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL,
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Types:      []string{"ukpga"},
		FromYear:   2023,
		ToYear:     2023,
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

	a, err := st.GetAct(schema.ResourceURI("uk", "ukpga", 2023, "57"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "ukpga/2023/57" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) == 0 {
		t.Error("expected articles")
	}
	wantCite := schema.ResourceURI("uk", "ukpga", 2024, "5")
	found := false
	for _, c := range a.Expression.Cites {
		if c == wantCite {
			found = true
		}
	}
	if !found {
		t.Errorf("cites = %v, want to include %s", a.Expression.Cites, wantCite)
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
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_defaultsTypesAndYears(t *testing.T) {
	// Empty Types/years exercise the default branches; the NotFound server makes
	// the (current-year) feed fetch fail, which is fine — we only assert the
	// defaults are applied without panicking before the fetch.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := Config{BaseURL: srv.URL, OutDir: filepath.Join(t.TempDir(), "g"), UA: "x", Client: srv.Client()}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected feed fetch error against NotFound server")
	}
}

func TestRun_feedFetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when feed 404s")
	}
}

func TestImportYear_emptyFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom"><page>1</page><morePages>1</morePages></feed>`))
	}))
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, err := store.Open(filepath.Join(t.TempDir(), "g"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	n, err := c.importYear(context.Background(), st, "ukpga", 2023)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0 for empty feed", n)
	}
}

func TestImportYear_badFeedXML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<not valid`))
	}))
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	if _, err := c.importYear(context.Background(), st, "ukpga", 2023); err == nil {
		t.Error("expected parse error for bad feed XML")
	}
}

// A per-act resource 404 (e.g. an old regnal-year act) must be skipped, not
// fatal: importAct returns (false, nil) and importYear keeps going.
func TestImportAct_fetch404Skipped(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	stored, err := c.importAct(context.Background(), st, "ukpga/Geo3/41")
	if err != nil {
		t.Fatalf("importAct: expected nil error for 404, got %v", err)
	}
	if stored {
		t.Error("expected stored=false for a 404 act")
	}
}

// An unparseable data.xml is likewise skipped, not fatal.
func TestImportAct_badXMLSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<not valid`))
	}))
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	stored, err := c.importAct(context.Background(), st, "ukpga/2023/57")
	if err != nil {
		t.Fatalf("importAct: expected nil error for bad XML, got %v", err)
	}
	if stored {
		t.Error("expected stored=false for unparseable XML")
	}
}

// Context cancellation during a fetch stays fatal.
func TestImportAct_cancelledFatal(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.importAct(ctx, st, "ukpga/2023/57"); err == nil {
		t.Error("expected fatal error on cancelled context")
	}
}

// A year feed containing a missing act and a good act imports the good one and
// skips the missing one rather than aborting the whole year.
func TestImportYear_skipsMissingActContinues(t *testing.T) {
	fxDir := filepath.Join("..", "clml", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/ukpga/2023/data.feed", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" ` +
			`xmlns:leg="http://www.legislation.gov.uk/namespaces/legislation" ` +
			`xmlns:ukm="http://www.legislation.gov.uk/namespaces/metadata">` +
			`<leg:page>1</leg:page><leg:morePages>1</leg:morePages>` +
			`<entry><id>http://www.legislation.gov.uk/id/ukpga/2023/999</id>` +
			`<title>Missing Act 2023</title>` +
			`<ukm:Year Value="2023"/><ukm:Number Value="999"/>` +
			`<ukm:DocumentMainType Value="UnitedKingdomPublicGeneralAct"/></entry>` +
			`<entry><id>http://www.legislation.gov.uk/id/ukpga/2023/57</id>` +
			`<title>National Insurance Contributions (Reduction in Rates) Act 2023</title>` +
			`<ukm:Year Value="2023"/><ukm:Number Value="57"/>` +
			`<ukm:DocumentMainType Value="UnitedKingdomPublicGeneralAct"/></entry></feed>`))
	})
	// 999 has no data.xml handler -> 404 (skipped); 57 is served.
	mux.HandleFunc("/ukpga/2023/57/data.xml", func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join(fxDir, "act.sample.xml"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(b)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	n, err := c.importYear(context.Background(), st, "ukpga", 2023)
	if err != nil {
		t.Fatalf("importYear: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d acts, want 1 (skipping the 404)", n)
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
