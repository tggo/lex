package search

import (
	"sort"
	"strings"
	"unicode"
)

// Stemmer reduces an inflected token to a stem so that morphological variants
// of a word match each other. Stemming is language-specific, so the index
// records its language and selects the matching stemmer; serving reuses it.
// See docs/TODO.md — this must be filled in per country/language.
type Stemmer interface {
	Stem(token string) string
}

// identityStemmer is the default for languages without a stemmer yet: it leaves
// tokens unchanged (behaves like plain unicode61 matching).
type identityStemmer struct{}

func (identityStemmer) Stem(t string) string { return t }

// StemmerFor returns the stemmer for a BCP-47 language code, or the identity
// stemmer if none is registered.
func StemmerFor(lang string) Stemmer {
	switch strings.ToLower(lang) {
	case "uk":
		return ukrainianStemmer{}
	default:
		return identityStemmer{}
	}
}

// --- Ukrainian ---

// ukrainianStemmer is a lightweight, conservative suffix-stripping stemmer for
// Ukrainian: it removes the most common inflectional endings so case/number
// variants of nouns and adjectives collapse (оренда/оренду/оренди → оренд,
// реєстрація/реєстрації → реєстрац). It is intentionally cautious — it only
// strips when a vowel-bearing stem of at least 3 letters remains — so it favours
// recall without aggressive over-merging. Not a full morphological analyser.
type ukrainianStemmer struct{}

var ukVowels = map[rune]bool{'а': true, 'е': true, 'и': true, 'і': true, 'о': true, 'у': true, 'ю': true, 'я': true, 'є': true, 'ї': true}

// ukEndings is sorted by descending rune length in init so the longest ending
// matches first.
var ukEndings = []string{
	"ами", "ями", "ого", "ому", "ими", "іми", "ією", "іях", "іям",
	"ах", "ях", "ам", "ям", "ів", "ом", "ем", "ою", "ею", "их", "ій",
	"ім", "ої", "ія", "ії", "ію", "ей", "єю", "ий",
	"а", "я", "и", "і", "о", "у", "ю", "е", "й", "ь", "ї", "є",
}

func init() {
	sort.SliceStable(ukEndings, func(i, j int) bool {
		return len([]rune(ukEndings[i])) > len([]rune(ukEndings[j]))
	})
}

func (ukrainianStemmer) Stem(token string) string {
	w := []rune(strings.ToLower(token))
	if len(w) < 4 {
		return string(w) // too short to strip safely
	}
	for _, suf := range ukEndings {
		sr := []rune(suf)
		stem := len(w) - len(sr)
		if stem < 3 || !hasRuneSuffix(w, sr) {
			continue
		}
		if containsUkVowel(w[:stem]) {
			w = w[:stem]
			break
		}
	}
	if len(w) > 3 && w[len(w)-1] == 'ь' {
		w = w[:len(w)-1]
	}
	return string(w)
}

func hasRuneSuffix(w, suf []rune) bool {
	if len(suf) > len(w) {
		return false
	}
	off := len(w) - len(suf)
	for i := range suf {
		if w[off+i] != suf[i] {
			return false
		}
	}
	return true
}

func containsUkVowel(rs []rune) bool {
	for _, r := range rs {
		if ukVowels[r] {
			return true
		}
	}
	return false
}

// --- tokenization shared by indexing, querying, and snippets ---

// tokenize splits text into word tokens on any non-letter, non-digit rune.
func tokenize(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// stemColumn produces the space-joined stems indexed for a piece of text.
func stemColumn(st Stemmer, text string) string {
	toks := tokenize(text)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		out = append(out, st.Stem(strings.ToLower(t)))
	}
	return strings.Join(out, " ")
}
