package ogd

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

func TestBuildActs_golden(t *testing.T) {
	si := NewStatusIndex(
		ParseIDList(readFixture(t, "perv1.sample.txt")),
		ParseIDList(readFixture(t, "perv0.sample.txt")),
	)
	acts, err := BuildActs(
		readFixture(t, "cards.sample.json"),
		readFixture(t, "texts.sample.json"),
		si, fixedTime,
	)
	if err != nil {
		t.Fatalf("BuildActs: %v", err)
	}

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
		t.Errorf("BuildActs mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestBuildActs_statusResolution(t *testing.T) {
	si := NewStatusIndex(
		ParseIDList(readFixture(t, "perv1.sample.txt")),
		ParseIDList(readFixture(t, "perv0.sample.txt")),
	)
	acts, err := BuildActs(readFixture(t, "cards.sample.json"), readFixture(t, "texts.sample.json"), si, fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]schema.Status{
		"4870-20": schema.StatusInForce, // in perv1 fixture
		"4840-20": schema.StatusInForce, // in perv1 fixture
		"4868-20": schema.StatusUnknown, // in no list
	}
	for _, a := range acts {
		if got := a.Expression.Status; got != want[a.Number] {
			t.Errorf("status[%s] = %v, want %v", a.Number, got, want[a.Number])
		}
	}
}

func TestTypeSlug(t *testing.T) {
	cases := map[string]string{
		"Конституція України":                              "konstytutsiya",
		"Цивільний кодекс України":                         "kodeks",
		"КОДЕКС законів про працю України":                 "kodeks", // case-insensitive
		"Про основні засади державного нагляду (контролю)": "zakon",
	}
	for title, want := range cases {
		if got := TypeSlug(title); got != want {
			t.Errorf("TypeSlug(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestStatusIndex(t *testing.T) {
	si := NewStatusIndex(map[string]bool{"a": true}, map[string]bool{"b": true})
	if si.Status("a") != schema.StatusInForce {
		t.Error("a should be in force")
	}
	if si.Status("b") != schema.StatusRepealed {
		t.Error("b should be repealed")
	}
	if si.Status("c") != schema.StatusUnknown {
		t.Error("c should be unknown")
	}
	// repealed takes precedence if an id is (erroneously) in both lists.
	si2 := NewStatusIndex(map[string]bool{"x": true}, map[string]bool{"x": true})
	if si2.Status("x") != schema.StatusRepealed {
		t.Error("repealed must win over in-force")
	}
}

func TestToAct_versionDateFallback(t *testing.T) {
	c := Card{Dokid: 1, Nreg: "1-1", Nazva: "Закон", Orgdat: 20200115}
	// No text record: version date falls back to adoption date.
	a, err := ToAct(c, nil, schema.StatusInForce, fixedTime)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Expression.VersionDate.Equal(time.Date(2020, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("versionDate = %v, want adoption date", a.Expression.VersionDate)
	}
	if a.Year != 2020 {
		t.Errorf("year = %d, want 2020", a.Year)
	}
}

func TestToAct_errors(t *testing.T) {
	if _, err := ToAct(Card{Dokid: 5, Orgdat: 20200101}, nil, schema.StatusUnknown, fixedTime); err == nil {
		t.Error("expected error for missing nreg")
	}
	if _, err := ToAct(Card{Nreg: "1-1", Orgdat: 0}, nil, schema.StatusUnknown, fixedTime); err == nil {
		t.Error("expected error for bad orgdat")
	}
	if _, err := ToAct(Card{Nreg: "1-1", Orgdat: 20201399}, nil, schema.StatusUnknown, fixedTime); err == nil {
		t.Error("expected error for invalid month/day")
	}
}

func TestParse_invalid(t *testing.T) {
	if _, err := ParseCards([]byte("{not json")); err == nil {
		t.Error("expected cards parse error")
	}
	if _, err := ParseTexts([]byte("nope")); err == nil {
		t.Error("expected texts parse error")
	}
}
