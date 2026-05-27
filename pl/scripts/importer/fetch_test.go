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
	"github.com/tggo/lex/pl/scripts/eli"
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

// importOne opens a temp store and imports a single act by ELI.
func (c *client) importOne(t *testing.T, eliID string) error {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "g"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if c.cfg.Now.IsZero() {
		c.cfg.Now = time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)
	}
	return c.importAct(context.Background(), st, eli.ListItem{ELI: eliID, TextHTML: true})
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

	b, err := testClient(t, srv).fetch(context.Background(), srv.URL)
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
	if _, err := testClient(t, srv).fetch(context.Background(), srv.URL); err != nil {
		t.Fatalf("fetch: %v", err)
	}
}

func TestFetch_nonRetryableStatus(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := testClient(t, srv).fetch(context.Background(), srv.URL); err == nil {
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

	if _, err := testClient(t, srv).fetch(ctx, srv.URL); err == nil {
		t.Error("expected error when context cancelled during backoff")
	}
}

func TestImportAct_referencesError(t *testing.T) {
	fxDir := filepath.Join("..", "eli", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/DU/2023/2777", fileHandler(t, fxDir, "act_detail.sample.json"))
	mux.HandleFunc("/DU/2023/2777/references", http.NotFoundHandler().ServeHTTP)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if err := testClient(t, srv).importOne(t, "DU/2023/2777"); err == nil {
		t.Error("expected references fetch error")
	}
}

func TestImportAct_structError(t *testing.T) {
	fxDir := filepath.Join("..", "eli", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/DU/2023/2777", fileHandler(t, fxDir, "act_detail.sample.json"))
	mux.HandleFunc("/DU/2023/2777/references", fileHandler(t, fxDir, "act_references.sample.json"))
	mux.HandleFunc("/DU/2023/2777/struct", http.NotFoundHandler().ServeHTTP)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := testClient(t, srv)
	c.cfg.WithArticles = true
	if err := c.importOne(t, "DU/2023/2777"); err == nil {
		t.Error("expected struct fetch error")
	}
}
