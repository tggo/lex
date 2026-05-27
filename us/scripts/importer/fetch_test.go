package importer

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
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
			w.WriteHeader(http.StatusServiceUnavailable)
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
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()

	if _, err := testClient(t, srv).fetch(ctx, srv.URL); err == nil {
		t.Error("expected error when context cancelled during backoff")
	}
}

// TestFetch_retriesTransportError ensures a transport-level failure (e.g. a
// dial / i/o timeout, which is what an unreachable OLRC host produces) is
// retried with backoff and can still succeed once the server recovers.
func TestFetch_retriesTransportError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// Wrap the server transport so the first Do fails like a dial timeout.
	base := srv.Client().Transport
	c := &client{
		cfg: Config{
			BaseURL: srv.URL, UA: "x",
			Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if atomic.AddInt32(&calls, 1) == 1 {
					return nil, &net.OpError{Op: "dial", Err: &timeoutErr{}}
				}
				return base.RoundTrip(req)
			})},
		},
		limiter: newLimiter(0),
	}

	b, err := c.fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if string(b) != "ok" {
		t.Errorf("body = %q, want ok", b)
	}
	if calls < 2 {
		t.Errorf("calls = %d, want a retry after transport error", calls)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

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
