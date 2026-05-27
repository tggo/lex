package fedlex

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseArticles_golden(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "cc_akn.sample.xml"))
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
		t.Errorf("ParseArticles mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseArticles_structure(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "cc_akn.sample.xml"))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	if len(arts) != 3 {
		t.Fatalf("got %d articles, want 3", len(arts))
	}
	if arts[0].Number != "1" {
		t.Errorf("article[0].Number = %q, want 1", arts[0].Number)
	}
	if arts[0].Label != "Art. 1" {
		t.Errorf("article[0].Label = %q, want 'Art. 1'", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article[0].Text is empty")
	}
	// Footnotes (<authorialNote>) must be excluded from the operative text.
	if bytes.Contains([]byte(arts[0].Text), []byte("Ausdruck gemäss")) {
		t.Errorf("article[0].Text leaked an authorialNote footnote: %q", arts[0].Text)
	}
}

func TestNumberFromLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Art. 1", "1"},
		{"Art. 12a", "12a"},
		{"art 5.", "5"},
		{"Art.  3 ", "3"},
		{"", ""},
	}
	for _, c := range cases {
		if got := numberFromLabel(c.in); got != c.want {
			t.Errorf("numberFromLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseArticles_empty(t *testing.T) {
	arts, err := ParseArticles([]byte(`<akomaNtoso><act><body></body></act></akomaNtoso>`))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	if arts != nil {
		t.Errorf("got %v, want nil for a body with no articles", arts)
	}
}

func TestParseArticles_invalid(t *testing.T) {
	if _, err := ParseArticles([]byte(`<article><num>broken`)); err == nil {
		t.Error("expected parse error for truncated xml")
	}
}
