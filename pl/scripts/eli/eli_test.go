package eli

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
// detail + references + articles parsed from struct + text.html.
func buildSampleAct(t *testing.T) *schema.Act {
	t.Helper()
	d, err := ParseDetail(readFixture(t, "act_detail.sample.json"))
	if err != nil {
		t.Fatalf("ParseDetail: %v", err)
	}
	refs, err := ParseReferences(readFixture(t, "act_references.sample.json"))
	if err != nil {
		t.Fatalf("ParseReferences: %v", err)
	}
	arts, err := ParseArticles(
		readFixture(t, "act_struct.sample.json"),
		readFixture(t, "act_text.sample.html"),
	)
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	act, err := ToAct(d, refs, arts, fixedTime)
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
	if act.Country != "pl" {
		t.Errorf("country = %q, want pl", act.Country)
	}
	if act.TypeSlug != "ustawa" {
		t.Errorf("typeSlug = %q, want ustawa", act.TypeSlug)
	}
	if act.Year != 2023 {
		t.Errorf("year = %d, want 2023", act.Year)
	}
	if act.Number != "DU/2777" {
		t.Errorf("number = %q, want DU/2777", act.Number)
	}
	if act.IDLocal != "DU/2023/2777" {
		t.Errorf("idLocal = %q, want DU/2023/2777", act.IDLocal)
	}
	// inForce=NOT_IN_FORCE → repealed (no longer in force).
	if act.Expression.Status != schema.StatusRepealed {
		t.Errorf("status = %v, want Repealed", act.Expression.Status)
	}
	if act.Expression.SourceURL != "https://eli.gov.pl/eli/DU/2023/2777" {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
	}
}

func TestToAct_references(t *testing.T) {
	act := buildSampleAct(t)
	// The fixture has one "Akty zmienione" → amends DU/2022/2666 (a Ustawa).
	wantAmend := schema.ResourceURI("pl", "ustawa", 2022, "DU/2666")
	if len(act.Expression.Amends) != 1 || act.Expression.Amends[0] != wantAmend {
		t.Errorf("amends = %v, want [%s]", act.Expression.Amends, wantAmend)
	}
	if len(act.Expression.Repeals) != 0 {
		t.Errorf("repeals = %v, want none", act.Expression.Repeals)
	}
}

func TestToAct_articles(t *testing.T) {
	act := buildSampleAct(t)
	arts := act.Expression.Articles
	if len(arts) != 2 {
		t.Fatalf("got %d articles, want 2", len(arts))
	}
	if arts[0].Number != "1" || arts[1].Number != "2" {
		t.Errorf("article numbers = %q,%q", arts[0].Number, arts[1].Number)
	}
	if arts[0].Label != "Art. 1." {
		t.Errorf("article[0].Label = %q, want 'Art. 1.'", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
}

func TestParseActList(t *testing.T) {
	l, err := ParseActList(readFixture(t, "list.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Items) == 0 {
		t.Fatal("no items parsed")
	}
	if l.Items[0].ELI == "" {
		t.Error("first item has empty ELI")
	}
}

func TestParsePublisherInfo(t *testing.T) {
	p, err := ParsePublisherInfo(readFixture(t, "publisher.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Code != "DU" {
		t.Errorf("code = %q, want DU", p.Code)
	}
	if len(p.Years) == 0 {
		t.Error("no years parsed")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ typ, title, want string }{
		{"Ustawa", "Ustawa z dnia ... o czymś", "ustawa"},
		{"Ustawa", "Ustawa — Kodeks cywilny", "kodeks"},
		{"Rozporządzenie", "Rozporządzenie ...", "rozporzadzenie"},
		{"Obwieszczenie", "Obwieszczenie ...", "obwieszczenie"},
		{"Uchwała", "Uchwała ...", "uchwala"},
		{"Coś Nowego", "tytuł", "cos-nowego"},
	}
	for _, c := range cases {
		if got := TypeSlug(c.typ, c.title); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.typ, c.title, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		inForce, status string
		want            schema.Status
	}{
		{"IN_FORCE", "", schema.StatusInForce},
		{"NOT_IN_FORCE", "", schema.StatusRepealed},
		{"", "obowiązujący", schema.StatusInForce},
		{"", "uchylony", schema.StatusRepealed},
		{"", "coś", schema.StatusUnknown},
	}
	for _, c := range cases {
		got := statusOf(&Detail{InForce: c.inForce, Status: c.status})
		if got != c.want {
			t.Errorf("statusOf(%q,%q) = %v, want %v", c.inForce, c.status, got, c.want)
		}
	}
}

func TestVersionDate(t *testing.T) {
	// changeDate preferred.
	d := &Detail{ChangeDate: "2024-03-14T12:55:38", Promulgation: "2023-12-27"}
	if got := versionDate(d); !got.Equal(time.Date(2024, 3, 14, 12, 55, 38, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want changeDate", got)
	}
	// fall back to promulgation when changeDate absent.
	d = &Detail{Promulgation: "2023-12-27"}
	if got := versionDate(d); !got.Equal(time.Date(2023, 12, 27, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want promulgation", got)
	}
	// space-separated datetime → date part.
	d = &Detail{ChangeDate: "2024-03-14 12:17"}
	if got := versionDate(d); !got.Equal(time.Date(2024, 3, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want 2024-03-14", got)
	}
}

func TestParseELI_errors(t *testing.T) {
	if _, _, _, err := parseELI("DU-2023-2777"); err == nil {
		t.Error("expected error for missing slashes")
	}
	if _, _, _, err := parseELI("DU/notayear/1"); err == nil {
		t.Error("expected error for bad year")
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := ParseDetail([]byte("{nope")); err == nil {
		t.Error("expected detail parse error")
	}
	if _, err := ParseReferences([]byte("nope")); err == nil {
		t.Error("expected references parse error")
	}
	if _, err := ParseStruct([]byte("nope")); err == nil {
		t.Error("expected struct parse error")
	}
	if _, err := ParseActList([]byte("nope")); err == nil {
		t.Error("expected list parse error")
	}
}

func TestParseArticles_noArticles(t *testing.T) {
	arts, err := ParseArticles([]byte(`[]`), []byte(`<html></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if arts != nil {
		t.Errorf("want nil articles, got %v", arts)
	}
}
