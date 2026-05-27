package fedlex

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/tggo/lex/internal/schema"
)

// ParseArticles extracts article structure from a Fedlex Akoma Ntoso XML
// document (the de/xml manifestation of an SR consolidation; the importer
// resolves that file's URL from the SPARQL endpoint). Each <article> element
// becomes one schema.Article: the number is taken from its <num> ("Art. 12a" →
// "12a"), the label is the full <num> text, and the text is the concatenation
// of the article's paragraph/content prose.
//
// Footnotes (<authorialNote>) are dropped from the text — they are editorial
// amendment provenance, not the operative legal text — so the article text
// reads as the consolidated provision. The parser is pure and offline; HTTP
// fetching lives in the importer.
func ParseArticles(xmlBytes []byte) ([]schema.Article, error) {
	dec := xml.NewDecoder(bytes.NewReader(xmlBytes))
	var arts []schema.Article
	for {
		tok, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("fedlex: parse akn xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "article" {
			continue
		}
		art, perr := parseArticle(dec)
		if perr != nil {
			return nil, fmt.Errorf("fedlex: parse akn xml: %w", perr)
		}
		if art.Number != "" || art.Text != "" {
			arts = append(arts, art)
		}
	}
	if len(arts) == 0 {
		return nil, nil
	}
	return arts, nil
}

// parseArticle consumes the token stream of a single <article> element (the
// decoder is positioned just after its StartElement) and returns the parsed
// article. The <num> child supplies the label/number; everything else
// contributes prose, except <authorialNote> subtrees, which are skipped.
func parseArticle(dec *xml.Decoder) (schema.Article, error) {
	var num strings.Builder
	var body strings.Builder
	depth := 1 // depth 1 == directly inside <article>
	inNum := 0 // depth at which the article's own <num> opened, else 0
	skip := 0  // >0 while inside an <authorialNote> element (text dropped)
	skipDepth := 0

	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			return schema.Article{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch t.Name.Local {
			// Only the article's direct <num> child (depth 2) is its label;
			// deeper <num> elements belong to paragraphs and stay in the body.
			case "num":
				if skip == 0 && inNum == 0 && depth == 2 {
					inNum = depth
				}
			case "authorialNote":
				if skip == 0 {
					skip++
					skipDepth = depth
				}
			}
		case xml.EndElement:
			if skip > 0 && depth == skipDepth {
				skip = 0
				skipDepth = 0
			}
			if inNum > 0 && depth == inNum {
				inNum = 0
			}
			depth--
		case xml.CharData:
			if skip > 0 {
				continue
			}
			text := string(t)
			if inNum > 0 {
				num.WriteString(text)
			} else {
				// Separate adjacent text runs from different elements so a
				// paragraph number ("2") never glues to the preceding
				// sentence; normalizeSpace collapses the extra spaces.
				body.WriteByte(' ')
				body.WriteString(text)
			}
		}
	}

	label := normalizeSpace(num.String())
	return schema.Article{
		Number: numberFromLabel(label),
		Label:  label,
		Text:   normalizeSpace(body.String()),
	}, nil
}

// numberFromLabel extracts the bare article number from a <num> label such as
// "Art. 12a" → "12a", "Art. 1" → "1". It strips a leading "Art." token (any
// case, with or without the period) and any trailing punctuation, and collapses
// the remainder; if no recognizable number is present it returns the trimmed
// label as-is so the article is still addressable.
func numberFromLabel(label string) string {
	s := strings.TrimSpace(label)
	low := strings.ToLower(s)
	for _, p := range []string{"art.", "art"} {
		if strings.HasPrefix(low, p) {
			s = strings.TrimSpace(s[len(p):])
			break
		}
	}
	s = strings.TrimRight(s, ". ")
	return strings.Join(strings.Fields(s), " ")
}

// normalizeSpace collapses all whitespace runs to single spaces and trims.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
