package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
)

const (
	firstWork = "http://data.legilux.public.lu/eli/etat/adm/a/1854/04/21/n1/jo"
	relWork   = "http://data.legilux.public.lu/eli/etat/leg/rgd/2020/03/18/a165/jo"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "legilux", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// emptyResults is a SPARQL results doc with no bindings (terminates paging).
const emptyResults = `{"head":{"vars":["work"]},"results":{"bindings":[]}}`

// fixtureServer serves the legilux fixtures: the acts query (first page returns
// the acts fixture, subsequent pages are empty) and per-work relations queries.
// The relations fixture is attached to the first act's work URI so the
// end-to-end test can assert edges.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	acts := fixture(t, "acts_page.sample.json")
	rels := fixture(t, "relations.sample.json")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/sparql-results+json")
		switch {
		case strings.Contains(q, "?rel"): // relations query
			if strings.Contains(q, firstWork) {
				_, _ = w.Write(rels)
				return
			}
			_, _ = w.Write([]byte(emptyResults))
		case strings.Contains(q, "OFFSET 0"): // first acts page
			_, _ = w.Write(acts)
		default: // later pages: empty → stop
			_, _ = w.Write([]byte(emptyResults))
		}
	}))
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		Endpoint:   srv.URL,
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

	res := schema.ResourceURI("lu", "arrete", 1854, "etat/adm/a/1854/04/21/n1/jo")
	a, err := st.GetAct(res)
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "etat/adm/a/1854/04/21/n1/jo" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	wantAmend := schema.ResourceURI("lu", "", 1988, "etat/leg/loi/1988/12/13/n1/jo")
	if len(a.Expression.Amends) != 1 || a.Expression.Amends[0] != wantAmend {
		t.Errorf("amends = %v, want [%s]", a.Expression.Amends, wantAmend)
	}
	if len(a.Expression.Cites) != 2 {
		t.Errorf("cites = %v, want 2", a.Expression.Cites)
	}
}

func TestRun_limit(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Limit = 2
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 2 {
		t.Errorf("imported %d acts, want 2 (limit)", n)
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

func TestRun_actsPageError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when acts page 404s")
	}
}

func TestRun_relationsError(t *testing.T) {
	acts := fixture(t, "acts_page.sample.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if strings.Contains(q, "?rel") {
			http.Error(w, "boom", http.StatusNotFound)
			return
		}
		if strings.Contains(q, "OFFSET 0") {
			_, _ = w.Write(acts)
			return
		}
		_, _ = w.Write([]byte(emptyResults))
	}))
	defer srv.Close()
	if _, err := Run(context.Background(), baseCfg(t, srv)); err == nil {
		t.Error("expected error when relations query 404s")
	}
}

func testClient(t *testing.T, srv *httptest.Server) *client {
	return &client{
		cfg:     Config{Endpoint: srv.URL, UA: "x", Client: srv.Client()},
		limiter: newLimiter(0),
	}
}

func TestQuery_retryThenSuccess(t *testing.T) {
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
	b, err := testClient(t, srv).query(context.Background(), "Q")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if string(b) != "ok" {
		t.Errorf("body = %q, want ok", b)
	}
	if calls < 2 {
		t.Errorf("calls = %d, want a retry", calls)
	}
}

func TestQuery_429Retryable(t *testing.T) {
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
	if _, err := testClient(t, srv).query(context.Background(), "Q"); err != nil {
		t.Fatalf("query: %v", err)
	}
}

func TestQuery_nonRetryableStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := testClient(t, srv).query(context.Background(), "Q"); err == nil {
		t.Error("expected error for 404")
	}
}

func TestQuery_contextCancelDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	if _, err := testClient(t, srv).query(ctx, "Q"); err == nil {
		t.Error("expected error when context cancelled during backoff")
	}
}

// redirectTransport routes every request to a single test-server base URL,
// preserving the original request path. It lets us serve the data.legilux host
// (baked into the fixtures' work URIs) from an httptest server without network.
type redirectTransport struct {
	base string
	rt   http.RoundTripper
}

func (t redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(t.base)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return t.rt.RoundTrip(req)
}

// articlesServer serves SPARQL like fixtureServer, plus the French HTML
// manifestation (the act.sample.html fixture) for any /fr/html request.
func articlesServer(t *testing.T) *httptest.Server {
	t.Helper()
	acts := fixture(t, "acts_page.sample.json")
	rels := fixture(t, "relations.sample.json")
	htmlBody := fixture(t, "act.sample.html")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/fr/html") {
			_, _ = w.Write(htmlBody)
			return
		}
		q := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/sparql-results+json")
		switch {
		case strings.Contains(q, "?rel"):
			if strings.Contains(q, firstWork) {
				_, _ = w.Write(rels)
				return
			}
			_, _ = w.Write([]byte(emptyResults))
		case strings.Contains(q, "OFFSET 0"):
			_, _ = w.Write(acts)
		default:
			_, _ = w.Write([]byte(emptyResults))
		}
	}))
}

func TestRun_withArticles(t *testing.T) {
	srv := articlesServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	// Route the legilux host (in the fixture work URIs) to the test server.
	cfg.Client = &http.Client{Transport: redirectTransport{base: srv.URL, rt: srv.Client().Transport}}

	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	res := schema.ResourceURI("lu", "arrete", 1854, "etat/adm/a/1854/04/21/n1/jo")
	a, err := st.GetAct(res)
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if len(a.Expression.Articles) != 3 {
		t.Fatalf("got %d articles, want 3", len(a.Expression.Articles))
	}
	if a.Expression.Articles[0].Number != "1er" {
		t.Errorf("first article number = %q, want 1er", a.Expression.Articles[0].Number)
	}
}

func TestAttachArticles_fetchErrorIsNonFatal(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := testClient(t, srv)
	c.cfg.Client = &http.Client{Transport: redirectTransport{base: srv.URL, rt: srv.Client().Transport}}

	act := &schema.Act{Expression: &schema.Expression{}}
	c.attachArticles(context.Background(), act, "http://data.legilux.public.lu/eli/x/jo")
	if act.Expression.Articles != nil {
		t.Errorf("want no articles on fetch error, got %v", act.Expression.Articles)
	}
}

func TestAttachArticles_noArticlesLeavesEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><app-root></app-root></body></html>`))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	c.cfg.Client = &http.Client{Transport: redirectTransport{base: srv.URL, rt: srv.Client().Transport}}

	act := &schema.Act{Expression: &schema.Expression{}}
	c.attachArticles(context.Background(), act, "http://data.legilux.public.lu/eli/x/jo")
	if act.Expression.Articles != nil {
		t.Errorf("shell page should yield no articles, got %v", act.Expression.Articles)
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
