package ris

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

// buildSampleAct assembles the fixture act exactly as the importer would: the
// law's § document list (one Gesetzesnummer) plus each non-head document's
// article text parsed from its content XML.
func buildSampleAct(t *testing.T) *schema.Act {
	t.Helper()
	_, docs, err := ParseSearchResult(readFixture(t, "law_list.sample.json"))
	if err != nil {
		t.Fatalf("ParseSearchResult: %v", err)
	}
	// Map fixture content XML to the document NOR ids in the list.
	xmlByNOR := map[string]string{
		"NOR11007174": "norm_head.sample.xml",
		"NOR12076986": "norm_p1.sample.xml",
		"NOR12076987": "norm_p2.sample.xml",
	}
	articles := map[string]schema.Article{}
	for _, d := range docs {
		nor := d.Data.Metadaten.Technisch.ID
		fx, ok := xmlByNOR[nor]
		if !ok {
			continue
		}
		art, has, err := ParseArticleText(readFixture(t, fx))
		if err != nil {
			t.Fatalf("ParseArticleText(%s): %v", nor, err)
		}
		if has {
			articles[nor] = art
		}
	}
	act, err := ToAct(docs, articles, fixedTime)
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
	if act.Country != "at" {
		t.Errorf("country = %q, want at", act.Country)
	}
	if act.TypeSlug != "verordnung" {
		t.Errorf("typeSlug = %q, want verordnung", act.TypeSlug)
	}
	if act.Year != 1990 {
		t.Errorf("year = %d, want 1990", act.Year)
	}
	if act.Number != "10007061" {
		t.Errorf("number = %q, want 10007061", act.Number)
	}
	if act.IDLocal != "10007061" {
		t.Errorf("idLocal = %q, want 10007061", act.IDLocal)
	}
	// Ausserkrafttretensdatum present → repealed.
	if act.Expression.Status != schema.StatusRepealed {
		t.Errorf("status = %v, want Repealed", act.Expression.Status)
	}
	if got := act.Expression.NoLongerInForce; got != time.Date(1994, 9, 30, 0, 0, 0, 0, time.UTC) {
		t.Errorf("noLongerInForce = %v, want 1994-09-30", got)
	}
	wantSrc := "https://www.ris.bka.gv.at/GeltendeFassung.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10007061"
	if act.Expression.SourceURL != wantSrc {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
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
	if arts[0].Label != "§ 1" {
		t.Errorf("article[0].Label = %q, want '§ 1'", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
}

func TestToAct_versionDateMandatory(t *testing.T) {
	act := buildSampleAct(t)
	if act.Expression.VersionDate.IsZero() {
		t.Fatal("version date is zero (ontology invariant)")
	}
	// No Geaendert in the fixture → falls back to Inkrafttretensdatum.
	if got := act.Expression.VersionDate; got != time.Date(1990, 12, 16, 0, 0, 0, 0, time.UTC) {
		t.Errorf("versionDate = %v, want 1990-12-16", got)
	}
}

func TestToAct_firstInForce(t *testing.T) {
	act := buildSampleAct(t)
	if got := act.Expression.FirstInForceDate; got != time.Date(1990, 12, 16, 0, 0, 0, 0, time.UTC) {
		t.Errorf("firstInForce = %v, want 1990-12-16", got)
	}
}

func TestParseSearchResult_count(t *testing.T) {
	sr, docs, err := ParseSearchResult(readFixture(t, "law_list.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 {
		t.Fatalf("docs = %d, want 3", len(docs))
	}
	if sr.OgdSearchResult.OgdDocumentResults.Hits.Text != "3" {
		t.Errorf("hits = %q, want 3", sr.OgdSearchResult.OgdDocumentResults.Hits.Text)
	}
	if docs[0].XMLURL() == "" {
		t.Error("head doc has no XML content URL")
	}
}

func TestParseSearchResult_singleHit(t *testing.T) {
	// RIS returns a bare object (not an array) when there is one hit.
	one := `{"OgdSearchResult":{"OgdDocumentResults":{"Hits":{"#text":"1"},` +
		`"OgdDocumentReference":{"Data":{"Metadaten":{"Technisch":{"ID":"NOR1"},` +
		`"Bundesrecht":{"BrKons":{"Paragraphnummer":"0","Gesetzesnummer":"99"}}}}}}}}`
	_, docs, err := ParseSearchResult([]byte(one))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Data.Metadaten.Technisch.ID != "NOR1" {
		t.Errorf("single-hit decode = %+v", docs)
	}
}

func TestParseSearchResult_empty(t *testing.T) {
	_, docs, err := ParseSearchResult([]byte(`{"OgdSearchResult":{"OgdDocumentResults":{"Hits":{"#text":"0"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if docs != nil {
		t.Errorf("want nil docs, got %v", docs)
	}
}

func TestParseSearchResult_invalid(t *testing.T) {
	if _, _, err := ParseSearchResult([]byte("nope")); err == nil {
		t.Error("expected parse error")
	}
	// Bad inner reference type → array decode and object decode both fail.
	if _, _, err := ParseSearchResult([]byte(`{"OgdSearchResult":{"OgdDocumentResults":{"OgdDocumentReference":42}}}`)); err == nil {
		t.Error("expected reference decode error")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ typ, title, want string }{
		{"BG", "Irgendein Bundesgesetz", "bundesgesetz"},
		{"BG", "Allgemeines bürgerliches Gesetzbuch", "gesetzbuch"},
		{"BVG", "Ein Verfassungsgesetz", "bundesverfassungsgesetz"},
		{"V", "Eine Verordnung", "verordnung"},
		{"K", "Kundmachung", "kundmachung"},
		{"", "ohne Typ", "norm"},
		{"XYZ", "Sonderfall", "xyz"},
		{"Größenänderung", "ä-Test", "groessenaenderung"},
	}
	for _, c := range cases {
		if got := TypeSlug(c.typ, c.title); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.typ, c.title, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		ausser, inkraft string
		want            schema.Status
	}{
		{"1994-09-30", "1990-12-16", schema.StatusRepealed},
		{"", "1990-12-16", schema.StatusInForce},
		{"", "", schema.StatusUnknown},
	}
	for _, c := range cases {
		got := statusOf(BrKons{Ausserkrafttretensdatum: c.ausser, Inkrafttretensdatum: c.inkraft})
		if got != c.want {
			t.Errorf("statusOf(%q,%q) = %v, want %v", c.ausser, c.inkraft, got, c.want)
		}
	}
}

func TestVersionDate_prefersGeaendert(t *testing.T) {
	docs := []DocumentReference{{}, {}}
	docs[0].Data.Metadaten.Allgemein.Geaendert = "2020-01-01"
	docs[1].Data.Metadaten.Allgemein.Geaendert = "2024-06-15" // latest wins
	got := versionDate(docs, BrKons{Inkrafttretensdatum: "1990-12-16"})
	if got != time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC) {
		t.Errorf("versionDate = %v, want 2024-06-15", got)
	}
}

func TestYearFromBgbl(t *testing.T) {
	if y := yearFromBgbl("727/1990"); y != 1990 {
		t.Errorf("year = %d, want 1990", y)
	}
	if y := yearFromBgbl("I 12/2024"); y != 2024 {
		t.Errorf("year = %d, want 2024", y)
	}
	if y := yearFromBgbl("garbage"); y != 0 {
		t.Errorf("year = %d, want 0", y)
	}
}

func TestParseArticleText(t *testing.T) {
	art, ok, err := ParseArticleText(readFixture(t, "norm_p1.sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a Text section in §1")
	}
	if art.Text == "" || !bytes.Contains([]byte(art.Text), []byte("Pflichtnotstandsreserve")) {
		t.Errorf("unexpected article text: %.120q", art.Text)
	}
}

func TestParseArticleText_invalid(t *testing.T) {
	if _, _, err := ParseArticleText([]byte("<risdok><nutzdaten")); err == nil {
		t.Error("expected xml parse error")
	}
}

func TestParseArticleText_noTextSection(t *testing.T) {
	xmlNoText := `<risdok xmlns="http://www.bka.gv.at"><nutzdaten><abschnitt>` +
		`<ueberschrift typ="titel">Kurztitel</ueberschrift>` +
		`<absatz>Some title</absatz></abschnitt></nutzdaten></risdok>`
	_, ok, err := ParseArticleText([]byte(xmlNoText))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("expected ok=false when no Text section present")
	}
}

func TestToAct_noDocuments(t *testing.T) {
	if _, err := ToAct(nil, nil, fixedTime); err == nil {
		t.Error("expected error for no documents")
	}
}

func TestToAct_noGesetzesnummer(t *testing.T) {
	var d DocumentReference
	d.Data.Metadaten.Bundesrecht.BrKons.Paragraphnummer = "0"
	if _, err := ToAct([]DocumentReference{d}, nil, fixedTime); err == nil {
		t.Error("expected error for missing Gesetzesnummer")
	}
}
