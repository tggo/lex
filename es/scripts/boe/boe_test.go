package boe

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
// metadatos + analisis + articles parsed from the texto XML.
func buildSampleAct(t *testing.T) *schema.Act {
	t.Helper()
	m, err := ParseMetadatos(readFixture(t, "norma_metadatos.sample.json"))
	if err != nil {
		t.Fatalf("ParseMetadatos: %v", err)
	}
	an, err := ParseAnalisis(readFixture(t, "norma_analisis.sample.json"))
	if err != nil {
		t.Fatalf("ParseAnalisis: %v", err)
	}
	arts, err := ParseArticles(readFixture(t, "norma_texto.sample.xml"))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	act, err := ToAct(m, an, arts, fixedTime)
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
	if act.Country != "es" {
		t.Errorf("country = %q, want es", act.Country)
	}
	if act.TypeSlug != "ley" {
		t.Errorf("typeSlug = %q, want ley", act.TypeSlug)
	}
	if act.Year != 2021 {
		t.Errorf("year = %d, want 2021", act.Year)
	}
	if act.Number != "BOE-A-2021-6945" {
		t.Errorf("number = %q, want BOE-A-2021-6945", act.Number)
	}
	if act.IDLocal != "BOE-A-2021-6945" {
		t.Errorf("idLocal = %q", act.IDLocal)
	}
	// estatus_derogacion=N, vigencia_agotada=N → in force.
	if act.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", act.Expression.Status)
	}
	if act.Expression.SourceURL != "https://www.boe.es/eli/es/l/2021/04/28/6" {
		t.Errorf("sourceURL = %q", act.Expression.SourceURL)
	}
	if !act.Expression.VersionDate.Equal(time.Date(2026, 5, 27, 8, 23, 30, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want fecha_actualizacion", act.Expression.VersionDate)
	}
}

func TestToAct_references(t *testing.T) {
	act := buildSampleAct(t)
	exp := act.Expression
	// anteriores: DEROGA BOE-A-1988-29622 (repeals), MODIFICA BOE-A-2011-12628
	// (amends), three AMPLÍA → cites.
	wantRepeal := schema.ResourceURI("es", "norma", 1988, "BOE-A-1988-29622")
	if len(exp.Repeals) != 1 || exp.Repeals[0] != wantRepeal {
		t.Errorf("repeals = %v, want [%s]", exp.Repeals, wantRepeal)
	}
	wantAmend := schema.ResourceURI("es", "norma", 2011, "BOE-A-2011-12628")
	if len(exp.Amends) != 1 || exp.Amends[0] != wantAmend {
		t.Errorf("amends = %v, want [%s]", exp.Amends, wantAmend)
	}
	if len(exp.Cites) != 3 {
		t.Errorf("cites = %v, want 3 (the AMPLÍA refs)", exp.Cites)
	}
	// posteriores: SE AMPLÍA by BOE-A-2026-11397 — inbound non-amend/repeal,
	// not modelled, so no amended_by/repealed_by.
	if len(exp.AmendedBy) != 0 || len(exp.RepealedBy) != 0 {
		t.Errorf("unexpected inbound edges: amendedBy=%v repealedBy=%v", exp.AmendedBy, exp.RepealedBy)
	}
}

func TestToAct_articles(t *testing.T) {
	act := buildSampleAct(t)
	arts := act.Expression.Articles
	if len(arts) != 2 {
		t.Fatalf("got %d articles, want 2", len(arts))
	}
	if arts[0].Label != "Disposición adicional única" {
		t.Errorf("article[0].Label = %q", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
}

func TestParseList(t *testing.T) {
	items, err := ParseList(readFixture(t, "list.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("no items parsed")
	}
	if items[0].Identificador == "" {
		t.Error("first item has empty identificador")
	}
}

func TestTypeSlug(t *testing.T) {
	cases := []struct{ rango, title, want string }{
		{"Ley", "Ley 6/2021 ...", "ley"},
		{"Ley Orgánica", "Ley Orgánica ...", "ley-organica"},
		{"Real Decreto", "Real Decreto ...", "real-decreto"},
		{"Real Decreto-ley", "Real Decreto-ley ...", "real-decreto-ley"},
		{"Real Decreto Legislativo", "Texto refundido ...", "real-decreto-legislativo"},
		{"Ley", "Real Decreto Legislativo ... Código Penal", "codigo"},
		{"Orden", "Orden ...", "orden"},
		{"Resolución", "Resolución ...", "resolucion"},
		{"Algo Nuevo", "título", "algo-nuevo"},
		{"", "", "norma"},
	}
	for _, c := range cases {
		if got := TypeSlug(c.rango, c.title); got != c.want {
			t.Errorf("TypeSlug(%q,%q) = %q, want %q", c.rango, c.title, got, c.want)
		}
	}
}

func TestStatusOf(t *testing.T) {
	cases := []struct {
		derog, agotada string
		want           schema.Status
	}{
		{"N", "N", schema.StatusInForce},
		{"S", "N", schema.StatusRepealed},
		{"N", "S", schema.StatusRepealed},
		{"", "", schema.StatusUnknown},
	}
	for _, c := range cases {
		got := statusOf(&Metadatos{EstatusDerogacion: c.derog, VigenciaAgotada: c.agotada})
		if got != c.want {
			t.Errorf("statusOf(%q,%q) = %v, want %v", c.derog, c.agotada, got, c.want)
		}
	}
}

func TestVersionDate(t *testing.T) {
	// fecha_actualizacion preferred (full timestamp).
	m := &Metadatos{FechaActualizacion: "20260527T082330Z", FechaDisposicion: "20210428"}
	if got := versionDate(m); !got.Equal(time.Date(2026, 5, 27, 8, 23, 30, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want fecha_actualizacion", got)
	}
	// fall back to fecha_disposicion when actualizacion absent.
	m = &Metadatos{FechaDisposicion: "20210428"}
	if got := versionDate(m); !got.Equal(time.Date(2021, 4, 28, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want fecha_disposicion", got)
	}
	// malformed actualizacion → date-prefix fallback.
	m = &Metadatos{FechaActualizacion: "20210429xxxxx"}
	if got := versionDate(m); !got.Equal(time.Date(2021, 4, 29, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want date prefix", got)
	}
}

func TestRelationFor(t *testing.T) {
	cases := []struct {
		label, dir, kind string
		ok               bool
	}{
		{"DEROGA", "anteriores", "repeals", true},
		{"SE DEROGA", "posteriores", "repealed_by", true},
		{"MODIFICA", "anteriores", "amends", true},
		{"SE MODIFICA", "posteriores", "amended_by", true},
		{"AMPLÍA", "anteriores", "cites", true},
		{"SE AMPLÍA", "posteriores", "", false},
	}
	for _, c := range cases {
		kind, ok := relationFor(c.label, c.dir)
		if kind != c.kind || ok != c.ok {
			t.Errorf("relationFor(%q,%q) = (%q,%v), want (%q,%v)", c.label, c.dir, kind, ok, c.kind, c.ok)
		}
	}
}

func TestIDYear_errors(t *testing.T) {
	if _, err := idYear("BOE-A"); err == nil {
		t.Error("expected error for short id")
	}
	if _, err := idYear("BOE-A-notayear-1"); err == nil {
		t.Error("expected error for bad year")
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := ParseList([]byte("nope")); err == nil {
		t.Error("expected list parse error")
	}
	if _, err := ParseMetadatos([]byte("{nope")); err == nil {
		t.Error("expected metadatos parse error")
	}
	if _, err := ParseMetadatos([]byte(`{"data":[]}`)); err == nil {
		t.Error("expected error for empty metadatos data")
	}
	if _, err := ParseAnalisis([]byte("nope")); err == nil {
		t.Error("expected analisis parse error")
	}
	if _, err := ParseArticles([]byte("<not-xml")); err == nil {
		t.Error("expected texto parse error")
	}
}

func TestParseAnalisis_empty(t *testing.T) {
	an, err := ParseAnalisis([]byte(`{"data":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if an == nil {
		t.Fatal("want non-nil empty analisis")
	}
}

func TestParseArticles_noPreceptos(t *testing.T) {
	xml := `<?xml version="1.0"?><response><data><texto>` +
		`<bloque id="pr" tipo="preambulo" titulo="[preambulo]"><version><p>x</p></version></bloque>` +
		`</texto></data></response>`
	arts, err := ParseArticles([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if arts != nil {
		t.Errorf("want nil articles, got %v", arts)
	}
}

func TestNumberFromTitle(t *testing.T) {
	if got := numberFromTitle("Artículo 6. Código personal.", "a6"); got != "6" {
		t.Errorf("numberFromTitle numeric = %q, want 6", got)
	}
	if got := numberFromTitle("Disposición adicional única", "da"); got != "da" {
		t.Errorf("numberFromTitle fallback = %q, want da", got)
	}
}

func TestParseRefList_arrayShape(t *testing.T) {
	// A bare array of grouping objects, single-object inner value.
	raw := json.RawMessage(`[{"anterior":{"id_norma":"BOE-A-2000-1","relacion":{"texto":"DEROGA"}}}]`)
	got := parseRefList(raw)
	if len(got) != 1 || got[0].IDNorma != "BOE-A-2000-1" {
		t.Errorf("parseRefList array shape = %v", got)
	}
	if parseRefList(nil) != nil {
		t.Error("nil raw should yield nil")
	}
}
