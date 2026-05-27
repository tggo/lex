package uslm

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

const sampleSourceURL = "https://uscode.house.gov/download/releasepoints/us/pl/118/78/usc01@118-78.htm"

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
	d, err := ParseDocument(readFixture(t, "usc01.sample.xml"))
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	act, err := ToAct(d, sampleSourceURL, fixedTime)
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
	if act.Country != "us" {
		t.Errorf("country = %q, want us", act.Country)
	}
	if act.TypeSlug != "usc-title" {
		t.Errorf("typeSlug = %q, want usc-title", act.TypeSlug)
	}
	if act.Number != "title-1" {
		t.Errorf("number = %q, want title-1", act.Number)
	}
	if act.IDLocal != "usc/title-1" {
		t.Errorf("idLocal = %q, want usc/title-1", act.IDLocal)
	}
	if act.Year != 2024 {
		t.Errorf("year = %d, want 2024", act.Year)
	}
	// One operative section remains → title in force.
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
	if act.Expression.SourceURL != sampleSourceURL {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
	}
	// dcterms:modified 2024-01-08 is the as-of date.
	if !act.Expression.VersionDate.Equal(time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want 2024-01-08", act.Expression.VersionDate)
	}
}

func TestToAct_articles(t *testing.T) {
	act := buildSampleAct(t)
	arts := act.Expression.Articles
	if len(arts) != 3 {
		t.Fatalf("got %d articles, want 3", len(arts))
	}
	if arts[0].Number != "1" || arts[1].Number != "2" || arts[2].Number != "5" {
		t.Errorf("article numbers = %q,%q,%q", arts[0].Number, arts[1].Number, arts[2].Number)
	}
	if arts[0].Label != "§ 1. Words denoting number, gender, and so forth" {
		t.Errorf("article[0].Label = %q", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
	// Entities decoded, tags stripped, whitespace collapsed.
	if got := arts[1].Text; got == "" || got[0] == '<' {
		t.Errorf("article[1].Text not flattened: %q", got)
	}
}

func TestTitleNumber(t *testing.T) {
	cases := []struct {
		ident, numVal, want string
	}{
		{"/us/usc/t1", "1", "1"},
		{"/us/usc/t26", "26", "26"},
		{"", "5", "5"},
	}
	for _, c := range cases {
		tl := &Title{Identifier: c.ident, Num: Num{Value: c.numVal}}
		if got := titleNumber(tl); got != c.want {
			t.Errorf("titleNumber(%q,%q) = %q, want %q", c.ident, c.numVal, got, c.want)
		}
	}
}

func TestSectionNumber(t *testing.T) {
	if got := sectionNumber(&Section{Num: Num{Value: "1a"}}); got != "1a" {
		t.Errorf("sectionNumber from num = %q, want 1a", got)
	}
	if got := sectionNumber(&Section{Identifier: "/us/usc/t1/s7"}); got != "7" {
		t.Errorf("sectionNumber from identifier = %q, want 7", got)
	}
	if got := sectionNumber(&Section{}); got != "" {
		t.Errorf("sectionNumber empty = %q, want empty", got)
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		in   string
		want schema.Status
	}{
		{"", schema.StatusInForce},
		{"operative", schema.StatusInForce},
		{"repealed", schema.StatusRepealed},
		{"omitted", schema.StatusRepealed},
		{"transferred", schema.StatusRepealed},
		{"weird", schema.StatusUnknown},
	}
	for _, c := range cases {
		if got := statusOf(c.in); got != c.want {
			t.Errorf("statusOf(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestTitleStatus(t *testing.T) {
	if got := titleStatus(nil); got != schema.StatusUnknown {
		t.Errorf("titleStatus(nil) = %v, want Unknown", got)
	}
	allGone := []Section{{Status: "repealed"}, {Status: "omitted"}}
	if got := titleStatus(allGone); got != schema.StatusRepealed {
		t.Errorf("titleStatus(all repealed) = %v, want Repealed", got)
	}
	mixed := []Section{{Status: "repealed"}, {Status: ""}}
	if got := titleStatus(mixed); got != schema.StatusInForce {
		t.Errorf("titleStatus(mixed) = %v, want InForce", got)
	}
}

func TestVersionDate_fallbacks(t *testing.T) {
	// modified preferred.
	d := &Document{Meta: Meta{Modified: "2024-03-01T00:00:00Z", Created: "2023-01-01"}}
	if got := versionDate(d); !got.Equal(time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want modified", got)
	}
	// fall back to created.
	d = &Document{Meta: Meta{Created: "2023-01-01"}}
	if got := versionDate(d); !got.Equal(time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want created", got)
	}
	// fall back to a section startPeriod.
	d = &Document{}
	d.Main.Title.Sections = []Section{{StartPeriod: "2022-06-15"}}
	if got := versionDate(d); !got.Equal(time.Date(2022, 6, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want startPeriod", got)
	}
	// nothing → zero.
	if got := versionDate(&Document{}); !got.IsZero() {
		t.Errorf("versionDate = %v, want zero", got)
	}
}

func TestFlatten(t *testing.T) {
	in := "<p>Hello &amp; <ref>world</ref></p>\n  <p>again</p>"
	if got := flatten(in); got != "Hello & world again" {
		t.Errorf("flatten = %q", got)
	}
	if got := flatten(""); got != "" {
		t.Errorf("flatten empty = %q", got)
	}
}

func TestParseDocument_errors(t *testing.T) {
	if _, err := ParseDocument([]byte("<not-uslm/>")); err == nil {
		t.Error("expected error for wrong root element")
	}
	if _, err := ParseDocument([]byte("<uslm><meta>")); err == nil {
		t.Error("expected error for malformed xml")
	}
}

func TestToAct_noTitleNumber(t *testing.T) {
	d := &Document{}
	if _, err := ToAct(d, "", fixedTime); err == nil {
		t.Error("expected error when title number is undeterminable")
	}
}
