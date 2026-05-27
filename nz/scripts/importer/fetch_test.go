package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tggo/lex/nz/scripts/lenz"
)

func testClient(t *testing.T, srv *httptest.Server) *client {
	return &client{
		cfg:     Config{BaseURL: srv.URL, UA: "x", Client: srv.Client()},
		limiter: newLimiter(0),
	}
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

func TestImportAct_parseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<not-valid-xml"))
	}))
	defer srv.Close()
	c := testClient(t, srv)
	if err := c.importAct(context.Background(), nil, lenz.ListItem{Category: "public", Year: 2020, Number: "0038"}); err == nil {
		t.Error("expected parse error on malformed XML")
	}
}

func TestImportAct_fetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	c := testClient(t, srv)
	if err := c.importAct(context.Background(), nil, lenz.ListItem{Category: "public", Year: 2020, Number: "0038"}); err == nil {
		t.Error("expected fetch error on 404")
	}
}

func TestActURL(t *testing.T) {
	c := &client{cfg: Config{BaseURL: "https://x"}}
	got := c.actURL(lenz.ListItem{Year: 2020, Number: "0038"}) // empty category → public
	want := "https://x/act/public/2020/0038/latest/whole.xml"
	if got != want {
		t.Errorf("actURL = %q, want %q", got, want)
	}
}
