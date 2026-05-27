package importer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
	"github.com/tggo/lex/jp/scripts/egov"
)

var fixedTime = time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)

// fakeEgov serves a minimal two-page /laws sequence plus one law_data body,
// standing in for the live e-Gov API.
func fakeEgov(t *testing.T) *httptest.Server {
	t.Helper()
	page0 := `{"count":1,"next_offset":1,"laws":[
	  {"law_info":{"law_type":"Act","law_id":"129AC0000000089","promulgation_date":"1896-04-27"},
	   "revision_info":{"law_revision_id":"REV1","law_title":"民法",
	     "amendment_enforcement_date":"2026-04-01","repeal_status":"None",
	     "amendment_law_id":"105DF0000000337",
	     "current_revision_status":"CurrentEnforced"}}]}`
	page1 := `{"count":2,"next_offset":3,"laws":[
	  {"law_info":{"law_type":"CabinetOrder","law_id":"105DF0000000337","promulgation_date":"1872-11-09"},
	   "revision_info":{"law_revision_id":"REV2","law_title":"改暦ノ布告",
	     "amendment_enforcement_date":"1872-11-09","repeal_status":"None",
	     "current_revision_status":"CurrentEnforced"}},
	  {"law_info":{"law_type":"Act","law_id":"113DF0000000036","promulgation_date":"1880-07-17"},
	   "revision_info":{"law_revision_id":"REV3","law_title":"旧刑法",
	     "amendment_enforcement_date":"1908-10-01","repeal_status":"Repeal",
	     "amendment_law_id":"105DF0000000337",
	     "current_revision_status":"Repeal"}}]}`
	pageEmpty := `{"count":0,"next_offset":3,"laws":[]}`
	lawData := `{"law_full_text":{"tag":"Law","attr":{},"children":[
	  {"tag":"LawBody","attr":{},"children":[
	    {"tag":"MainProvision","attr":{},"children":[
	      {"tag":"Article","attr":{"Num":"1"},"children":[
	        {"tag":"ArticleTitle","attr":{},"children":["第一条"]},
	        {"tag":"Paragraph","attr":{},"children":[
	          {"tag":"Sentence","attr":{},"children":["本文。"]}]}]}]}]}]}}`

	mux := http.NewServeMux()
	mux.HandleFunc("/laws", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("offset") {
		case "0":
			fmt.Fprint(w, page0)
		case "1":
			fmt.Fprint(w, page1)
		default:
			fmt.Fprint(w, pageEmpty)
		}
	})
	mux.HandleFunc("/law_data/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, lawData)
	})
	// Civil Code revision timeline: one past revision (amended by the CabinetOrder,
	// in set) plus the current enforced one (which equals the Expression and must
	// be skipped as a separate revision).
	civilRevisions := `{"law_info":{"law_id":"129AC0000000089"},"revisions":[
	  {"amendment_enforcement_date":"2005-04-01","current_revision_status":"PreviousEnforced",
	   "amendment_law_id":"105DF0000000337"},
	  {"amendment_enforcement_date":"2026-04-01","current_revision_status":"CurrentEnforced",
	   "amendment_law_id":"105DF0000000337"}]}`
	mux.HandleFunc("/law_revisions/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "129AC0000000089") {
			fmt.Fprint(w, civilRevisions)
			return
		}
		fmt.Fprint(w, `{"law_info":{"law_id":"x"},"revisions":[]}`)
	})
	return httptest.NewServer(mux)
}

func TestRun_endToEnd(t *testing.T) {
	srv := fakeEgov(t)
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "graph")
	n, err := Run(context.Background(), Config{
		BaseURL:      srv.URL,
		OutDir:       dir,
		UA:           "lex-test",
		Now:          fixedTime,
		WithArticles: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 3 {
		t.Fatalf("imported %d acts, want 3", n)
	}

	// Reopen the store and confirm the Civil Code round-trips with its article.
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	got, err := st.GetAct("https://lex.dev/eli/jp/act/1896/129AC0000000089")
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if got.Expression.Title != "民法" {
		t.Errorf("title = %q, want 民法", got.Expression.Title)
	}
	if len(got.Expression.Articles) != 1 || got.Expression.Articles[0].Number != "1" {
		t.Fatalf("articles = %+v, want one article #1", got.Expression.Articles)
	}
	if !strings.Contains(got.Expression.Articles[0].Text, "本文。") {
		t.Errorf("article text = %q, want it to contain 本文。", got.Expression.Articles[0].Text)
	}
	// The Civil Code's amendment_law_id points at the CabinetOrder, which is in
	// the listed set, so it must resolve to an eli:amended_by edge.
	wantTarget := "https://lex.dev/eli/jp/cabinet-order/1872/105DF0000000337"
	if len(got.Expression.AmendedBy) != 1 || got.Expression.AmendedBy[0] != wantTarget {
		t.Errorf("amendedBy = %v, want [%s]", got.Expression.AmendedBy, wantTarget)
	}

	// The repealed act resolves its repealing law to an eli:repealed_by edge,
	// and carries no amended_by.
	rep, err := st.GetAct("https://lex.dev/eli/jp/act/1880/113DF0000000036")
	if err != nil {
		t.Fatalf("GetAct repealed: %v", err)
	}
	if rep.Expression.Status != schema.StatusRepealed {
		t.Errorf("status = %v, want Repealed", rep.Expression.Status)
	}
	if len(rep.Expression.RepealedBy) != 1 || rep.Expression.RepealedBy[0] != wantTarget {
		t.Errorf("repealedBy = %v, want [%s]", rep.Expression.RepealedBy, wantTarget)
	}
	if len(rep.Expression.AmendedBy) != 0 {
		t.Errorf("repealed act amendedBy = %v, want empty", rep.Expression.AmendedBy)
	}
}

func TestRun_withRevisions(t *testing.T) {
	srv := fakeEgov(t)
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "graph")
	if _, err := Run(context.Background(), Config{
		BaseURL: srv.URL, OutDir: dir, Now: fixedTime, WithRevisions: true,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	got, err := st.GetAct("https://lex.dev/eli/jp/act/1896/129AC0000000089")
	if err != nil {
		t.Fatal(err)
	}
	// The current 2026-04-01 revision is the Expression; only the 2005 one is a
	// separate revision, with its amending law resolved.
	if len(got.Revisions) != 1 {
		t.Fatalf("revisions = %d, want 1 (current is excluded)", len(got.Revisions))
	}
	rv := got.Revisions[0]
	if !rv.VersionDate.Equal(time.Date(2005, 4, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("revision date = %v, want 2005-04-01", rv.VersionDate)
	}
	wantTarget := "https://lex.dev/eli/jp/cabinet-order/1872/105DF0000000337"
	if len(rv.AmendedBy) != 1 || rv.AmendedBy[0] != wantTarget {
		t.Errorf("revision amendedBy = %v, want [%s]", rv.AmendedBy, wantTarget)
	}
}

func TestResolveAmendments(t *testing.T) {
	rec := func(lawID, slug string, year int) egov.Record {
		return egov.Record{
			Act: &schema.Act{
				Country: "jp", TypeSlug: slug, Year: year, Number: lawID, IDLocal: lawID,
				Expression: &schema.Expression{},
			},
		}
	}
	a := rec("AAA", "act", 2000)
	a.AmendedByLawID = "BBB" // amended by B (in set) -> resolves
	b := rec("BBB", "act", 1990)
	c := rec("CCC", "act", 2010)
	c.AmendedByLawID = "ZZZ" // amending law not in set -> dropped
	d := rec("DDD", "act", 1880)
	d.RepealedByLawID = "BBB" // repealed by B (in set) -> resolves as repealed_by
	recs := []egov.Record{a, b, c, d}

	resolveAmendments(recs, lawIDIndex(recs))

	wantB := "https://lex.dev/eli/jp/act/1990/BBB"
	if got := recs[0].Act.Expression.AmendedBy; len(got) != 1 || got[0] != wantB {
		t.Errorf("A.AmendedBy = %v, want [%s]", got, wantB)
	}
	if got := recs[2].Act.Expression.AmendedBy; len(got) != 0 {
		t.Errorf("C.AmendedBy = %v, want empty (target not in set)", got)
	}
	if got := recs[3].Act.Expression.RepealedBy; len(got) != 1 || got[0] != wantB {
		t.Errorf("D.RepealedBy = %v, want [%s]", got, wantB)
	}
	if got := recs[3].Act.Expression.AmendedBy; len(got) != 0 {
		t.Errorf("D.AmendedBy = %v, want empty (it was repealed, not amended)", got)
	}
}

func TestRun_limit(t *testing.T) {
	srv := fakeEgov(t)
	defer srv.Close()

	n, err := Run(context.Background(), Config{
		BaseURL: srv.URL,
		OutDir:  filepath.Join(t.TempDir(), "graph"),
		Now:     fixedTime,
		Limit:   1, // stop after the first act, before paging further
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Errorf("imported %d acts, want 1 (limit)", n)
	}
}

func TestRun_listErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Run(context.Background(), Config{
		BaseURL: srv.URL,
		OutDir:  filepath.Join(t.TempDir(), "graph"),
	})
	if err == nil {
		t.Error("expected error when /laws returns 500")
	}
}

func TestRun_defaultsDoNotPanic(t *testing.T) {
	// Exercise the nil-Client / zero-Now defaulting against a one-page server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"count":0,"next_offset":0,"laws":[]}`)
	}))
	defer srv.Close()

	n, err := Run(context.Background(), Config{
		BaseURL: srv.URL,
		OutDir:  filepath.Join(t.TempDir(), "graph"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("imported %d, want 0", n)
	}
	if _, err := os.Stat(filepath.Join(t.TempDir())); err != nil {
		t.Fatal(err)
	}
}
