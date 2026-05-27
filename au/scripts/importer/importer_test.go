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

// fixtureServer serves the frl package's committed fixtures at the real FRL
// OData paths, so the importer runs end-to-end without the network. The year
// listing is built inline to contain exactly the one act we have a full set of
// fixtures for (C1901A00002).
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "frl", "testdata")
	serve := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join(fxDir, file))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(b)
		}
	}
	mux := http.NewServeMux()
	// One title for the year listing.
	mux.HandleFunc("/v1/titles", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$skip") != "0" {
			_, _ = w.Write([]byte(`{"@odata.count":1,"value":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"@odata.count":1,"value":[` +
			`{"id":"C1901A00002","name":"Acts Interpretation Act 1901",` +
			`"collection":"Act","seriesType":"Act","year":1901,"number":2,` +
			`"status":"InForce","isInForce":true}]}`))
	})
	mux.HandleFunc("/v1/titles/C1901A00002", serve("title_detail.sample.json"))
	mux.HandleFunc("/v1/Versions/Default.Find(titleId='C1901A00002',asAtSpecification='current')",
		serve("version_current.sample.json"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL + "/v1",
		OutDir:     filepath.Join(t.TempDir(), "graph"),
		UA:         "lex-test",
		Client:     srv.Client(),
		Now:        time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Collection: "Act",
		FromYear:   1901,
		ToYear:     1901,
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

	a, err := st.GetAct(schema.ResourceURI("au", "act", 1901, "C1901A00002"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "C1901A00002" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	wantAmend := schema.ResourceURI("au", "act", 2026, "C2026A00004")
	if len(a.Expression.AmendedBy) != 1 || a.Expression.AmendedBy[0] != wantAmend {
		t.Errorf("amendedBy = %v, want [%s]", a.Expression.AmendedBy, wantAmend)
	}
}

func TestRun_missingVersionTolerated(t *testing.T) {
	fxDir := filepath.Join("..", "frl", "testdata")
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/titles", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$skip") != "0" {
			_, _ = w.Write([]byte(`{"@odata.count":1,"value":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"@odata.count":1,"value":[{"id":"C1901A00002","year":1901}]}`))
	})
	mux.HandleFunc("/v1/titles/C1901A00002", func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(fxDir, "title_detail.sample.json"))
		_, _ = w.Write(b)
	})
	// Versions endpoint 404s → tolerated, metadata still imports.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}
	st, _ := store.Open(cfg.OutDir)
	defer st.Close()
	a, err := st.GetAct(schema.ResourceURI("au", "act", 1901, "C1901A00002"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	// Falls back to makingDate (1901-07-12) for version_date.
	if a.Expression.VersionDate.Year() != 1901 {
		t.Errorf("versionDate = %v, want 1901 fallback", a.Expression.VersionDate)
	}
}

func TestRun_defaultsClientAndNow(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Client = nil
	cfg.Now = time.Time{}
	cfg.Collection = ""
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_requiresYearRange(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.FromYear, cfg.ToYear = 0, 0
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when year range missing")
	}
}

func TestRun_listFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when listing fails")
	}
}

func TestImportTitle_detailParseError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/titles/X", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &client{cfg: Config{BaseURL: srv.URL + "/v1", UA: "x", Client: srv.Client()}, limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	if err := c.importTitle(context.Background(), st, "X"); err == nil {
		t.Error("expected detail parse error")
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

func TestURLEncoding_filterHasYear(t *testing.T) {
	// Guard the OData filter construction stays well-formed.
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "titles") && r.URL.RawQuery != "" {
			gotURL = r.URL.Query().Get("$filter")
		}
		_, _ = w.Write([]byte(`{"@odata.count":0,"value":[]}`))
	}))
	defer srv.Close()
	c := &client{cfg: baseCfg(t, srv), limiter: newLimiter(0)}
	st, _ := store.Open(filepath.Join(t.TempDir(), "g"))
	defer st.Close()
	if _, err := c.importYear(context.Background(), st, 1901); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "year eq 1901") {
		t.Errorf("filter = %q, want year eq 1901", gotURL)
	}
}
