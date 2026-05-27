package schema

import (
	"testing"
	"time"
)

func TestResourceURI(t *testing.T) {
	got := ResourceURI("ua", "kodeks", 2003, "435-15")
	want := "https://lex.dev/eli/ua/kodeks/2003/435-15"
	if got != want {
		t.Errorf("ResourceURI = %q, want %q", got, want)
	}
}

func TestResourceURI_escapesSlashInNumber(t *testing.T) {
	// Constitution id "254к/96-вр" contains a slash that must not split the path.
	got := ResourceURI("ua", "konstytutsiya", 1996, "254к/96-вр")
	// url.PathEscape percent-encodes '/' and all non-ASCII, giving a
	// deterministic, portable, single-segment id. Human URL lives in sourceURL.
	want := "https://lex.dev/eli/ua/konstytutsiya/1996/254%D0%BA%2F96-%D0%B2%D1%80"
	if got != want {
		t.Errorf("ResourceURI = %q, want %q", got, want)
	}
}

func TestExpressionAndArticleURI(t *testing.T) {
	res := ResourceURI("ua", "kodeks", 2003, "435-15")
	vd := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expr := ExpressionURI(res, vd, "uk")
	if expr != res+"/2026-01-01/uk" {
		t.Fatalf("ExpressionURI = %q", expr)
	}
	if art := ArticleURI(expr, "1"); art != expr+"/art_1" {
		t.Errorf("ArticleURI = %q", art)
	}
}

func TestStatusInForceURI(t *testing.T) {
	if StatusInForce.InForceURI() != InForceInForce {
		t.Error("StatusInForce should map to InForce individual")
	}
	if StatusRepealed.InForceURI() != InForceNotInForce {
		t.Error("StatusRepealed should map to NotInForce individual")
	}
	if StatusUnknown.InForceURI() != "" {
		t.Error("StatusUnknown should map to empty")
	}
}

func TestActURIHelpers(t *testing.T) {
	a := &Act{
		Country:  "ua",
		TypeSlug: "kodeks",
		Year:     2003,
		Number:   "435-15",
		Expression: &Expression{
			VersionDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			LangTag:     "uk",
		},
	}
	if a.ResourceURI() != "https://lex.dev/eli/ua/kodeks/2003/435-15" {
		t.Errorf("Act.ResourceURI = %q", a.ResourceURI())
	}
	if a.ExpressionURI() != a.ResourceURI()+"/2026-01-01/uk" {
		t.Errorf("Act.ExpressionURI = %q", a.ExpressionURI())
	}
}
