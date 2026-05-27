package clml

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
)

var update = flag.Bool("update", false, "update golden files")

var fixedTime = time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func buildSampleAct(t *testing.T) *schema.Act {
	t.Helper()
	l, err := ParseLegislation(readFixture(t, "act.sample.xml"))
	if err != nil {
		t.Fatalf("ParseLegislation: %v", err)
	}
	act, err := ToAct(l, fixedTime)
	if err != nil {
		t.Fatalf("ToAct: %v", err)
	}
	return act
}

func TestToAct_golden(t *testing.T) {
	act := buildSampleAct(t)

	got, err := json.MarshalIndent(act, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "act.golden.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("ToAct mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestToAct_identity(t *testing.T) {
	act := buildSampleAct(t)
	if act.Country != "uk" {
		t.Errorf("country = %q, want uk", act.Country)
	}
	if act.TypeSlug != "ukpga" {
		t.Errorf("typeSlug = %q, want ukpga", act.TypeSlug)
	}
	if act.Year != 2023 {
		t.Errorf("year = %d, want 2023", act.Year)
	}
	if act.Number != "57" {
		t.Errorf("number = %q, want 57", act.Number)
	}
	if act.IDLocal != "ukpga/2023/57" {
		t.Errorf("idLocal = %q, want ukpga/2023/57", act.IDLocal)
	}
	if act.Expression.SourceURL != "https://www.legislation.gov.uk/ukpga/2023/57" {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
	}
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
}

func TestToAct_versionAndFirstInForce(t *testing.T) {
	act := buildSampleAct(t)
	// RestrictStartDate=2023-12-18 is the as-of/consolidation date.
	wantV := time.Date(2023, 12, 18, 0, 0, 0, 0, time.UTC)
	if !act.Expression.VersionDate.Equal(wantV) {
		t.Errorf("versionDate = %v, want %v", act.Expression.VersionDate, wantV)
	}
	if !act.Expression.FirstInForceDate.Equal(wantV) {
		t.Errorf("firstInForce = %v, want %v", act.Expression.FirstInForceDate, wantV)
	}
}

func TestToAct_cites(t *testing.T) {
	act := buildSampleAct(t)
	// The fixture cites ukpga/2024/5.
	want := schema.ResourceURI("uk", "ukpga", 2024, "5")
	found := false
	for _, c := range act.Expression.Cites {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Errorf("cites = %v, want to include %s", act.Expression.Cites, want)
	}
}

func TestToAct_articles(t *testing.T) {
	act := buildSampleAct(t)
	arts := act.Expression.Articles
	if len(arts) == 0 {
		t.Fatal("no articles parsed")
	}
	if arts[0].Number == "" {
		t.Error("first article has empty number")
	}
	if arts[0].Text == "" {
		t.Error("first article has empty text")
	}
}

func TestParseFeed(t *testing.T) {
	f, err := ParseFeed(readFixture(t, "feed.sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Entries) == 0 {
		t.Fatal("no feed entries parsed")
	}
	p, ok := f.Entries[0].Path()
	if !ok {
		t.Fatal("first entry has no path")
	}
	if p != "ukpga/2023/57" {
		t.Errorf("first entry path = %q, want ukpga/2023/57", p)
	}
	if f.Entries[0].Number.Value != "57" {
		t.Errorf("number = %q, want 57", f.Entries[0].Number.Value)
	}
}

func TestFeedEntryPath_bad(t *testing.T) {
	if _, ok := (FeedEntry{ID: "nonsense"}).Path(); ok {
		t.Error("expected no path for bad id")
	}
	if _, ok := (FeedEntry{ID: "http://x/id/ukpga"}).Path(); ok {
		t.Error("expected no path for too-short id")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := map[string]string{
		"ukpga": "ukpga", "uksi": "uksi", " UKPGA ": "ukpga", "asp": "asp",
	}
	for in, want := range cases {
		if got := TypeSlug(in); got != want {
			t.Errorf("TypeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionDate_fallbacks(t *testing.T) {
	l := &Legislation{RestrictStartDate: "2020-01-01"}
	l.Metadata.Valid = "2019-01-01"
	l.Metadata.EnactmentDate.Date = "2018-01-01"
	if got := versionDate(l); !got.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate prefers RestrictStartDate, got %v", got)
	}
	l2 := &Legislation{}
	l2.Metadata.Valid = "2019-02-02"
	if got := versionDate(l2); !got.Equal(time.Date(2019, 2, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate falls back to valid, got %v", got)
	}
	l3 := &Legislation{}
	l3.Metadata.EnactmentDate.Date = "2017-03-03"
	if got := versionDate(l3); !got.Equal(time.Date(2017, 3, 3, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate falls back to enactment, got %v", got)
	}
	if got := versionDate(&Legislation{}); !got.IsZero() {
		t.Errorf("versionDate with nothing should be zero, got %v", got)
	}
}

func TestCitationURIToResource(t *testing.T) {
	// Full /id/ path.
	got := citationURIToResource(Citation{URI: "http://www.legislation.gov.uk/id/uksi/1998/3175"})
	if want := schema.ResourceURI("uk", "uksi", 1998, "3175"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Class fallback (no parseable /id/ triple).
	got = citationURIToResource(Citation{URI: "http://x/id/foo", Class: "UnitedKingdomPublicGeneralAct", Year: "2020", Num: "9"})
	if want := schema.ResourceURI("uk", "ukpga", 2020, "9"); got != want {
		t.Errorf("class fallback got %q, want %q", got, want)
	}
	// Unresolvable.
	if got := citationURIToResource(Citation{URI: "http://example.com/random"}); got != "" {
		t.Errorf("expected empty for non-legislation URI, got %q", got)
	}
	if got := citationURIToResource(Citation{URI: "http://x/id/uksi/notayear/1"}); got != "" {
		t.Errorf("expected empty for bad year, got %q", got)
	}
}

func TestClassToType(t *testing.T) {
	cases := map[string]string{
		"UnitedKingdomPublicGeneralAct":    "ukpga",
		"UnitedKingdomStatutoryInstrument": "uksi",
		"ScottishAct":                      "asp",
		"NorthernIrelandAct":               "nia",
		"Unknown":                          "",
	}
	for in, want := range cases {
		if got := classToType(in); got != want {
			t.Errorf("classToType(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := ParseLegislation([]byte("<nope")); err == nil {
		t.Error("expected legislation parse error")
	}
	if _, err := ParseFeed([]byte("<nope")); err == nil {
		t.Error("expected feed parse error")
	}
}

func TestParseLegislation_pathError(t *testing.T) {
	// Valid XML but no usable id/document URI → ToAct path error.
	l, err := ParseLegislation([]byte(`<Legislation></Legislation>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ToAct(l, fixedTime); err == nil {
		t.Error("expected path error for empty legislation")
	}
}

func TestArticles_none(t *testing.T) {
	if got := Articles(&Legislation{}); got != nil {
		t.Errorf("want nil articles, got %v", got)
	}
}

func TestSplitPath_errors(t *testing.T) {
	if _, _, _, err := splitPath("ukpga/2023"); err == nil {
		t.Error("expected error for short path")
	}
	if _, _, _, err := splitPath("ukpga/xx/1"); err == nil {
		t.Error("expected error for bad year")
	}
}

func TestFlattenText(t *testing.T) {
	got := flattenText(`<Text>Hello   <b>world</b>
		again</Text>`)
	if got != "Hello world again" {
		t.Errorf("flattenText = %q", got)
	}
}
