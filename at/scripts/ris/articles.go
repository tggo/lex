package ris

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/tggo/lex/internal/schema"
)

// risdok mirrors the RIS content XML document (namespace http://www.bka.gv.at).
// The body lives in <nutzdaten>; each <abschnitt> contains <ueberschrift>
// headings and <absatz> paragraphs. The article's substantive text is the run
// of <absatz> elements following the <ueberschrift typ="titel">Text</ueberschrift>
// marker, up to the next <ueberschrift>.
type risdok struct {
	XMLName  xml.Name   `xml:"risdok"`
	Abschnit []abschnit `xml:"nutzdaten>abschnitt"`
}

type abschnit struct {
	Nodes []bodyNode `xml:",any"`
}

// bodyNode captures any child element of an abschnitt in document order, so we
// can distinguish headings (<ueberschrift>) from paragraphs (<absatz>).
type bodyNode struct {
	XMLName xml.Name
	Typ     string `xml:"typ,attr"`
	Inner   string `xml:",innerxml"`
	Chars   string `xml:",chardata"`
}

// ParseArticleText extracts the substantive article text from one § document's
// RIS content XML. It returns a schema.Article carrying only the Text (Number and
// Label are filled by ToAct from the document metadata). The boolean is false
// when the document has no "Text" section (e.g. a head/§0 document).
func ParseArticleText(xmlBytes []byte) (schema.Article, bool, error) {
	var doc risdok
	if err := xml.Unmarshal(xmlBytes, &doc); err != nil {
		return schema.Article{}, false, fmt.Errorf("ris: parse content xml: %w", err)
	}

	var b strings.Builder
	collecting := false
	found := false
	for _, ab := range doc.Abschnit {
		for _, n := range ab.Nodes {
			switch n.XMLName.Local {
			case "ueberschrift":
				// A "titel" heading whose text is "Text" begins the body; any
				// later heading ends it.
				if collecting {
					collecting = false
				}
				if strings.EqualFold(strings.TrimSpace(stripTags(n.Inner)), "Text") {
					collecting = true
					found = true
				}
			case "absatz":
				if collecting {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(stripTags(n.Inner))
				}
			}
		}
	}
	if !found {
		return schema.Article{}, false, nil
	}
	return schema.Article{Text: normalizeSpace(b.String())}, true, nil
}

// stripTags removes any XML/HTML tags from a fragment and unescapes entities,
// leaving only the character data.
func stripTags(s string) string {
	var out strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				out.WriteRune(r)
			}
		}
	}
	return unescape(out.String())
}

// unescape resolves the handful of XML entities that appear in RIS bodies.
func unescape(s string) string {
	r := strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'",
		"&nbsp;", " ", "&#160;", " ",
	)
	return r.Replace(s)
}

// normalizeSpace collapses runs of whitespace to single spaces and trims.
func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
