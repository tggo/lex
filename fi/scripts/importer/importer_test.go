package importer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
)

// fixtureServer serves the akn package's committed fixtures at the real Finlex
// API paths, so the importer runs end-to-end without the network. The list
// returns both fixture acts; the full-expression endpoints serve the
// single-act documents (which carry the body sections).
func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	fxDir := filepath.Join("..", "akn", "testdata")
	serve := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, err := os.ReadFile(filepath.Join(fxDir, file))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write(b)
		}
	}
	base := "/finlex/avoindata/v1/" + collection
	mux := http.NewServeMux()
	mux.HandleFunc(base, serve("list.sample.xml"))
	mux.HandleFunc(base+"/2019/469/fin@", serve("act.sample.xml"))
	mux.HandleFunc(base+"/2025/51/fin@", serve("act_decree.sample.xml"))
	return httptest.NewServer(mux)
}

func baseCfg(t *testing.T, srv *httptest.Server) Config {
	return Config{
		BaseURL:    srv.URL + "/finlex/avoindata/v1",
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
	cfg.WithArticles = true

	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 2 {
		t.Fatalf("imported %d acts, want 2", n)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("fi", "laki", 2019, "2019/469"))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != "http://data.finlex.fi/eli/sd/2019/469/ajantasa" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 15 {
		t.Errorf("articles = %d, want 15", len(a.Expression.Articles))
	}
	wantAmend := schema.ResourceURI("fi", "statute", 2022, "2022/1099")
	if len(a.Expression.AmendedBy) != 1 || a.Expression.AmendedBy[0] != wantAmend {
		t.Errorf("amendedBy = %v, want [%s]", a.Expression.AmendedBy, wantAmend)
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
	a, err := st.GetAct(schema.ResourceURI("fi", "laki", 2019, "2019/469"))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Expression.Articles) != 0 {
		t.Errorf("articles = %d, want 0 (articles disabled)", len(a.Expression.Articles))
	}
}

// listDoc renders one minimal list-envelope <akomaNtoso> document with a
// distinct work identity, enough for ParseList/ToAct to accept it.
func listDoc(year int, number string) string {
	return fmt.Sprintf(`<akomaNtoso xmlns="http://docs.oasis-open.org/legaldocml/ns/akn/3.0" xmlns:finlex="http://data.finlex.fi/schema/finlex">
<act contains="multipleVersions" name="main"><meta>
<identification source="#org"><FRBRWork>
<FRBRuri value="/akn/fi/act/statute-consolidated/%d/%s"/>
<FRBRalias name="eli" value="http://data.finlex.fi/eli/sd/%d/%s/ajantasa"/>
<FRBRcountry value="fi"/><FRBRsubtype value="statute-consolidated"/><FRBRnumber value="%s"/>
</FRBRWork><FRBRExpression>
<FRBRuri value="/akn/fi/act/statute-consolidated/%d/%s/fin@"/>
<FRBRdate date="%d-01-01" name="dateConsolidated"/><FRBRlanguage language="fin"/>
</FRBRExpression></identification>
<proprietary source="#org"><finlex:documentYear>%d</finlex:documentYear>
<finlex:isInForce value="true"/></proprietary></meta>
<preface><p><docNumber>%s/%d</docNumber><docTitle>Test act %d/%s</docTitle></p></preface>
</act></akomaNtoso>`, year, number, year, number, number, year, number, year, year, number, year, year, number)
}

func listPage(docs ...string) string {
	return "<AknXmlList><Results>" + strings.Join(docs, "") + "</Results></AknXmlList>"
}

// TestRun_pagesTerminate proves the paging loop walks each page exactly once
// and STOPS when the source is exhausted. It is the regression guard for the
// infinite-loop bug: the Finlex API ignores "offset" and pages by 1-based
// "page", so a server that always returns the same window unless "page"
// advances would loop forever under the old code.
func TestRun_pagesTerminate(t *testing.T) {
	// 3 full pages (pageLimit docs each) then a short final page, then empty.
	pages := map[int]string{}
	num := 0
	for p := 1; p <= 3; p++ {
		var ds []string
		for i := 0; i < pageLimit; i++ {
			num++
			ds = append(ds, listDoc(2000, strconv.Itoa(num)))
		}
		pages[p] = listPage(ds...)
	}
	num++
	pages[4] = listPage(listDoc(2000, strconv.Itoa(num))) // short last page (1 doc)
	// page 5+ -> empty results (out-of-range), like the real API.

	var mu sync.Mutex
	seen := map[string]int{} // requested page param -> count

	base := "/finlex/avoindata/v1/" + collection
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		pg := r.URL.Query().Get("page")
		mu.Lock()
		seen[pg]++
		mu.Unlock()
		if r.URL.Query().Get("offset") != "" {
			t.Errorf("importer sent offset=%q; must page by 'page'", r.URL.Query().Get("offset"))
		}
		n, _ := strconv.Atoi(pg)
		w.Header().Set("Content-Type", "application/xml")
		if body, ok := pages[n]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(listPage())) // empty <Results/>
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv) // WithArticles=false: list endpoint only
	got, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantTotal := 3*pageLimit + 1
	if got != wantTotal {
		t.Errorf("imported %d acts, want %d", got, wantTotal)
	}

	// Each page requested exactly once; no page re-requested (no loop).
	for pg, cnt := range seen {
		if cnt != 1 {
			t.Errorf("page=%q requested %d times, want exactly 1 (re-fetch = loop)", pg, cnt)
		}
	}
	// Walked pages 1..4 (the short page is the last full request); a 4-doc
	// short page < pageLimit stops the loop, so page 5 must NOT be requested.
	var visited []int
	for pg := range seen {
		n, _ := strconv.Atoi(pg)
		visited = append(visited, n)
	}
	sort.Ints(visited)
	want := []int{1, 2, 3, 4}
	if fmt.Sprint(visited) != fmt.Sprint(want) {
		t.Errorf("visited pages %v, want %v (short page must stop, no extra request)", visited, want)
	}
}

// TestRun_emptyFinalPage covers the path where the last page is exactly
// pageLimit long and termination relies on a following EMPTY page (len==0),
// not on a short page.
func TestRun_emptyFinalPage(t *testing.T) {
	pages := map[int]string{}
	num := 0
	for p := 1; p <= 2; p++ {
		var ds []string
		for i := 0; i < pageLimit; i++ {
			num++
			ds = append(ds, listDoc(2001, strconv.Itoa(num)))
		}
		pages[p] = listPage(ds...)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	base := "/finlex/avoindata/v1/" + collection
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		pg := r.URL.Query().Get("page")
		mu.Lock()
		seen[pg]++
		mu.Unlock()
		n, _ := strconv.Atoi(pg)
		w.Header().Set("Content-Type", "application/xml")
		if body, ok := pages[n]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		_, _ = w.Write([]byte(listPage())) // page 3 -> empty
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	got, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 2*pageLimit {
		t.Errorf("imported %d acts, want %d", got, 2*pageLimit)
	}
	// Must request the empty page 3 once to learn the source is exhausted,
	// then stop. Each page exactly once.
	for pg, cnt := range seen {
		if cnt != 1 {
			t.Errorf("page=%q requested %d times, want 1", pg, cnt)
		}
	}
	if seen["3"] != 1 {
		t.Errorf("expected one request for empty page 3 to detect exhaustion, got %d", seen["3"])
	}
	if _, asked := seen["4"]; asked {
		t.Error("page 4 must not be requested after empty page 3")
	}
}

func TestRun_limit(t *testing.T) {
	srv := fixtureServer(t)
	defer srv.Close()
	cfg := baseCfg(t, srv)
	cfg.Limit = 1
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Errorf("imported %d acts, want 1 (limit)", n)
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

func TestRun_listFetchError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t, srv)
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when list fetch 404s")
	}
}

// A full-expression document that 404s is a per-act source quirk: the act is
// logged and skipped, not fatal. Mirrors ie's TestImportYear_actFetchSkipped.
func TestRun_fullFetchSkipped(t *testing.T) {
	fxDir := filepath.Join("..", "akn", "testdata")
	base := "/finlex/avoindata/v1/" + collection
	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(fxDir, "list.sample.xml"))
		_, _ = w.Write(b)
	})
	// full-expression endpoints intentionally 404.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run should tolerate a 404 full-expression fetch, got: %v", err)
	}
	if n != 0 {
		t.Errorf("imported %d acts, want 0 (every full-expression fetch 404s)", n)
	}
}

// A mix of one healthy act and one 404 act: the healthy act is stored and the
// 404 act is skipped, so the run completes with a partial count.
func TestRun_partialFullFetchSkipped(t *testing.T) {
	fxDir := filepath.Join("..", "akn", "testdata")
	base := "/finlex/avoindata/v1/" + collection
	serve := func(file string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := os.ReadFile(filepath.Join(fxDir, file))
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write(b)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(base, serve("list.sample.xml"))
	mux.HandleFunc(base+"/2019/469/fin@", serve("act.sample.xml"))
	// /2025/51/fin@ intentionally left to 404 (NotFound default).
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := baseCfg(t, srv)
	cfg.WithArticles = true
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run should skip the 404 act and continue, got: %v", err)
	}
	if n != 1 {
		t.Errorf("imported %d acts, want 1 (one healthy, one 404)", n)
	}
	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.GetAct(schema.ResourceURI("fi", "laki", 2019, "2019/469")); err != nil {
		t.Errorf("healthy act not stored: %v", err)
	}
}
