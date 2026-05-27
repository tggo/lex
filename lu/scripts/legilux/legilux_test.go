package legilux

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

func parseFixtures(t *testing.T) (*Results, *Results) {
	t.Helper()
	acts, err := ParseResults(readFixture(t, "acts_page.sample.json"))
	if err != nil {
		t.Fatalf("ParseResults(acts): %v", err)
	}
	rels, err := ParseResults(readFixture(t, "relations.sample.json"))
	if err != nil {
		t.Fatalf("ParseResults(relations): %v", err)
	}
	return acts, rels
}

// buildSampleActs assembles acts from the fixtures exactly as the importer
// would: the relations fixture is attached to the first act row.
func buildSampleActs(t *testing.T) []*schema.Act {
	t.Helper()
	actsRes, relsRes := parseFixtures(t)
	rows := ParseActRows(actsRes)
	amends, repeals, cites, consolidates := ParseRelations(relsRes)

	var out []*schema.Act
	for i, r := range rows {
		var am, rp, ci, co []string
		if i == 0 { // attach the relations fixture to the first act
			am, rp, ci, co = amends, repeals, cites, consolidates
		}
		if a := ToAct(r, am, rp, ci, co, fixedTime); a != nil {
			out = append(out, a)
		}
	}
	return out
}

func TestToAct_golden(t *testing.T) {
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
		t.Errorf("ToAct mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestToAct_identityAndStatus(t *testing.T) {
	acts := buildSampleActs(t)
	if len(acts) != 3 {
		t.Fatalf("got %d acts, want 3", len(acts))
	}
	a := acts[0]
	if a.Country != "lu" {
		t.Errorf("country = %q, want lu", a.Country)
	}
	if a.TypeSlug != "arrete" {
		t.Errorf("typeSlug = %q, want arrete", a.TypeSlug)
	}
	if a.Year != 1854 {
		t.Errorf("year = %d, want 1854", a.Year)
	}
	if a.Number != "etat/adm/a/1854/04/21/n1/jo" {
		t.Errorf("number = %q", a.Number)
	}
	if a.IDLocal != "etat/adm/a/1854/04/21/n1/jo" {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if a.Expression.SourceURL != "http://data.legilux.public.lu/eli/etat/adm/a/1854/04/21/n1/jo" {
		t.Errorf("sourceURL = %q", a.Expression.SourceURL)
	}
	if a.Expression.LangTag != "fr" || a.Expression.LangAlpha3 != "FRA" {
		t.Errorf("lang = %q/%q, want fr/FRA", a.Expression.LangTag, a.Expression.LangAlpha3)
	}
	// version_date is mandatory; comes from dateDocument.
	if !a.Expression.VersionDate.Equal(time.Date(1854, 4, 21, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v", a.Expression.VersionDate)
	}
}

func TestToAct_repealedCarriesNoLonger(t *testing.T) {
	acts := buildSampleActs(t)
	// The second fixture act is no-longer-in-force with a dateNoLongerInForce.
	a := acts[1]
	if a.Expression.Status != schema.StatusRepealed {
		t.Fatalf("status = %v, want Repealed", a.Expression.Status)
	}
	if a.Expression.NoLongerInForce.IsZero() {
		t.Error("repealed act missing NoLongerInForce date")
	}
}

func TestToAct_relations(t *testing.T) {
	acts := buildSampleActs(t)
	e := acts[0].Expression
	// Relations fixture: 1 modifies (→amends), 2 cites.
	wantAmend := schema.ResourceURI("lu", "", 1988, "etat/leg/loi/1988/12/13/n1/jo")
	if len(e.Amends) != 1 || e.Amends[0] != wantAmend {
		t.Errorf("amends = %v, want [%s]", e.Amends, wantAmend)
	}
	if len(e.Cites) != 2 {
		t.Errorf("cites = %v, want 2", e.Cites)
	}
	if len(e.Repeals) != 0 {
		t.Errorf("repeals = %v, want none", e.Repeals)
	}
	// An EU-directive cite (still under the Legilux ELI base) resolves to a lex
	// work URI like any other Legilux target.
	wantEU := schema.ResourceURI("lu", "", 2003, "dir_ue/2003/109/jo")
	foundEU := false
	for _, c := range e.Cites {
		if c == wantEU {
			foundEU = true
		}
	}
	if !foundEU {
		t.Errorf("expected EU directive cite resolved to %s, got %v", wantEU, e.Cites)
	}
}

func TestToAct_skipsMissingVersionDate(t *testing.T) {
	r := ActRow{WorkURI: eliBase + "etat/leg/loi/x/jo", TypeURI: "x/LOI"}
	if a := ToAct(r, nil, nil, nil, nil, fixedTime); a != nil {
		t.Errorf("want nil act for missing version date, got %+v", a)
	}
}

func TestTypeSlugFromAuthority(t *testing.T) {
	auth := "http://data.legilux.public.lu/resource/authority/resource-type/"
	cases := []struct{ typeURI, work, want string }{
		{auth + "LOI", "", "loi"},
		{auth + "RGD", "", "rgd"},
		{auth + "A", "", "arrete"},
		{auth + "AMIN", "", "amin"},
		{auth + "DIR_UE", "", "dir-ue"},
		{auth + "REG_UE", "", "reg-ue"},
		{auth + "LOI", "http://x/eli/etat/leg/code/civil/jo", "code"},
		{auth + "XYZ", "", "xyz"},
		{"", "", "acte"},
	}
	for _, c := range cases {
		if got := typeSlugFromAuthority(c.typeURI, c.work); got != c.want {
			t.Errorf("typeSlug(%q,%q) = %q, want %q", c.typeURI, c.work, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	base := "http://data.legilux.public.lu/resource/authority/application-status/"
	cases := []struct {
		uri  string
		want schema.Status
	}{
		{base + "in-force", schema.StatusInForce},
		{base + "applicable", schema.StatusInForce},
		{base + "no-longer-in-force", schema.StatusRepealed},
		{base + "no-longer-in-force-implicit", schema.StatusRepealed},
		{base + "not-applicable", schema.StatusRepealed},
		{base + "not-yet-in-force", schema.StatusUnknown},
		{"", schema.StatusUnknown},
	}
	for _, c := range cases {
		if got := statusOf(c.uri); got != c.want {
			t.Errorf("statusOf(%q) = %v, want %v", c.uri, got, c.want)
		}
	}
}

func TestYearOf(t *testing.T) {
	if y := yearOf("2020-03-18", "x"); y != 2020 {
		t.Errorf("yearOf from dateDoc = %d, want 2020", y)
	}
	if y := yearOf("", eliBase+"etat/leg/rgd/2020/03/18/a165/jo"); y != 2020 {
		t.Errorf("yearOf from URI = %d, want 2020", y)
	}
	if y := yearOf("", eliBase+"etat/leg/rgd/jo"); y != 0 {
		t.Errorf("yearOf with no year = %d, want 0", y)
	}
}

func TestParseResults_invalid(t *testing.T) {
	if _, err := ParseResults([]byte("{nope")); err == nil {
		t.Error("expected parse error")
	}
}

func TestParseRelations_foreignTargetKeptVerbatim(t *testing.T) {
	res := &Results{}
	res.Results.Bindings = []map[string]Binding{
		{"rel": {Value: predCites}, "target": {Value: "http://example.org/foreign/act"}},
	}
	_, _, cites, _ := ParseRelations(res)
	if len(cites) != 1 || cites[0] != "http://example.org/foreign/act" {
		t.Errorf("cites = %v, want foreign URI kept verbatim", cites)
	}
}

func TestParseRelations_skipsUnknownPredicate(t *testing.T) {
	res := &Results{}
	res.Results.Bindings = []map[string]Binding{
		{"rel": {Value: NSjolux + "somethingElse"}, "target": {Value: eliBase + "x"}},
		{"rel": {Value: predRepeals}, "target": {Value: ""}}, // empty target skipped
	}
	am, rp, ci, co := ParseRelations(res)
	if am != nil || rp != nil || ci != nil || co != nil {
		t.Errorf("expected no relations, got %v %v %v %v", am, rp, ci, co)
	}
}
