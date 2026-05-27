package legi

import (
	"strings"

	"github.com/tggo/lex/internal/schema"
)

// articleText collapses runs of whitespace (incl. non-breaking spaces) in a
// LEGI <CONTENU> body to single spaces, mirroring the PL nodeText behaviour.
func articleText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// label builds a human article label, e.g. "Article 1".
func label(num string) string {
	num = strings.TrimSpace(num)
	if num == "" {
		return "Article"
	}
	return "Article " + num
}

// ToArticle maps one parsed LEGI ARTICLE into a schema.Article.
func ToArticle(a *Article) schema.Article {
	return schema.Article{
		Number: strings.TrimSpace(a.Meta.Num),
		Label:  label(a.Meta.Num),
		Text:   articleText(a.Contenu),
	}
}

// BuildArticles orders the parsed articles by the text's STRUCT, dropping any
// LIEN_ART whose article XML was not supplied (e.g. abrogated members with no
// body). byID maps an article id (LEGIARTI…) to its parsed ARTICLE. The struct
// order is authoritative document order. It also returns the union of the
// articles' outgoing <LIENS> for relation extraction.
func BuildArticles(st *TexteStruct, byID map[string]*Article) ([]schema.Article, []Lien) {
	var arts []schema.Article
	var liens []Lien
	for _, la := range st.Liens {
		a, ok := byID[la.ID]
		if !ok {
			continue
		}
		arts = append(arts, ToArticle(a))
		liens = append(liens, a.Liens...)
	}
	return arts, liens
}
