package legi

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
// texte version + struct + the two member article XML files.
func buildSampleAct(t *testing.T) *schema.Act {
	t.Helper()
	tv, err := ParseTexteVersion(readFixture(t, "texte_version.sample.xml"))
	if err != nil {
		t.Fatalf("ParseTexteVersion: %v", err)
	}
	st, err := ParseTexteStruct(readFixture(t, "texte_struct.sample.xml"))
	if err != nil {
		t.Fatalf("ParseTexteStruct: %v", err)
	}
	a1, err := ParseArticle(readFixture(t, "article_1.sample.xml"))
	if err != nil {
		t.Fatalf("ParseArticle 1: %v", err)
	}
	a2, err := ParseArticle(readFixture(t, "article_2.sample.xml"))
	if err != nil {
		t.Fatalf("ParseArticle 2: %v", err)
	}
	byID := map[string]*Article{a1.Common.ID: a1, a2.Common.ID: a2}
	arts, liens := BuildArticles(st, byID)
	act, err := ToAct(tv, arts, liens, fixedTime)
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
	if act.Country != "fr" {
		t.Errorf("country = %q, want fr", act.Country)
	}
	if act.TypeSlug != "code" {
		t.Errorf("typeSlug = %q, want code", act.TypeSlug)
	}
	if act.Number != "LEGITEXT000006070721" {
		t.Errorf("number = %q", act.Number)
	}
	if act.IDLocal != "LEGITEXT000006070721" {
		t.Errorf("idLocal = %q", act.IDLocal)
	}
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
	if act.Expression.SourceURL != "https://www.legifrance.gouv.fr/codes/texte_lc/LEGITEXT000006070721" {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
	}
	// version_date is DATE_DEBUT of the version (mandatory as-of date).
	if !act.Expression.VersionDate.Equal(time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want 2024-07-01", act.Expression.VersionDate)
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
	if arts[0].Label != "Article 1" {
		t.Errorf("article[0].Label = %q, want 'Article 1'", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
}

func TestToAct_relations(t *testing.T) {
	act := buildSampleAct(t)
	// article_1 has a CITATION lien to LEGITEXT000006070721.
	wantCite := schema.ResourceURI("fr", "texte", 0, "LEGITEXT000006070721")
	if len(act.Expression.Cites) != 1 || act.Expression.Cites[0] != wantCite {
		t.Errorf("cites = %v, want [%s]", act.Expression.Cites, wantCite)
	}
}

func TestToAct_noIdentifier(t *testing.T) {
	if _, err := ToAct(&TexteVersion{}, nil, nil, fixedTime); err == nil {
		t.Error("expected error for texte with no CID/ID")
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := ParseTexteVersion([]byte("<nope")); err == nil {
		t.Error("expected texte version parse error")
	}
	if _, err := ParseTexteStruct([]byte("<nope")); err == nil {
		t.Error("expected struct parse error")
	}
	if _, err := ParseArticle([]byte("<nope")); err == nil {
		t.Error("expected article parse error")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ nature, title, want string }{
		{"CODE", "Code civil", "code"},
		{"LOI", "Loi n° 78-17 ...", "loi"},
		{"LOI", "Loi portant code de la route", "code"}, // title wins
		{"ORDONNANCE", "Ordonnance ...", "ordonnance"},
		{"DECRET", "Décret ...", "decret"},
		{"ARRETE", "Arrêté ...", "arrete"},
		{"DELIBERATION", "Délibération", "deliberation"},
		{"", "", "texte"},
	}
	for _, c := range cases {
		if got := TypeSlug(c.nature, c.title); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.nature, c.title, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		etat string
		want schema.Status
	}{
		{"VIGUEUR", schema.StatusInForce},
		{"VIGUEUR_DIFF", schema.StatusInForce},
		{"ABROGE", schema.StatusRepealed},
		{"PERIME", schema.StatusRepealed},
		{"", schema.StatusUnknown},
		{"WAT", schema.StatusUnknown},
	}
	for _, c := range cases {
		if got := statusOf(c.etat); got != c.want {
			t.Errorf("statusOf(%q) = %v, want %v", c.etat, got, c.want)
		}
	}
}

func TestParseDateAndYear(t *testing.T) {
	if !parseDate("").IsZero() {
		t.Error("empty date should be zero")
	}
	if !parseDate(openEndDate).IsZero() {
		t.Error("open-end sentinel should be zero")
	}
	if !parseDate("nope").IsZero() {
		t.Error("bad date should be zero")
	}
	// yearOf prefers DATE_TEXTE; falls back to DATE_PUBLI when sentinel.
	tv := &TexteVersion{}
	tv.Chronicle.DateTexte = openEndDate
	tv.Chronicle.DatePubli = "1804-03-15"
	if y := yearOf(tv); y != 1804 {
		t.Errorf("yearOf = %d, want 1804", y)
	}
	if y := yearOf(&TexteVersion{}); y != 0 {
		t.Errorf("yearOf empty = %d, want 0", y)
	}
}

func TestVersionDateFallbacks(t *testing.T) {
	tv := &TexteVersion{}
	tv.Chronicle.DatePubli = "1999-01-02"
	if got := versionDate(tv); !got.Equal(time.Date(1999, 1, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate fallback = %v, want 1999-01-02", got)
	}
	if got := versionDate(&TexteVersion{}); !got.IsZero() {
		t.Errorf("versionDate empty = %v, want zero", got)
	}
}

func TestBuildArticles_missingBody(t *testing.T) {
	st := &TexteStruct{Liens: []LienArt{{ID: "X", Num: "1"}, {ID: "Y", Num: "2"}}}
	byID := map[string]*Article{"X": {Contenu: "hello"}}
	arts, _ := BuildArticles(st, byID)
	if len(arts) != 1 {
		t.Fatalf("got %d articles, want 1 (Y has no body)", len(arts))
	}
}

func TestToActFieldFallbacks(t *testing.T) {
	// Title/nature/cid fallbacks: only TITRE, common NATURE and common ID set.
	tv := &TexteVersion{}
	tv.Version.Titre = "Bare title"
	tv.Common.Nature = "LOI"
	tv.Common.ID = "LEGITEXT999"
	act, err := ToAct(tv, nil, nil, fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	if act.Expression.Title != "Bare title" {
		t.Errorf("title = %q", act.Expression.Title)
	}
	if act.TypeSlug != "loi" {
		t.Errorf("slug = %q, want loi", act.TypeSlug)
	}
	if act.Number != "LEGITEXT999" {
		t.Errorf("number = %q", act.Number)
	}
}
