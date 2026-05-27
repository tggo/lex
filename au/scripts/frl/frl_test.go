package frl

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

// buildSampleAct assembles the fixture act exactly as the importer would:
// title detail + current version.
func buildSampleAct(t *testing.T) *schema.Act {
	t.Helper()
	d, err := ParseDetail(readFixture(t, "title_detail.sample.json"))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	v, err := ParseVersion(readFixture(t, "version_current.sample.json"))
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}
	act, err := ToAct(d, v, fixedTime)
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

func TestToAct_identityAndStatus(t *testing.T) {
	act := buildSampleAct(t)
	if act.Country != "au" {
		t.Errorf("country = %q, want au", act.Country)
	}
	if act.TypeSlug != "act" {
		t.Errorf("typeSlug = %q, want act", act.TypeSlug)
	}
	if act.Year != 1901 {
		t.Errorf("year = %d, want 1901", act.Year)
	}
	if act.Number != "C1901A00002" {
		t.Errorf("number = %q, want C1901A00002", act.Number)
	}
	if act.IDLocal != "C1901A00002" {
		t.Errorf("idLocal = %q, want C1901A00002", act.IDLocal)
	}
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
	if act.Expression.SourceURL != "https://www.legislation.gov.au/C1901A00002" {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
	}
	// version_date is the current compilation's start (2026-03-28).
	want := time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)
	if !act.Expression.VersionDate.Equal(want) {
		t.Errorf("versionDate = %v, want %v", act.Expression.VersionDate, want)
	}
	// first_in_force is the making date (1901-07-12).
	wantFirst := time.Date(1901, 7, 12, 0, 0, 0, 0, time.UTC)
	if !act.Expression.FirstInForceDate.Equal(wantFirst) {
		t.Errorf("firstInForce = %v, want %v", act.Expression.FirstInForceDate, wantFirst)
	}
}

func TestToAct_amendedByEdge(t *testing.T) {
	act := buildSampleAct(t)
	// The current version's reason: amended BY C2026A00004 (an Act).
	want := schema.ResourceURI("au", "act", 2026, "C2026A00004")
	if len(act.Expression.AmendedBy) != 1 || act.Expression.AmendedBy[0] != want {
		t.Errorf("amendedBy = %v, want [%s]", act.Expression.AmendedBy, want)
	}
	if len(act.Expression.RepealedBy) != 0 {
		t.Errorf("repealedBy = %v, want none", act.Expression.RepealedBy)
	}
	// No section text is captured (deferred — see ADR-0024).
	if len(act.Expression.Articles) != 0 {
		t.Errorf("articles = %d, want 0", len(act.Expression.Articles))
	}
}

func TestToAct_repealedVersion(t *testing.T) {
	d := &Detail{
		ID: "C1901A00001", Name: "Consolidated Revenue", Collection: "Act",
		SeriesType: "Act", Year: 1901, Number: 1, Status: "Repealed",
		MakingDate: "1901-06-25T00:00:00",
	}
	v, err := ParseVersion(readFixture(t, "version_repealed.sample.json"))
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}
	act, err := ToAct(d, v, fixedTime)
	if err != nil {
		t.Fatalf("ToAct: %v", err)
	}
	if act.Expression.Status != schema.StatusRepealed {
		t.Errorf("status = %v, want Repealed", act.Expression.Status)
	}
	want := schema.ResourceURI("au", "act", 1934, "C1934A00045")
	if len(act.Expression.RepealedBy) != 1 || act.Expression.RepealedBy[0] != want {
		t.Errorf("repealedBy = %v, want [%s]", act.Expression.RepealedBy, want)
	}
}

func TestToAct_nilVersionFallsBackToDetail(t *testing.T) {
	d := &Detail{
		ID: "C1901A00002", Name: "Acts Interpretation Act 1901", Collection: "Act",
		SeriesType: "Act", Year: 1901, Number: 2, Status: "InForce",
		MakingDate: "1901-07-12T00:00:00",
	}
	act, err := ToAct(d, nil, fixedTime)
	if err != nil {
		t.Fatalf("ToAct: %v", err)
	}
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
	// version_date falls back to makingDate when no version is present.
	want := time.Date(1901, 7, 12, 0, 0, 0, 0, time.UTC)
	if !act.Expression.VersionDate.Equal(want) {
		t.Errorf("versionDate = %v, want %v (makingDate fallback)", act.Expression.VersionDate, want)
	}
}

func TestToAct_errors(t *testing.T) {
	if _, err := ToAct(nil, nil, fixedTime); err == nil {
		t.Error("expected error for nil detail")
	}
	if _, err := ToAct(&Detail{}, nil, fixedTime); err == nil {
		t.Error("expected error for unidentified detail")
	}
}

func TestParseTitleList(t *testing.T) {
	l, err := ParseTitleList(readFixture(t, "titles_list.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Count == 0 {
		t.Error("@odata.count not parsed")
	}
	if len(l.Value) == 0 || l.Value[0].ID == "" {
		t.Error("no items / empty id")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ collection, series, want string }{
		{"Act", "Act", "act"},
		{"LegislativeInstrument", "", "legislative-instrument"},
		{"NotifiableInstrument", "", "notifiable-instrument"},
		{"Constitution", "", "constitution"},
		{"", "Act", "act"},
		{"PrerogativeInstrument", "", "prerogative-instrument"},
		{"", "", "act"}, // empty → safe default
	}
	for _, c := range cases {
		if got := TypeSlug(c.collection, c.series); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.collection, c.series, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		in   string
		want schema.Status
	}{
		{"InForce", schema.StatusInForce},
		{"Repealed", schema.StatusRepealed},
		{"Ceased", schema.StatusRepealed},
		{"something", schema.StatusUnknown},
	}
	for _, c := range cases {
		if got := statusOf(c.in); got != c.want {
			t.Errorf("statusOf(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseDateTime(t *testing.T) {
	if got := parseDateTime("2026-03-28T00:00:00"); !got.Equal(time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("parseDateTime full = %v", got)
	}
	if got := parseDateTime("2026-03-28"); !got.Equal(time.Date(2026, 3, 28, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("parseDateTime date-only = %v", got)
	}
	if got := parseDateTime(""); !got.IsZero() {
		t.Errorf("parseDateTime empty = %v, want zero", got)
	}
	if got := parseDateTime("garbage"); !got.IsZero() {
		t.Errorf("parseDateTime garbage = %v, want zero", got)
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := ParseDetail([]byte("{nope")); err == nil {
		t.Error("expected detail parse error")
	}
	if _, err := ParseVersion([]byte("nope")); err == nil {
		t.Error("expected version parse error")
	}
	if _, err := ParseTitleList([]byte("nope")); err == nil {
		t.Error("expected list parse error")
	}
}
