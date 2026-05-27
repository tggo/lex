package legilux

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseArticles_golden(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "act.sample.html"))
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
	arts, err := ParseArticles(readFixture(t, "act.sample.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 3 {
		t.Fatalf("got %d articles, want 3", len(arts))
	}
	if arts[0].Number != "1er" {
		t.Errorf("first article number = %q, want 1er", arts[0].Number)
	}
	if arts[1].Number != "2" || arts[2].Number != "3" {
		t.Errorf("article numbers = %q,%q, want 2,3", arts[1].Number, arts[2].Number)
	}
	for _, a := range arts {
		if a.Text == "" {
			t.Errorf("article %q has empty text", a.Number)
		}
		if a.Label == "" {
			t.Errorf("article %q has empty label", a.Number)
		}
	}
}

func TestParseArticles_noArticles(t *testing.T) {
	// The Angular shell Legilux serves for PDF-only acts has no richtext_article.
	shell := []byte(`<!DOCTYPE html><html><body><app-root></app-root></body></html>`)
	arts, err := ParseArticles(shell)
	if err != nil {
		t.Fatal(err)
	}
	if arts != nil {
		t.Errorf("want nil for shell, got %v", arts)
	}
}

func TestParseArticles_skipsEmptyID(t *testing.T) {
	// A richtext_article div with an empty id (a quoted inserted article) must
	// not be emitted as a standalone article.
	in := []byte(`<html><body>
<div id="" class="richtext_article"><p class="richtext_num_article">Art. 1.</p>nested</div>
<div id="art_5" class="richtext_article"><p class="richtext_num_article">Art. 5.</p>real body</div>
</body></html>`)
	arts, err := ParseArticles(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].Number != "5" {
		t.Fatalf("want single article 5, got %+v", arts)
	}
	if arts[0].Label != "Art. 5." {
		t.Errorf("label = %q, want %q", arts[0].Label, "Art. 5.")
	}
}

func TestParseArticles_labelFallback(t *testing.T) {
	// An article div lacking a num paragraph falls back to "Art. <num>".
	in := []byte(`<html><body><div id="art_7" class="richtext_article">body only</div></body></html>`)
	arts, err := ParseArticles(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].Label != "Art. 7" {
		t.Fatalf("want fallback label 'Art. 7', got %+v", arts)
	}
}

func TestFullTextURL(t *testing.T) {
	w := "http://data.legilux.public.lu/eli/etat/leg/rgd/2022/01/28/a45/jo"
	if got := FullTextURL(w); got != w+"/fr/html" {
		t.Errorf("FullTextURL = %q", got)
	}
	if got := FullTextURL(w + "/"); got != w+"/fr/html" {
		t.Errorf("FullTextURL trailing slash = %q", got)
	}
}
