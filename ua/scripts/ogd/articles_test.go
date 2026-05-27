package ogd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseArticles_golden(t *testing.T) {
	b := readFixture(t, "act_articles.sample.htm")
	arts, err := ParseArticles(b)
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	if len(arts) != 2 {
		t.Fatalf("got %d articles, want 2", len(arts))
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
		t.Errorf("articles mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseArticles_numbersAndBodies(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "act_articles.sample.htm"))
	if err != nil {
		t.Fatal(err)
	}
	if arts[0].Number != "1" || arts[1].Number != "2" {
		t.Errorf("numbers = %q, %q; want 1, 2", arts[0].Number, arts[1].Number)
	}
	// Heading title is captured in the label; body text is non-empty.
	if !bytes.Contains([]byte(arts[0].Label), []byte("Визначення термінів")) {
		t.Errorf("article 1 label = %q", arts[0].Label)
	}
	if arts[0].Text == "" {
		t.Error("article 1 has empty body text")
	}
}

func TestParseArticles_edgeCases(t *testing.T) {
	// No headings → no articles; preamble is ignored.
	noArts, err := ParseArticles([]byte(`<html><body><p>Преамбула без статей.</p></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(noArts) != 0 {
		t.Errorf("expected 0 articles, got %d", len(noArts))
	}

	// Inserted article number "1-1" and multi-paragraph body.
	in := `<html><body>
<p><span>Стаття 1-1. </span>Вставлена</p>
<p>Перший абзац.</p>
<p>Другий абзац.</p>
</body></html>`
	arts, err := ParseArticles([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 || arts[0].Number != "1-1" {
		t.Fatalf("got %+v, want one article numbered 1-1", arts)
	}
	if arts[0].Text != "Перший абзац.\nДругий абзац." {
		t.Errorf("body = %q", arts[0].Text)
	}
}
