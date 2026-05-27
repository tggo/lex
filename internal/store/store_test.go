package store

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
)

var update = flag.Bool("update", false, "update golden files")

func sampleAct() *schema.Act {
	return &schema.Act{
		Country:  "ua",
		TypeSlug: "kodeks",
		Year:     2003,
		Number:   "435-15",
		IDLocal:  "435-15",
		Expression: &schema.Expression{
			Title:            "Цивільний кодекс України",
			LangTag:          "uk",
			LangAlpha3:       "UKR",
			VersionDate:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			FirstInForceDate: time.Date(2004, 1, 1, 0, 0, 0, 0, time.UTC),
			Status:           schema.StatusInForce,
			SourceURL:        "https://zakon.rada.gov.ua/laws/show/435-15",
			RetrievedAt:      time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
			Articles: []schema.Article{
				{Number: "1", Label: "Стаття 1", Text: "Цивільним законодавством регулюються відносини."},
				{Number: "2", Label: "Стаття 2", Text: "Учасниками цивільних відносин є фізичні та юридичні особи."},
			},
			Cites: []string{schema.ResourceURI("ua", "konstytutsiya", 1996, "254к/96-вр")},
		},
	}
}

func TestRoundTrip_memory(t *testing.T) {
	s, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	in := sampleAct()
	if err := s.AddAct(in); err != nil {
		t.Fatalf("AddAct: %v", err)
	}

	got, err := s.GetAct(in.ResourceURI())
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	assertActEqual(t, in, got)
}

func TestRoundTrip_file(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "graph") // Badger uses a directory
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	in := sampleAct()
	if err := s.AddAct(in); err != nil {
		t.Fatalf("AddAct: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen the same directory: data must persist.
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.GetAct(in.ResourceURI())
	if err != nil {
		t.Fatalf("GetAct after reopen: %v", err)
	}
	assertActEqual(t, in, got)
}

func TestListAndEachAct(t *testing.T) {
	s, _ := OpenMemory()
	defer s.Close()
	if err := s.AddAct(sampleAct()); err != nil {
		t.Fatal(err)
	}
	// A second act to confirm listing/ordering.
	second := sampleAct()
	second.Number = "100-1"
	second.Expression.Articles = nil
	second.Expression.Cites = nil
	if err := s.AddAct(second); err != nil {
		t.Fatal(err)
	}

	uris, err := s.ListResourceURIs()
	if err != nil {
		t.Fatal(err)
	}
	if len(uris) != 2 {
		t.Fatalf("got %d resource URIs, want 2", len(uris))
	}

	var seen []string
	if err := s.EachAct(func(a *schema.Act) error {
		seen = append(seen, a.Number)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Errorf("EachAct visited %d acts, want 2", len(seen))
	}
}

func TestEachAct_propagatesError(t *testing.T) {
	s, _ := OpenMemory()
	defer s.Close()
	_ = s.AddAct(sampleAct())
	wantErr := fmt.Errorf("boom")
	if err := s.EachAct(func(*schema.Act) error { return wantErr }); err != wantErr {
		t.Errorf("EachAct error = %v, want %v", err, wantErr)
	}
}

func TestRoundTrip_amendedBy(t *testing.T) {
	s, _ := OpenMemory()
	defer s.Close()

	amending := schema.ResourceURI("jp", "act", 2024, "506AC0000000033")
	in := sampleAct()
	in.Expression.AmendedBy = []string{amending}
	if err := s.AddAct(in); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAct(in.ResourceURI())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Expression.AmendedBy) != 1 || got.Expression.AmendedBy[0] != amending {
		t.Errorf("amendedBy = %v, want [%s]", got.Expression.AmendedBy, amending)
	}
}

func TestGetAct_notFound(t *testing.T) {
	s, _ := OpenMemory()
	defer s.Close()
	if _, err := s.GetAct(schema.ResourceURI("ua", "zakon", 1999, "nope")); err == nil {
		t.Error("expected error for missing act, got nil")
	}
}

func TestDumpSorted_golden(t *testing.T) {
	s, _ := OpenMemory()
	defer s.Close()
	if err := s.AddAct(sampleAct()); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := s.DumpSorted(&buf); err != nil {
		t.Fatal(err)
	}

	golden := filepath.Join("testdata", "civil_code.nt.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("triples mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", buf.Bytes(), want)
	}
}

func assertActEqual(t *testing.T, want, got *schema.Act) {
	t.Helper()
	if got.Country != want.Country || got.TypeSlug != want.TypeSlug ||
		got.Year != want.Year || got.Number != want.Number {
		t.Errorf("identity mismatch: got (%s,%s,%d,%s)", got.Country, got.TypeSlug, got.Year, got.Number)
	}
	w, g := want.Expression, got.Expression
	if g == nil {
		t.Fatal("got nil expression")
	}
	if g.Title != w.Title {
		t.Errorf("title = %q, want %q", g.Title, w.Title)
	}
	if g.LangTag != w.LangTag {
		t.Errorf("langTag = %q, want %q", g.LangTag, w.LangTag)
	}
	if g.LangAlpha3 != w.LangAlpha3 {
		t.Errorf("langAlpha3 = %q, want %q", g.LangAlpha3, w.LangAlpha3)
	}
	if !g.VersionDate.Equal(w.VersionDate) {
		t.Errorf("versionDate = %v, want %v", g.VersionDate, w.VersionDate)
	}
	if !g.FirstInForceDate.Equal(w.FirstInForceDate) {
		t.Errorf("firstInForce = %v, want %v", g.FirstInForceDate, w.FirstInForceDate)
	}
	if g.Status != w.Status {
		t.Errorf("status = %v, want %v", g.Status, w.Status)
	}
	if g.SourceURL != w.SourceURL {
		t.Errorf("sourceURL = %q, want %q", g.SourceURL, w.SourceURL)
	}
	if !g.RetrievedAt.Equal(w.RetrievedAt) {
		t.Errorf("retrievedAt = %v, want %v", g.RetrievedAt, w.RetrievedAt)
	}
	if len(g.Articles) != len(w.Articles) {
		t.Fatalf("articles = %d, want %d", len(g.Articles), len(w.Articles))
	}
	for i := range w.Articles {
		if g.Articles[i] != w.Articles[i] {
			t.Errorf("article[%d] = %+v, want %+v", i, g.Articles[i], w.Articles[i])
		}
	}
	if len(g.Cites) != len(w.Cites) {
		t.Fatalf("cites = %d, want %d", len(g.Cites), len(w.Cites))
	}
	for i := range w.Cites {
		if g.Cites[i] != w.Cites[i] {
			t.Errorf("cites[%d] = %q, want %q", i, g.Cites[i], w.Cites[i])
		}
	}
}
