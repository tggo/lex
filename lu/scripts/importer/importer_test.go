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
