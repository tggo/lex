package akn

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

func sampleAct(t *testing.T) *schema.Act {
	t.Helper()
	d, err := Parse(readFixture(t, "act.sample.xml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	act, err := d.ToAct(fixedTime, true)
	if err != nil {
		t.Fatalf("ToAct: %v", err)
	}
	return act
}

func TestToAct_golden(t *testing.T) {
	act := sampleAct(t)

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
	act := sampleAct(t)
	if act.Country != "fi" {
		t.Errorf("country = %q, want fi", act.Country)
	}
	if act.TypeSlug != "laki" {
		t.Errorf("typeSlug = %q, want laki", act.TypeSlug)
	}
	if act.Year != 2019 {
		t.Errorf("year = %d, want 2019", act.Year)
	}
	if act.Number != "2019/469" {
		t.Errorf("number = %q, want 2019/469", act.Number)
	}
	if act.IDLocal != "http://data.finlex.fi/eli/sd/2019/469/ajantasa" {
		t.Errorf("idLocal = %q", act.IDLocal)
	}
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
	if act.Expression.Title != "Laki valtion etuosto-oikeudesta eräillä alueilla" {
		t.Errorf("title = %q", act.Expression.Title)
	}
	if !act.Expression.VersionDate.Equal(time.Date(2022, 12, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want 2022-12-20 (dateConsolidated)", act.Expression.VersionDate)
	}
	if !act.Expression.FirstInForceDate.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("firstInForce = %v, want 2020-01-01", act.Expression.FirstInForceDate)
	}
}

func TestToAct_relations(t *testing.T) {
	act := sampleAct(t)
	// The fixture's proprietary block has one amendedBy → /akn/fi/act/statute/2022/1099.
	want := schema.ResourceURI("fi", "statute", 2022, "2022/1099")
	if len(act.Expression.AmendedBy) != 1 || act.Expression.AmendedBy[0] != want {
		t.Errorf("amendedBy = %v, want [%s]", act.Expression.AmendedBy, want)
	}
}

func TestToAct_articles(t *testing.T) {
	act := sampleAct(t)
	arts := act.Expression.Articles
	if len(arts) == 0 {
		t.Fatal("no articles parsed")
	}
	if arts[0].Number != "1" {
		t.Errorf("article[0].Number = %q, want 1", arts[0].Number)
	}
	if arts[0].Label != "1 §" {
		t.Errorf("article[0].Label = %q, want '1 §'", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
}

func TestToAct_decreeFixture(t *testing.T) {
	d, err := Parse(readFixture(t, "act_decree.sample.xml"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	act, err := d.ToAct(fixedTime, true)
	if err != nil {
		t.Fatalf("ToAct: %v", err)
	}
	if act.TypeSlug != "asetus" {
		t.Errorf("typeSlug = %q, want asetus", act.TypeSlug)
	}
	if act.Year != 2025 || act.Number != "2025/51" {
		t.Errorf("identity = %d/%s, want 2025/2025/51", act.Year, act.Number)
	}
	// issuedUnderActs → cites 1559/2001.
	want := schema.ResourceURI("fi", "statute", 2001, "2001/1559")
	if len(act.Expression.Cites) != 1 || act.Expression.Cites[0] != want {
		t.Errorf("cites = %v, want [%s]", act.Expression.Cites, want)
	}
	if len(act.Expression.Articles) != 4 {
		t.Errorf("articles = %d, want 4", len(act.Expression.Articles))
	}
}

func TestToAct_withoutArticles(t *testing.T) {
	d, err := Parse(readFixture(t, "act.sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	act, err := d.ToAct(fixedTime, false)
	if err != nil {
		t.Fatal(err)
	}
	if act.Expression.Articles != nil {
		t.Errorf("articles = %v, want nil when disabled", act.Expression.Articles)
	}
}

func TestParseList(t *testing.T) {
	docs, err := ParseList(readFixture(t, "list.sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("got %d docs, want 2", len(docs))
	}
	for _, d := range docs {
		num, err := d.number()
		if err != nil || num == "" {
			t.Errorf("doc number = %q err=%v", num, err)
		}
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := Parse([]byte("<nope")); err == nil {
		t.Error("expected parse error for malformed xml")
	}
	if _, err := Parse([]byte(`<akomaNtoso><act><meta></meta></act></akomaNtoso>`)); err == nil {
		t.Error("expected error for document without ELI or title")
	}
	if _, err := ParseList([]byte("<nope")); err == nil {
		t.Error("expected list parse error")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ id, label, want string }{
		{"act", "Laki", "laki"},
		{"decree", "Asetus", "asetus"},
		{"", "Laki", "laki"},
		{"", "Asetus", "asetus"},
		{"", "Valtioneuvoston päätös", "valtioneuvoston-paatos"},
		{"weird-id", "", "weird-id"},
		{"", "", "saados"},
	}
	for _, c := range cases {
		if got := typeSlug(c.id, c.label); got != c.want {
			t.Errorf("typeSlug(%q,%q) = %q, want %q", c.id, c.label, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		isInForce, noLonger string
		want                schema.Status
	}{
		{"true", "", schema.StatusInForce},
		{"false", "", schema.StatusRepealed},
		{"true", "2020-01-01", schema.StatusRepealed},
		{"", "", schema.StatusUnknown},
	}
	for _, c := range cases {
		d := &Document{}
		d.doc.Act.Meta.Proprietary.IsInForce.Value = c.isInForce
		d.doc.Act.Meta.Proprietary.NoLongerForce.Date.Date = c.noLonger
		if got := d.statusOf(); got != c.want {
			t.Errorf("statusOf(%q,%q) = %v, want %v", c.isInForce, c.noLonger, got, c.want)
		}
	}
}

func TestVersionDate_fallbacks(t *testing.T) {
	// dateConsolidated preferred.
	d := &Document{}
	d.doc.Act.Meta.Identification.Expression.Dates = []frbrDate{
		{Date: "2022-12-20", Name: "dateConsolidated"},
		{Date: "2019-03-29", Name: "dateIssued"},
	}
	if got := d.versionDate(); !got.Equal(time.Date(2022, 12, 20, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want dateConsolidated", got)
	}
	// fall back to expression dateIssued.
	d = &Document{}
	d.doc.Act.Meta.Identification.Expression.Dates = []frbrDate{{Date: "2019-03-29", Name: "dateIssued"}}
	if got := d.versionDate(); !got.Equal(time.Date(2019, 3, 29, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want expression dateIssued", got)
	}
	// fall back to work dateIssued.
	d = &Document{}
	d.doc.Act.Meta.Identification.Work.Dates = []frbrDate{{Date: "2018-01-01", Name: "dateIssued"}}
	if got := d.versionDate(); !got.Equal(time.Date(2018, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want work dateIssued", got)
	}
}

func TestToAct_noVersionDate(t *testing.T) {
	d := &Document{}
	d.doc.Act.Preface.DocTitle = "x"
	d.doc.Act.Meta.Identification.Work.Number.Value = "1"
	d.doc.Act.Meta.Proprietary.DocumentYear = "2020"
	if _, err := d.ToAct(fixedTime, false); err == nil {
		t.Error("expected error when version date is missing")
	}
}

func TestSplitAknPath_errors(t *testing.T) {
	if _, _, err := splitAknPath("/akn/fi/foo/bar"); err == nil {
		t.Error("expected error for path without statute segment")
	}
	if _, _, err := splitAknPath("/akn/fi/act/statute/notayear/1"); err == nil {
		t.Error("expected error for bad year")
	}
	y, n, err := splitAknPath("/akn/fi/act/statute-consolidated/2019/469/fin@")
	if err != nil || y != 2019 || n != "469" {
		t.Errorf("split = %d/%s err=%v", y, n, err)
	}
}

func TestStripTags(t *testing.T) {
	got := stripTags(`<p>Hello <ref href="x">world</ref>!</p>`)
	if normalize(got) != "Hello world!" {
		t.Errorf("stripTags = %q", normalize(got))
	}
}

func TestYearNumberFromPath(t *testing.T) {
	// documentYear absent → derive from work FRBRuri.
	d := &Document{}
	d.doc.Act.Meta.Identification.Work.URIs = []frbrValue{{Value: "/akn/fi/act/statute-consolidated/2019/469"}}
	y, err := d.year()
	if err != nil || y != 2019 {
		t.Errorf("year = %d err=%v", y, err)
	}
	n, err := d.number()
	if err != nil || n != "469" {
		t.Errorf("number = %q err=%v", n, err)
	}
}
