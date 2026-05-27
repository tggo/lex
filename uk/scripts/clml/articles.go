package clml

import (
	"encoding/xml"
	"sort"
	"strings"

	"github.com/tggo/lex/internal/schema"
)

// flattenText turns a section's raw inner XML into whitespace-collapsed plain
// text. CLML section bodies are deeply nested (P1para/P2/P3/Text…); rather than
// model every level we keep all descendant character data, which is exactly the
// text fed into FTS5 (lex:text).
func flattenText(inner string) string {
	dec := xml.NewDecoder(strings.NewReader("<x>" + inner + "</x>"))
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok {
			b.Write(cd)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Articles extracts one lex Article per CLML section (P1group). The section
// number comes from the P1's Pnumber; the label from the group Title; the text
// from the whole group body (sub-paragraphs, amendments, citations included).
func Articles(l *Legislation) []schema.Article {
	if len(l.P1groups) == 0 {
		return nil
	}
	out := make([]schema.Article, 0, len(l.P1groups))
	for _, g := range l.P1groups {
		num := strings.TrimSpace(g.P1.Pnumber)
		if num == "" {
			continue
		}
		out = append(out, schema.Article{
			Number: num,
			Label:  strings.TrimSpace(g.Title),
			Text:   flattenText(g.Inner),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortStrings(s []string) { sort.Strings(s) }
