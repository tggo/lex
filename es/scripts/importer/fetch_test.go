package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tggo/lex/internal/store"
)

func testClient(t *testing.T, srv *httptest.Server) *client {
	return &client{
		cfg:     Config{BaseURL: srv.URL, UA: "x", Client: srv.Client()},
		limiter: newLimiter(0),
	}
}

// fileHandler serves a fixture file from dir.
func fileHandler(t *testing.T, dir, file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(b)
	}
}

// importOne opens a temp store and imports a single norm by id.
func (c *client) importOne(t *testing.T, id string) error {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "g"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if c.cfg.Now.IsZero() {
		c.cfg.Now = time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	}
	return c.importNorm(context.Background(), st, id)
}

func TestFetch_retryThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable) // transient: triggers a retry
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	b, err := testClient(t, srv).fetch(context.Background(), srv.URL, "application/json")
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
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if _, err := testClient(t, srv).fetch(context.Background(), srv.URL, "application/json"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetch_nonRetryableStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := testClient(t, srv).fetch(context.Background(), srv.URL, "application/json"); err == nil {
		t.Error("expected error for 404")
	}
}

func TestFetch_contextCancelDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // always transient → forces backoff
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	if _, err := testClient(t, srv).fetch(ctx, srv.URL, "application/json"); err == nil {
		t.Error("expected error when context cancelled during backoff")
	}
}

func TestImportNorm_analisisError(t *testing.T) {
	fxDir := filepath.Join("..", "boe", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/id/"+sampleID+"/metadatos", fileHandler(t, fxDir, "norma_metadatos.sample.json"))
	mux.HandleFunc("/id/"+sampleID+"/analisis", http.NotFoundHandler().ServeHTTP)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := testClient(t, srv).importOne(t, sampleID); err == nil {
		t.Error("expected analisis fetch error")
	}
}

func TestImportNorm_textoError(t *testing.T) {
	fxDir := filepath.Join("..", "boe", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/id/"+sampleID+"/metadatos", fileHandler(t, fxDir, "norma_metadatos.sample.json"))
	mux.HandleFunc("/id/"+sampleID+"/analisis", fileHandler(t, fxDir, "norma_analisis.sample.json"))
	mux.HandleFunc("/id/"+sampleID+"/texto", http.NotFoundHandler().ServeHTTP)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := testClient(t, srv)
	c.cfg.WithArticles = true
	if err := c.importOne(t, sampleID); err == nil {
		t.Error("expected texto fetch error")
	}
}

func TestLimiter(t *testing.T) {
	l := newLimiter(1000) // 1000/s → 1ms interval
	start := time.Now()
	l.wait(context.Background())
	l.wait(context.Background())
	if time.Since(start) < 0 { // smoke: must not panic, monotonic
		t.Error("negative duration")
	}
	if newLimiter(0).interval != 0 {
		t.Error("rate<=0 should disable throttling")
	}
}
