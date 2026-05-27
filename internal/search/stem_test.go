package search

import "testing"

func TestUkrainianStemmer_families(t *testing.T) {
	st := StemmerFor("uk")
	// Each inflection family must collapse to a single stem.
	families := [][]string{
		{"оренда", "оренду", "оренди", "оренді", "орендою"},
		{"спадщина", "спадщини", "спадщину", "спадщині"},
		{"реєстрація", "реєстрації", "реєстрацію", "реєстрацій"},
		{"контроль", "контролю", "контролі"},
		{"земля", "землю", "землі"},
		{"правовий", "правова", "правові"},
	}
	for _, fam := range families {
		want := st.Stem(fam[0])
		for _, w := range fam[1:] {
			if got := st.Stem(w); got != want {
				t.Errorf("stem(%q)=%q, stem(%q)=%q — family should collapse", fam[0], want, w, got)
			}
		}
	}
}

func TestUkrainianStemmer_doesNotOvermerge(t *testing.T) {
	st := StemmerFor("uk")
	// Unrelated roots must keep distinct stems.
	pairs := [][2]string{
		{"оренда", "продаж"},
		{"закон", "земля"},
		{"спадщина", "реєстрація"},
	}
	for _, p := range pairs {
		if st.Stem(p[0]) == st.Stem(p[1]) {
			t.Errorf("stem(%q)==stem(%q)=%q — should differ", p[0], p[1], st.Stem(p[0]))
		}
	}
}

func TestUkrainianStemmer_shortAndNonWord(t *testing.T) {
	st := StemmerFor("uk")
	if st.Stem("рік") != "рік" { // <4 runes: unchanged
		t.Errorf("short word changed: %q", st.Stem("рік"))
	}
	if st.Stem("435") != "435" {
		t.Errorf("digits changed: %q", st.Stem("435"))
	}
}

func TestIdentityStemmer(t *testing.T) {
	st := StemmerFor("en") // no stemmer → identity
	if st.Stem("Renting") != "Renting" {
		t.Errorf("identity changed token: %q", st.Stem("Renting"))
	}
}
