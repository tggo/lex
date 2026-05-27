package egov

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

func TestBuildActs_golden(t *testing.T) {
	acts, err := BuildActs(readFixture(t, "laws.sample.json"), fixedTime)
	if err != nil {
		t.Fatalf("BuildActs: %v", err)
	}

	got, err := json.MarshalIndent(acts, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "acts.golden.json")
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
		t.Errorf("BuildActs mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildActs_mapsFields(t *testing.T) {
	acts, err := BuildActs(readFixture(t, "laws.sample.json"), fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*schema.Act{}
	for _, a := range acts {
		byID[a.Number] = a
	}

	civil, ok := byID["129AC0000000089"]
	if !ok {
		t.Fatal("Civil Code missing from output")
	}
	if civil.Country != "jp" {
		t.Errorf("country = %q, want jp", civil.Country)
	}
	if civil.TypeSlug != "act" {
		t.Errorf("Civil Code slug = %q, want act", civil.TypeSlug)
	}
	if civil.Year != 1896 { // promulgation 1896-04-27
		t.Errorf("year = %d, want 1896 (promulgation)", civil.Year)
	}
	if civil.IDLocal != "129AC0000000089" {
		t.Errorf("idLocal = %q, want law_id", civil.IDLocal)
	}
	e := civil.Expression
	if e.Title != "民法" {
		t.Errorf("title = %q, want 民法", e.Title)
	}
	if e.LangTag != "ja" || e.LangAlpha3 != "JPN" {
		t.Errorf("lang = %q/%q, want ja/JPN", e.LangTag, e.LangAlpha3)
	}
	wantVD := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC) // amendment_enforcement_date
	if !e.VersionDate.Equal(wantVD) {
		t.Errorf("versionDate = %v, want %v (enforcement date)", e.VersionDate, wantVD)
	}
	if e.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", e.Status)
	}
	if e.SourceURL != "https://laws.e-gov.go.jp/law/129AC0000000089" {
		t.Errorf("sourceURL = %q", e.SourceURL)
	}
	if !e.RetrievedAt.Equal(fixedTime) {
		t.Errorf("retrievedAt = %v, want %v", e.RetrievedAt, fixedTime)
	}
}

func TestBuildActs_statusResolution(t *testing.T) {
	acts, err := BuildActs(readFixture(t, "laws.sample.json"), fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]schema.Status{
		"105DF0000000337": schema.StatusInForce,  // CurrentEnforced
		"129AC0000000089": schema.StatusInForce,  // CurrentEnforced
		"113DF0000000036": schema.StatusRepealed, // repeal_status = Repeal
	}
	for _, a := range acts {
		if got := a.Expression.Status; got != want[a.Number] {
			t.Errorf("status[%s] = %v, want %v", a.Number, got, want[a.Number])
		}
	}
}

func TestTypeSlug(t *testing.T) {
	cases := map[string]string{
		"Constitution":         "constitution",
		"Act":                  "act",
		"CabinetOrder":         "cabinet-order",
		"ImperialOrder":        "imperial-order",
		"MinisterialOrdinance": "ministerial-ordinance",
		"Rule":                 "rule",
		"":                     "law",       // defensive fallback
		"DietRule":             "diet-rule", // unknown type -> kebab-cased
	}
	for in, want := range cases {
		if got := TypeSlug(in); got != want {
			t.Errorf("TypeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildActs_dropsRecordsWithoutVersionDate(t *testing.T) {
	// Ontology invariant 1: an expression with no version date is dropped,
	// never guessed.
	raw := `{"laws":[
	  {"law_info":{"law_type":"Act","law_id":"X","promulgation_date":"2000-01-01"},
	   "revision_info":{"law_title":"No date","amendment_enforcement_date":null,
	     "repeal_status":"None","current_revision_status":"CurrentEnforced"}},
	  {"law_info":{"law_type":"Act","law_id":"Y","promulgation_date":"2000-01-01"},
	   "revision_info":{"law_title":"Has date","amendment_enforcement_date":"2001-02-03",
	     "repeal_status":"None","current_revision_status":"CurrentEnforced"}}
	]}`
	acts, err := BuildActs([]byte(raw), fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0].Number != "Y" {
		t.Fatalf("expected only Y to survive, got %d acts", len(acts))
	}
}

func TestBuildActs_yearFromEnforcementWhenPromulgationBadOrMissing(t *testing.T) {
	// No promulgation date, and an unparseable one: the URI year falls back to
	// the enforcement (version) date's year either way.
	raw := `{"laws":[
	  {"law_info":{"law_type":"Act","law_id":"A"},
	   "revision_info":{"law_title":"no promulgation","amendment_enforcement_date":"2010-06-07",
	     "current_revision_status":"CurrentEnforced"}},
	  {"law_info":{"law_type":"Act","law_id":"B","promulgation_date":"not-a-date"},
	   "revision_info":{"law_title":"bad promulgation","amendment_enforcement_date":"2011-08-09",
	     "current_revision_status":"CurrentEnforced"}}
	]}`
	acts, err := BuildActs([]byte(raw), fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"A": 2010, "B": 2011}
	for _, a := range acts {
		if a.Year != want[a.Number] {
			t.Errorf("year[%s] = %d, want %d", a.Number, a.Year, want[a.Number])
		}
	}
}

func TestBuildActs_invalidJSON(t *testing.T) {
	if _, err := BuildActs([]byte("{not json"), fixedTime); err == nil {
		t.Error("expected parse error")
	}
}
