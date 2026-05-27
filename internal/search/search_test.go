package search

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/tggo/lex/internal/schema"
)

func sampleAct() *schema.Act {
	return &schema.Act{
		Country: "ua", TypeSlug: "kodeks", Year: 2003, Number: "435-15",
		Expression: &schema.Expression{
			Title:       "Цивільний кодекс України",
			LangTag:     "uk",
			VersionDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Articles: []schema.Article{
				{Number: "1", Label: "Стаття 1", Text: "Цивільні відносини регулюються цим Кодексом."},
				{Number: "2", Label: "Стаття 2", Text: "Учасниками цивільних відносин є фізичні особи."},
			},
		},
	}
}

func TestSearch_titleAndArticles(t *testing.T) {
	idx, err := OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if err := idx.AddAct(sampleAct()); err != nil {
		t.Fatal(err)
	}

	// "кодекс" appears in the title and (lowercased) in article 1 → multiple hits.
	hits, err := idx.Search("кодекс", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits for 'кодекс'")
	}
	foundTitle := false
	for _, h := range hits {
		if h.Kind == KindTitle && h.ActURI == sampleAct().ResourceURI() {
			foundTitle = true
		}
	}
	if !foundTitle {
		t.Errorf("expected a title hit, got %+v", hits)
	}

	// A term only present in article bodies returns article hits.
	hits, err = idx.Search("фізичні", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Kind != KindArticle {
		t.Fatalf("expected one article hit for 'фізичні', got %+v", hits)
	}
	if hits[0].URI != schema.ArticleURI(sampleAct().ExpressionURI(), "2") {
		t.Errorf("article hit URI = %q", hits[0].URI)
	}
}

func TestSearch_noResultsAndEmptyQuery(t *testing.T) {
	idx, _ := OpenMemory()
	defer idx.Close()
	_ = idx.AddAct(sampleAct())

	if hits, _ := idx.Search("неіснуючеслово", 10); len(hits) != 0 {
		t.Errorf("expected no hits, got %d", len(hits))
	}
	if hits, _ := idx.Search("   ", 10); hits != nil {
		t.Errorf("empty query should yield nil, got %+v", hits)
	}
}

func TestSearch_punctuationSafe(t *testing.T) {
	idx, _ := OpenMemory()
	defer idx.Close()
	_ = idx.AddAct(sampleAct())
	// Punctuation that is FTS5-special must not error.
	if _, err := idx.Search(`кодекс" OR (`, 10); err != nil {
		t.Errorf("punctuation query errored: %v", err)
	}
}

func TestAddAct_nil(t *testing.T) {
	idx, _ := OpenMemory()
	defer idx.Close()
	if err := idx.AddAct(nil); err == nil {
		t.Error("expected error for nil act")
	}
}

func TestOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fts.db")
	idx, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.AddAct(sampleAct()); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	// Reopen: data persists.
	idx2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer idx2.Close()
	if hits, _ := idx2.Search("Цивільний", 10); len(hits) == 0 {
		t.Error("expected persisted hit after reopen")
	}
}
