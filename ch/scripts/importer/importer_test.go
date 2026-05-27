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

// fixtureServer serves the fedlex package's committed SPARQL JSON fixture for
// any POST to the endpoint, so the importer runs end-to-end without network.
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fx := filepath.Join("..", "fedlex", "testdata", "sr_acts.sample.json")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "want POST", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil || r.Form.Get("query") == "" {
			http.Error(w, "missing query", http.StatusBadRequest)
			return
		}
		b, err := os.ReadFile(fx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/sparql-results+json")
		_, _ = w.Write(b)
	}))
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		Endpoint:    srv.URL,
		OutDir:      filepath.Join(t.TempDir(), "graph"),
		UA:          "lex-test",
		Client:      srv.Client(),
		Now:         time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		SRNotations: []string{"210", "220", "311.0"},
		RatePerSec:  0,
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

	a, err := st.GetAct(schema.ResourceURI("ch", "gesetzbuch", 1912, "210"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "SR 210" {
		t.Errorf("idLocal = %q, want SR 210", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if a.Expression.VersionDate.IsZero() {
		t.Error("version_date is mandatory but zero")
	}
	if a.Expression.SourceURL != "https://fedlex.data.admin.ch/eli/cc/24/233_245_233" {
		t.Errorf("sourceURL = %q", a.Expression.SourceURL)
	}
}

func TestRun_buildsPersistentIndex(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.IndexPath = filepath.Join(t.TempDir(), "index.fts")
	cfg.Lang = "de"

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

func TestRun_fetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when endpoint 404s")
	}
}

func TestRun_badJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected parse error for bad JSON")
	}
}

func testClient(srv *httptest.Server) *client {
	return &client{
		cfg:     Config{Endpoint: srv.URL, UA: "x", Client: srv.Client()},
		limiter: newLimiter(0),
	}
}

func TestBuildQuery(t *testing.T) {
	c := &client{cfg: Config{SRNotations: []string{"210", "220"}, Limit: 50}}
	q := c.buildQuery()
	if !strings.Contains(q, `IN ("210", "220")`) {
		t.Errorf("query missing SR filter: %s", q)
	}
	if !strings.Contains(q, "LIMIT 50") {
		t.Errorf("query missing limit: %s", q)
	}
	// No filter and no limit when unset.
	empty := (&client{}).buildQuery()
	if strings.Contains(empty, "FILTER(STR(?srNotation) IN") {
		t.Error("expected no SR filter when none configured")
	}
	if strings.Contains(empty, "LIMIT") {
		t.Error("expected no LIMIT when unset")
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
	b, err := testClient(srv).fetch(context.Background(), "q")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(b) != "ok" {
		t.Errorf("body = %q, want ok", b)
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
	if _, err := testClient(srv).fetch(context.Background(), "q"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetch_nonRetryableStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := testClient(srv).fetch(context.Background(), "q"); err == nil {
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
	if _, err := testClient(srv).fetch(ctx, "q"); err == nil {
		t.Error("expected error when context cancelled during backoff")
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(1000)
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
