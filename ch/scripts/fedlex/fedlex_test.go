package fedlex

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

func buildSampleActs(t *testing.T) []*schema.Act {
	t.Helper()
	bs, err := ParseResults(readFixture(t, "sr_acts.sample.json"))
	if err != nil {
		t.Fatalf("ParseResults: %v", err)
	}
	return ToActs(bs, fixedTime)
}

func TestToActs_golden(t *testing.T) {
	acts := buildSampleActs(t)

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
		t.Errorf("ToActs mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestToActs_identityAndStatus(t *testing.T) {
	acts := buildSampleActs(t)
	if len(acts) != 3 {
		t.Fatalf("got %d acts, want 3", len(acts))
	}
	// Sorted by SR notation: 210, 220, 311.0.
	zgb := acts[0]
	if zgb.Country != "ch" {
		t.Errorf("country = %q, want ch", zgb.Country)
	}
	if zgb.Number != "210" {
		t.Errorf("number = %q, want 210", zgb.Number)
	}
	if zgb.IDLocal != "SR 210" {
		t.Errorf("idLocal = %q, want SR 210", zgb.IDLocal)
	}
	if zgb.TypeSlug != "gesetzbuch" {
		t.Errorf("typeSlug = %q, want gesetzbuch", zgb.TypeSlug)
	}
	if zgb.Year != 1912 {
		t.Errorf("year = %d, want 1912", zgb.Year)
	}
	if zgb.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", zgb.Expression.Status)
	}
	if zgb.Expression.SourceURL != "https://fedlex.data.admin.ch/eli/cc/24/233_245_233" {
		t.Errorf("sourceURL = %q", zgb.Expression.SourceURL)
	}
	if zgb.Expression.VersionDate.IsZero() {
		t.Error("version_date is mandatory but zero")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ short, title, want string }{
		{"ZGB", "Schweizerisches Zivilgesetzbuch vom 10. Dezember 1907", "gesetzbuch"},
		{"BV", "Bundesverfassung der Schweizerischen Eidgenossenschaft", "bundesverfassung"},
		{"DSG", "Bundesgesetz über den Datenschutz", "bundesgesetz"},
		{"ZEV", "Verordnung des EFD über Zollerleichterungen", "verordnung"},
		{"", "Bundesbeschluss über die Genehmigung", "bundesbeschluss"},
		{"", "Reglement der Bundesversammlung", "reglement"},
		{"", "Briefwechsel zwischen X und Y", "erlass"},
	}
	for _, c := range cases {
		if got := TypeSlug(c.short, c.title); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.short, c.title, got, c.want)
		}
	}
}

func TestStatusAndVersionDate_repealed(t *testing.T) {
	// A row carrying dateNoLongerInForce is repealed; version_date freezes at
	// the repeal date and NoLongerInForce is recorded.
	b := binding{
		"cc":                  {Value: "https://fedlex.data.admin.ch/eli/cc/2003/731"},
		"srNotation":          {Value: "916.344"},
		"title":               {Value: "Höchstbestandesverordnung"},
		"titleShort":          {Value: "HBV"},
		"dateEntryInForce":    {Value: "2004-01-01"},
		"dateNoLongerInForce": {Value: "2014-01-01"},
	}
	act, ok := toAct(b, fixedTime)
	if !ok {
		t.Fatal("toAct returned ok=false for a complete repealed row")
	}
	if act.Expression.Status != schema.StatusRepealed {
		t.Errorf("status = %v, want Repealed", act.Expression.Status)
	}
	want := time.Date(2014, 1, 1, 0, 0, 0, 0, time.UTC)
	if !act.Expression.VersionDate.Equal(want) {
		t.Errorf("version_date = %v, want repeal date %v", act.Expression.VersionDate, want)
	}
	if !act.Expression.NoLongerInForce.Equal(want) {
		t.Errorf("noLongerInForce = %v, want %v", act.Expression.NoLongerInForce, want)
	}
	if act.Year != 2014 {
		t.Errorf("year = %d, want 2014 (from version date)", act.Year)
	}
}

func TestToAct_dropsIncomplete(t *testing.T) {
	// Missing title.
	if _, ok := toAct(binding{"cc": {Value: "x"}, "srNotation": {Value: "1"}}, fixedTime); ok {
		t.Error("expected drop when title missing")
	}
	// Missing version date (no entry-in-force, not repealed) → mandatory field
	// unavailable, must drop.
	b := binding{"cc": {Value: "x"}, "srNotation": {Value: "1"}, "title": {Value: "T"}}
	if _, ok := toAct(b, fixedTime); ok {
		t.Error("expected drop when version_date unavailable")
	}
}

func TestToActs_dedupAndSort(t *testing.T) {
	bs := []binding{
		{"cc": {Value: "b"}, "srNotation": {Value: "311.0"}, "title": {Value: "B"}, "dateEntryInForce": {Value: "1942-01-01"}},
		{"cc": {Value: "a"}, "srNotation": {Value: "210"}, "title": {Value: "A Gesetzbuch"}, "dateEntryInForce": {Value: "1912-01-01"}},
		{"cc": {Value: "a2"}, "srNotation": {Value: "210"}, "title": {Value: "A duplicate"}, "dateEntryInForce": {Value: "1912-01-01"}},
		{"cc": {Value: "skip"}, "srNotation": {Value: ""}, "title": {Value: "no sr"}},
	}
	acts := ToActs(bs, fixedTime)
	if len(acts) != 2 {
		t.Fatalf("got %d acts, want 2 (dedup by SR, drop empty)", len(acts))
	}
	if acts[0].Number != "210" || acts[1].Number != "311.0" {
		t.Errorf("order = %q,%q, want 210,311.0", acts[0].Number, acts[1].Number)
	}
	// First-seen solution wins for a duplicated SR.
	if acts[0].Expression.Title != "A Gesetzbuch" {
		t.Errorf("title = %q, want first-seen 'A Gesetzbuch'", acts[0].Expression.Title)
	}
}

func TestSRLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"210", "311.0", true},
		{"311.0", "916.344", true},
		{"916.344", "210", false},
		{"210", "210.1", true},
		{"abc", "210", false}, // non-numeric falls back to string compare
	}
	for _, c := range cases {
		if got := srLess(c.a, c.b); got != c.want {
			t.Errorf("srLess(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestParseResults_invalid(t *testing.T) {
	if _, err := ParseResults([]byte("{nope")); err == nil {
		t.Error("expected parse error for malformed JSON")
	}
}
