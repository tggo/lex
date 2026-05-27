package lenz

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
// list item metadata + whole.xml.
func buildSampleAct(t *testing.T) *schema.Act {
	t.Helper()
	list, err := ParseList(readFixture(t, "list.sample.xml"))
	if err != nil {
		t.Fatalf("ParseList: %v", err)
	}
	if len(list.Items) == 0 {
		t.Fatal("no list items")
	}
	whole, err := ParseAct(readFixture(t, "act_whole.sample.xml"))
	if err != nil {
		t.Fatalf("ParseAct: %v", err)
	}
	act, err := ToAct(list.Items[0], whole, fixedTime)
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
	if act.Country != "nz" {
		t.Errorf("country = %q, want nz", act.Country)
	}
	if act.TypeSlug != "public-act" {
		t.Errorf("typeSlug = %q, want public-act", act.TypeSlug)
	}
	if act.Year != 2020 {
		t.Errorf("year = %d, want 2020", act.Year)
	}
	if act.Number != "0038" {
		t.Errorf("number = %q, want 0038", act.Number)
	}
	if act.IDLocal != "act/public/2020/0038" {
		t.Errorf("idLocal = %q, want act/public/2020/0038", act.IDLocal)
	}
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
	if act.Expression.SourceURL != "https://www.legislation.govt.nz/act/public/2020/0038/latest/whole.html" {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
	}
}

func TestToAct_versionAndForceDates(t *testing.T) {
	act := buildSampleAct(t)
	wantVD := time.Date(2023, 4, 12, 0, 0, 0, 0, time.UTC)
	if !act.Expression.VersionDate.Equal(wantVD) {
		t.Errorf("versionDate = %v, want %v", act.Expression.VersionDate, wantVD)
	}
	wantFIF := time.Date(2020, 8, 1, 0, 0, 0, 0, time.UTC)
	if !act.Expression.FirstInForceDate.Equal(wantFIF) {
		t.Errorf("firstInForce = %v, want %v", act.Expression.FirstInForceDate, wantFIF)
	}
}

func TestToAct_articles(t *testing.T) {
	act := buildSampleAct(t)
	arts := act.Expression.Articles
	if len(arts) != 3 {
		t.Fatalf("got %d articles, want 3", len(arts))
	}
	if arts[0].Number != "1" || arts[2].Number != "3" {
		t.Errorf("article numbers = %q..%q", arts[0].Number, arts[2].Number)
	}
	if arts[0].Label != "Section 1" {
		t.Errorf("article[0].Label = %q, want 'Section 1'", arts[0].Label)
	}
	// Section 3 sits under a <part> wrapper — confirm flattening reached it.
	if arts[2].Text == "" {
		t.Error("article[2].Text is empty (nested part not flattened?)")
	}
	// Heading should prefix the body text.
	if got := arts[0].Text; got == "" || got[:5] != "Title" {
		t.Errorf("article[0].Text = %q, want heading-prefixed", got)
	}
}

func TestParseList(t *testing.T) {
	l, err := ParseList(readFixture(t, "list.sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(l.Items))
	}
	if l.Items[0].Number != "0038" || l.Items[0].Year != 2020 {
		t.Errorf("first item = %+v", l.Items[0])
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ cat, title, want string }{
		{"public", "Sample Reform Act 2020", "public-act"},
		{"public", "Crimes Code Act", "code"},
		{"", "Some Act", "public-act"},
		{"local", "Auckland Council Act", "local-act"},
		{"private", "Some Trust Act", "private-act"},
		{"imperial", "Magna Carta", "imperial-act"},
		{"weird type", "Whatever", "weird-type-act"},
	}
	for _, c := range cases {
		if got := TypeSlug(c.cat, c.title); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.cat, c.title, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		cover Cover
		want  schema.Status
	}{
		{Cover{}, schema.StatusInForce},
		{Cover{RepealDate: "2019-01-01"}, schema.StatusRepealed},
		{Cover{Repealed: "yes"}, schema.StatusRepealed},
		{Cover{Repealed: "true"}, schema.StatusRepealed},
	}
	for _, c := range cases {
		if got := statusOf(c.cover); got != c.want {
			t.Errorf("statusOf(%+v) = %v, want %v", c.cover, got, c.want)
		}
	}
}

func TestVersionDate(t *testing.T) {
	// version-date preferred.
	c := Cover{VersionDate: "2023-04-12", CommencementDt: "2020-08-01", AssentDate: "2020-07-01"}
	if got := versionDate(c); !got.Equal(time.Date(2023, 4, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want version-date", got)
	}
	// fall back to commencement.
	c = Cover{CommencementDt: "2020-08-01", AssentDate: "2020-07-01"}
	if got := versionDate(c); !got.Equal(time.Date(2020, 8, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want commencement", got)
	}
	// fall back to assent.
	c = Cover{AssentDate: "2020-07-01"}
	if got := versionDate(c); !got.Equal(time.Date(2020, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want assent", got)
	}
}

func TestToAct_errors(t *testing.T) {
	// missing version date → error (ontology invariant).
	whole := &Act{Cover: Cover{Title: "X"}}
	if _, err := ToAct(ListItem{Category: "public", Year: 2020, Number: "0001", Title: "X"}, whole, fixedTime); err == nil {
		t.Error("expected error for missing version date")
	}
	// missing year (no list year, no cover year) → error.
	whole = &Act{Cover: Cover{Title: "X", VersionDate: "2020-01-01"}}
	if _, err := ToAct(ListItem{Category: "public", Number: "0001", Title: "X"}, whole, fixedTime); err == nil {
		t.Error("expected error for missing year")
	}
}

func TestToAct_fallbacksToCover(t *testing.T) {
	// list item lacks year/number/title; cover supplies them.
	whole, err := ParseAct(readFixture(t, "act_whole.sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	act, err := ToAct(ListItem{Category: "public", Number: "0038"}, whole, fixedTime)
	if err != nil {
		t.Fatalf("ToAct: %v", err)
	}
	if act.Year != 2020 {
		t.Errorf("year fallback = %d, want 2020", act.Year)
	}
	if act.Expression.Title != "Sample Reform Act 2020" {
		t.Errorf("title fallback = %q", act.Expression.Title)
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := ParseAct([]byte("<nope")); err == nil {
		t.Error("expected act parse error")
	}
	if _, err := ParseList([]byte("<nope")); err == nil {
		t.Error("expected list parse error")
	}
}

func TestArticles_none(t *testing.T) {
	a := &Act{}
	if got := a.Articles(); got != nil {
		t.Errorf("want nil articles, got %v", got)
	}
}

func TestStripTags(t *testing.T) {
	in := `<para>Hello <emph>world</emph>  and   more</para>`
	if got := stripTags(in); got != "Hello world and more" {
		t.Errorf("stripTags = %q", got)
	}
}

func TestSectionNumber(t *testing.T) {
	cases := map[string]string{"1": "1", "s 10A": "10A", "S 3": "3", " 5 ": "5"}
	for in, want := range cases {
		if got := sectionNumber(in); got != want {
			t.Errorf("sectionNumber(%q) = %q, want %q", in, got, want)
		}
	}
}
