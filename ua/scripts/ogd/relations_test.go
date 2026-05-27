package ogd

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tggo/lex/internal/schema"
)

func TestParseLinks(t *testing.T) {
	got := ParseLinks("1468#1:57|6:11|29:1##")
	if len(got) != 1 || got[0].TargetDokid != 1468 {
		t.Fatalf("got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Codes, []int{1, 6, 29}) {
		t.Errorf("codes = %v, want [1 6 29]", got[0].Codes)
	}
	if ParseLinks("") != nil || ParseLinks("##") != nil {
		t.Error("empty links should yield nil")
	}
	// Target with no codes.
	one := ParseLinks("597##")
	if len(one) != 1 || one[0].TargetDokid != 597 || len(one[0].Codes) != 0 {
		t.Errorf("got %+v", one)
	}
}

func TestParseDocIndex(t *testing.T) {
	idx, err := ParseDocIndex(strings.NewReader(string(readFixture(t, "doc.sample.txt"))))
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := idx[1468]
	if !ok {
		t.Fatal("dokid 1468 missing")
	}
	if ref.Nreg != "435-15" || ref.TypeCode != 21 || ref.Year != 2003 {
		t.Errorf("ref = %+v", ref)
	}
	if ref.ResourceURI() != schema.ResourceURI("ua", "kodeks", 2003, "435-15") {
		t.Errorf("uri = %q", ref.ResourceURI())
	}
}

func TestResolveRelations(t *testing.T) {
	idx, _ := ParseDocIndex(strings.NewReader(string(readFixture(t, "doc.sample.txt"))))

	// Act amends(1), refers(6) and partially-repeals(29) target 1468.
	amends, repeals, cites := ResolveRelations("1468#1:57|6:11|29:1##", idx)
	kodeks := schema.ResourceURI("ua", "kodeks", 2003, "435-15")
	if !reflect.DeepEqual(amends, []string{kodeks}) {
		t.Errorf("amends = %v", amends)
	}
	if !reflect.DeepEqual(repeals, []string{kodeks}) {
		t.Errorf("repeals = %v", repeals)
	}
	if !reflect.DeepEqual(cites, []string{kodeks}) {
		t.Errorf("cites = %v", cites)
	}

	// Ratifies(17) target 597 -> a generic citation.
	_, _, cites = ResolveRelations("597#17:1##", idx)
	zakon := schema.ResourceURI("ua", "zakon", 1996, "2456-12")
	if !reflect.DeepEqual(cites, []string{zakon}) {
		t.Errorf("cites = %v, want [%s]", cites, zakon)
	}

	// Unknown target, and a target without a year, are skipped.
	a, r, c := ResolveRelations("12345#1:1##", idx)
	if a != nil || r != nil || c != nil {
		t.Errorf("unknown target should resolve to nothing, got %v %v %v", a, r, c)
	}
	if _, _, c := ResolveRelations("9999#6:1##", idx); c != nil {
		t.Errorf("year-less target should be skipped, got %v", c)
	}
}

func TestTypeAndRelationMaps(t *testing.T) {
	if slugForType(1) != "zakon" || slugForType(21) != "kodeks" || slugForType(99999) != "akt" {
		t.Error("type slug mapping wrong")
	}
	if kindOf(1) != relAmend || kindOf(4) != relRepeal || kindOf(6) != relCite {
		t.Error("relation kind mapping wrong")
	}
}
