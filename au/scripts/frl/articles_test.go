package frl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseArticles_golden(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "document.sample.html"))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	if len(arts) == 0 {
		t.Fatal("ParseArticles returned no sections")
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
	if string(got) != string(want) {
		t.Errorf("ParseArticles mismatch with golden.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseArticles_numbersAndHeadings(t *testing.T) {
	arts, err := ParseArticles(readFixture(t, "document.sample.html"))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	// The fixture is sections 1–3 of the Criminal Code Amendment (Hate Crimes)
	// Act 2025: Short title, Commencement, Schedules.
	if len(arts) != 3 {
		t.Fatalf("got %d sections, want 3", len(arts))
	}
	wantNum := []string{"1", "2", "3"}
	for i, a := range arts {
		if a.Number != wantNum[i] {
			t.Errorf("section %d number = %q, want %q", i, a.Number, wantNum[i])
		}
		if a.Text == "" {
			t.Errorf("section %s has empty text", a.Number)
		}
	}
	if arts[0].Label != "Section 1 Short title" {
		t.Errorf("section 1 label = %q", arts[0].Label)
	}
	if !contains(arts[0].Text, "Criminal Code Amendment (Hate Crimes) Act 2025") {
		t.Errorf("section 1 text missing short title: %q", arts[0].Text)
	}
}

func TestParseArticles_emptyAndPreamble(t *testing.T) {
	// No section headings => no articles (not an error).
	arts, err := ParseArticles([]byte(`<html><body><p class="LongT">An Act about things</p></body></html>`))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	if arts != nil {
		t.Errorf("expected nil for headingless doc, got %d", len(arts))
	}

	// An ActHead5 with no CharSectno (a Part/Division head reusing the class)
	// is not treated as a section.
	arts, err = ParseArticles([]byte(`<html><body><p class="ActHead5"><span>Part 1—Preliminary</span></p></body></html>`))
	if err != nil {
		t.Fatalf("ParseArticles: %v", err)
	}
	if arts != nil {
		t.Errorf("expected nil when no numbered section, got %d", len(arts))
	}
}

func TestParseDocumentList(t *testing.T) {
	dl, err := ParseDocumentList([]byte(`{"value":[
		{"titleId":"C1901A00002","start":"2026-01-22T00:00:00","format":"Word","compilationNumber":"5"},
		{"titleId":"C1901A00002","start":"2026-01-22T00:00:00","format":"Epub","compilationNumber":"5"},
		{"titleId":"C1901A00002","start":"2025-01-01T00:00:00","format":"Epub","compilationNumber":"4"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(dl.Value) != 3 {
		t.Fatalf("got %d documents", len(dl.Value))
	}
	if _, err := ParseDocumentList([]byte("{bad")); err == nil {
		t.Error("expected parse error")
	}

	// LatestEpub picks the newest Epub (the 2026-01-22 compilation #5).
	id, date, asMade, ok := LatestEpub(dl)
	if !ok {
		t.Fatal("expected an EPUB document")
	}
	if id != "C1901A00002" || date != "2026-01-22" || asMade {
		t.Errorf("LatestEpub = (%q,%q,%v), want (C1901A00002,2026-01-22,false)", id, date, asMade)
	}

	// No EPUB → not ok.
	none := &DocumentList{Value: []Document{{Format: "Pdf", Start: "2020-01-01"}}}
	if _, _, _, ok := LatestEpub(none); ok {
		t.Error("expected ok=false when no EPUB present")
	}

	// As-made (compilationNumber "0") and registerId fallback for the id.
	asm := &DocumentList{Value: []Document{
		{RegisterID: "C2025A00001", Start: "2025-02-07T00:00:00", Format: "Epub", CompilationNumber: "0"},
	}}
	gid, gdate, gAsMade, ok := LatestEpub(asm)
	if !ok || gid != "C2025A00001" || gdate != "2025-02-07" || !gAsMade {
		t.Errorf("as-made LatestEpub = (%q,%q,%v,%v)", gid, gdate, gAsMade, ok)
	}
}

func TestEpubDocPath(t *testing.T) {
	asm := EpubDocPath("C2025A00001", "2025-02-07", true, 1)
	want := "C2025A00001/asmade/2025-02-07/text/original/epub/OEBPS/document_1/document_1.html"
	if asm != want {
		t.Errorf("as-made path = %q, want %q", asm, want)
	}
	comp := EpubDocPath("C1914A00012", "2026-02-18", false, 2)
	wantC := "C1914A00012/2026-02-18/2026-02-18/text/original/epub/OEBPS/document_2/document_2.html"
	if comp != wantC {
		t.Errorf("compilation path = %q, want %q", comp, wantC)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
