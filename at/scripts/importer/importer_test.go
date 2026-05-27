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
	"github.com/tggo/lex/internal/store"
)

const sampleGN = "10007061"

// fixtureServer serves the ris package's committed fixtures at the real RIS OGD
// API shape, so the importer runs end-to-end without the network. The list
// JSON's absolute content URLs (www.ris.bka.gv.at/...) are rewritten to point at
// this test server, and the matching content XML is served at those paths.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "ris", "testdata")
	read := func(file string) []byte {
		b, err := os.ReadFile(filepath.Join(fxDir, file))
		if err != nil {
			t.Fatalf("read fixture %s: %v", file, err)
		}
		return b
	}

	mux := http.NewServeMux()

	// Content XML for each § document, at the rewritten paths.
	xmlByNOR := map[string]string{
		"NOR11007174": "norm_head.sample.xml",
		"NOR12076986": "norm_p1.sample.xml",
		"NOR12076987": "norm_p2.sample.xml",
	}
	for nor, fx := range xmlByNOR {
		body := read(fx)
		mux.HandleFunc("/xml/"+nor, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(body)
		})
	}

	srv := httptest.NewServer(mux)

	// Rewrite the list fixture's absolute XML content URLs to the test server.
	list := string(read("law_list.sample.json"))
	for nor := range xmlByNOR {
		old := "https://www.ris.bka.gv.at/Dokumente/Bundesnormen/" + nor + "/" + nor + ".xml"
		list = strings.ReplaceAll(list, old, srv.URL+"/xml/"+nor)
	}
	mux.HandleFunc("/Bundesrecht", func(w http.ResponseWriter, r *http.Request) {
		// Page 1 returns the law's documents; page 2+ returns empty so paging stops.
		if r.URL.Query().Get("Seitennummer") != "1" {
			_, _ = w.Write([]byte(`{"OgdSearchResult":{"OgdDocumentResults":{"Hits":{"#text":"0"}}}}`))
			return
		}
		_, _ = w.Write([]byte(list))
	})
	return srv
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:        srv.URL + "/Bundesrecht",
		OutDir:         filepath.Join(t.TempDir(), "graph"),
		UA:             "lex-test",
		Client:         srv.Client(),
		Now:            time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Gesetzesnummer: []string{sampleGN},
		RatePerSec:     0, // no throttling in tests
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

	a, err := st.GetAct(schema.ResourceURI("at", "verordnung", 1990, sampleGN))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != sampleGN {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusRepealed {
		t.Errorf("status = %v, want Repealed", a.Expression.Status)
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
	a, err := st.GetAct(schema.ResourceURI("at", "verordnung", 1990, sampleGN))
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

func TestRun_searchFetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when search fetch 404s")
	}
}

func TestRun_noDocuments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"OgdSearchResult":{"OgdDocumentResults":{"Hits":{"#text":"0"}}}}`))
	}))
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when a law has no documents")
	}
}

func TestImportLaw_articleFetchError(t *testing.T) {
	// Search returns valid docs, but their content XML 404s → WithArticles errors.
	fxDir := filepath.Join("..", "ris", "testdata")
	listBytes, err := os.ReadFile(filepath.Join(fxDir, "law_list.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Rewrite article XML URLs to a path on this server that 404s.
	list := string(listBytes)
	for _, nor := range []string{"NOR12076986", "NOR12076987"} {
		old := "https://www.ris.bka.gv.at/Dokumente/Bundesnormen/" + nor + "/" + nor + ".xml"
		list = strings.ReplaceAll(list, old, srv.URL+"/dead/"+nor)
	}
	mux.HandleFunc("/Bundesrecht", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("Seitennummer") != "1" {
			_, _ = w.Write([]byte(`{"OgdSearchResult":{"OgdDocumentResults":{"Hits":{"#text":"0"}}}}`))
			return
		}
		_, _ = w.Write([]byte(list))
	})
	mux.HandleFunc("/dead/", http.NotFoundHandler().ServeHTTP)

	c := &client{cfg: Config{BaseURL: srv.URL + "/Bundesrecht", UA: "x",
		Client: srv.Client(), Now: time.Now(), WithArticles: true}, limiter: newLimiter(0)}
	st, err := store.Open(filepath.Join(t.TempDir(), "g"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := c.importLaw(context.Background(), st, sampleGN); err == nil {
		t.Error("expected article XML fetch error")
	}
}

func TestFetch_retryThenSuccess(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := &client{cfg: Config{UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	b, err := c.fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(b) != "ok" {
		t.Errorf("body = %q", b)
	}
	if calls < 2 {
		t.Errorf("calls = %d, want a retry", calls)
	}
}

func TestFetch_429Retryable(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	c := &client{cfg: Config{UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	if _, err := c.fetch(context.Background(), srv.URL); err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetch_nonRetryableStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := &client{cfg: Config{UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	if _, err := c.fetch(context.Background(), srv.URL); err == nil {
		t.Error("expected error for 404")
	}
}

func TestFetch_contextCancelDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	c := &client{cfg: Config{UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	if _, err := c.fetch(ctx, srv.URL); err == nil {
		t.Error("expected error when context cancelled during backoff")
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
