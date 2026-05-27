package eisb

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
	act, err := ParseAct(readFixture(t, "act.sample.html"), fixedTime)
	if err != nil {
		t.Fatalf("ParseAct: %v", err)
	}
	return act
}

func TestParseAct_golden(t *testing.T) {
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
		t.Errorf("ParseAct mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseAct_identity(t *testing.T) {
	act := buildSampleAct(t)
	if act.Country != "ie" {
		t.Errorf("country = %q, want ie", act.Country)
	}
	if act.TypeSlug != "act" {
		t.Errorf("typeSlug = %q, want act", act.TypeSlug)
	}
	if act.Year != 2015 {
		t.Errorf("year = %d, want 2015", act.Year)
	}
	if act.Number != "60" {
		t.Errorf("number = %q, want 60", act.Number)
	}
	if act.IDLocal != "2015/act/60" {
		t.Errorf("idLocal = %q, want 2015/act/60", act.IDLocal)
	}
}

func TestParseAct_expression(t *testing.T) {
	e := buildSampleAct(t).Expression
	if e.Title != "Bankruptcy (Amendment) Act 2015" {
		t.Errorf("title = %q", e.Title)
	}
	if e.LangTag != "en" || e.LangAlpha3 != "ENG" {
		t.Errorf("lang = %q/%q, want en/ENG", e.LangTag, e.LangAlpha3)
	}
	want := time.Date(2015, 12, 25, 0, 0, 0, 0, time.UTC)
	if !e.VersionDate.Equal(want) {
		t.Errorf("versionDate = %v, want %v", e.VersionDate, want)
	}
	if e.VersionDate.IsZero() {
		t.Error("versionDate must not be zero (ontology invariant)")
	}
	if e.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", e.Status)
	}
	if e.SourceURL != "http://www.irishstatutebook.ie/eli/2015/act/60" {
		t.Errorf("sourceURL = %q", e.SourceURL)
	}
}

func TestParseAct_amends(t *testing.T) {
	e := buildSampleAct(t).Expression
	// eli:changes → eli/1988/act/27 (Bankruptcy Act 1988).
	want := schema.ResourceURI("ie", "act", 1988, "27")
	if len(e.Amends) != 1 || e.Amends[0] != want {
		t.Errorf("amends = %v, want [%s]", e.Amends, want)
	}
}

func TestParseAct_sections(t *testing.T) {
	arts := buildSampleAct(t).Expression.Articles
	if len(arts) != 3 {
		t.Fatalf("got %d sections, want 3", len(arts))
	}
	if arts[0].Number != "1" || arts[2].Number != "3" {
		t.Errorf("section numbers = %q..%q", arts[0].Number, arts[2].Number)
	}
	if arts[0].Label != "Section 1" {
		t.Errorf("section[0].Label = %q, want 'Section 1'", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("section[0].Text is empty")
	}
	// Section 1 ("Definitions") must contain its body text.
	if !contains(arts[0].Text, "In this Act") {
		t.Errorf("section[0].Text missing body: %q", arts[0].Text)
	}
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }

func TestParseActList(t *testing.T) {
	l, err := ParseActList(readFixture(t, "list.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if l.ResultCount != 67 {
		t.Errorf("resultCount = %d, want 67", l.ResultCount)
	}
	if len(l.Items) != 3 {
		t.Fatalf("got %d items, want 3", len(l.Items))
	}
	it := l.Items[0]
	if it.Year != 2015 || it.Number != "60" {
		t.Errorf("item0 = %d/%q, want 2015/60", it.Year, it.Number)
	}
	if it.StatuteBookID != "2015/act/60" {
		t.Errorf("statuteBookID = %q, want 2015/act/60", it.StatuteBookID)
	}
}

func TestPrintURL(t *testing.T) {
	want := "http://www.irishstatutebook.ie/eli/2015/act/60/enacted/en/print.html"
	if got := PrintURL("2015/act/60"); got != want {
		t.Errorf("PrintURL = %q, want %q", got, want)
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ res, word, want string }{
		{"http://x/resource-type#ACT", "", "act"},
		{"http://x/resource-type#SI", "", "si"},
		{"", "act", "act"},
		{"", "si", "si"},
		{"", "weird", "act"},
	}
	for _, c := range cases {
		if got := TypeSlug(c.res, c.word); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.res, c.word, got, c.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	if got := parseDate("2015-12-25"); !got.Equal(time.Date(2015, 12, 25, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("parseDate = %v", got)
	}
	if got := parseDate(""); !got.IsZero() {
		t.Errorf("parseDate(\"\") = %v, want zero", got)
	}
	if got := parseDate("nonsense"); !got.IsZero() {
		t.Errorf("parseDate(nonsense) = %v, want zero", got)
	}
}

func TestSplitELIPath_errors(t *testing.T) {
	if _, _, _, err := splitELIPath("2015-act-60"); err == nil {
		t.Error("expected error for missing slashes")
	}
	if _, _, _, err := splitELIPath("notayear/act/60"); err == nil {
		t.Error("expected error for bad year")
	}
}

func TestParseActList_invalid(t *testing.T) {
	if _, err := ParseActList([]byte("{nope")); err == nil {
		t.Error("expected list parse error")
	}
}

func TestParseAct_invalidHTMLNoMeta(t *testing.T) {
	if _, err := ParseAct([]byte("<html><body><p>hi</p></body></html>"), fixedTime); err == nil {
		t.Error("expected error when no ELI metadata present")
	}
}

func TestStatuteBookID(t *testing.T) {
	if got := statuteBookID("http://www.irishstatutebook.ie/eli/2015/act/60"); got != "2015/act/60" {
		t.Errorf("statuteBookID = %q", got)
	}
	if got := statuteBookID("nonsense"); got != "" {
		t.Errorf("statuteBookID(nonsense) = %q, want empty", got)
	}
}

func TestEliPathFromURL(t *testing.T) {
	if got := eliPathFromURL("http://x/eli/2015/act/60/enacted/en"); got != "2015/act/60" {
		t.Errorf("eliPathFromURL = %q", got)
	}
	if got := eliPathFromURL("http://x/no-eli-here"); got != "" {
		t.Errorf("eliPathFromURL = %q, want empty", got)
	}
	if got := eliPathFromURL("http://x/eli/short"); got != "" {
		t.Errorf("eliPathFromURL(short) = %q, want empty", got)
	}
}

func TestSectionAnchorNum(t *testing.T) {
	if sectionAnchorNum("sec12") != "12" {
		t.Error("sec12 should yield 12")
	}
	if sectionAnchorNum("s1._p2") != "" {
		t.Error("sub-paragraph anchor must not be a section")
	}
	if sectionAnchorNum("sched1") != "" {
		t.Error("schedule anchor must not be a section")
	}
	if sectionAnchorNum("sec") != "" {
		t.Error("bare sec must not be a section")
	}
}
