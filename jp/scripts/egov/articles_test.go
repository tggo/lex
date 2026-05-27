package egov

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseArticles_golden(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "lawdata.sample.json"))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}

	got, err := json.MarshalIndent(arts, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	golden := filepath.Join("testdata", "articles.golden.json")
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
		t.Errorf("ParseArticles mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseArticles_fields(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "lawdata.sample.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 {
		t.Fatalf("got %d articles, want 2", len(arts))
	}
	a := arts[0]
	if a.Number != "1" {
		t.Errorf("Number = %q, want 1", a.Number)
	}
	if a.Label != "第一条（基本原則）" {
		t.Errorf("Label = %q, want 第一条（基本原則）", a.Label)
	}
	// All three paragraphs' text must be present and feedable to FTS.
	for _, want := range []string{
		"私権は、公共の福祉に適合しなければならない。",
		"権利の行使及び義務の履行は、信義に従い誠実に行わなければならない。",
		"権利の濫用は、これを許さない。",
	} {
		if !bytes.Contains([]byte(a.Text), []byte(want)) {
			t.Errorf("article 1 text missing %q", want)
		}
	}
	// The caption/title must not be duplicated into the body text.
	if bytes.Contains([]byte(a.Text), []byte("基本原則")) {
		t.Errorf("article 1 text should not repeat the caption: %q", a.Text)
	}
}

func TestParseArticles_invalidJSON(t *testing.T) {
	if _, err := ParseArticles([]byte("{not json")); err == nil {
		t.Error("expected parse error")
	}
}

func TestParseArticles_noArticles(t *testing.T) {
	// A law_data response whose body has no Article nodes yields zero articles,
	// not an error (e.g. a pre-modern decree of plain paragraphs).
	raw := `{"law_full_text":{"tag":"Law","attr":{},"children":[
	  {"tag":"LawBody","attr":{},"children":[
	    {"tag":"MainProvision","attr":{},"children":[
	      {"tag":"Paragraph","attr":{},"children":[
	        {"tag":"Sentence","attr":{},"children":["just a sentence"]}]}]}]}]}}`
	arts, err := ParseArticles([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 0 {
		t.Errorf("got %d articles, want 0", len(arts))
	}
}
