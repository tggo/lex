package eisb

import (
	"strings"

	"golang.org/x/net/html"

	"github.com/tggo/lex/internal/schema"
)

// attr returns the value of the first attribute whose key matches name
// case-insensitively (eISB pages mix CONTENT/content, LANG/lang, etc.).
func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// eliPathFromURL recovers an act ELI path ("2015/act/60") from a full eISB URL
// such as ".../eli/2015/act/60/enacted/en". Returns "" if not an eISB ELI URL.
func eliPathFromURL(u string) string {
	i := strings.Index(u, "/eli/")
	if i < 0 {
		return ""
	}
	parts := strings.Split(strings.Trim(u[i+len("/eli/"):], "/"), "/")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:3], "/")
}

// extractMeta walks the document's RDFa <meta> elements and pulls the eli:*
// facts lex needs. The page expresses ELI properties via property="eli:<x>"
// with the value in either a CONTENT attribute (literals) or a resource
// attribute (URIs).
func extractMeta(root *html.Node) Meta {
	var m Meta
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "meta") {
			prop := attr(n, "property")
			resource := attr(n, "resource")
			content := attr(n, "content")
			switch strings.ToLower(prop) {
			case "eli:type_document":
				m.TypeDocument = resource
			case "eli:title":
				if m.Title == "" {
					m.Title = strings.Join(strings.Fields(content), " ")
				}
			case "eli:date_document":
				m.DateDocument = content
			case "eli:number":
				m.Number = content
			case "eli:changes":
				if p := eliPathFromURL(resource); p != "" {
					m.Changes = append(m.Changes, p)
				}
			case "eli:is_realized_by", "eli:realizes", "eli:has_part":
				if m.ELIPath == "" {
					if p := eliPathFromURL(resource); p != "" {
						m.ELIPath = p
					}
				}
			}
			// The about="eisb:2015/act/60/enacted" attribute is the most reliable
			// identity anchor; recover the ELI path from it as a fallback.
			if m.ELIPath == "" {
				if about := attr(n, "about"); strings.HasPrefix(about, "eisb:") {
					parts := strings.Split(strings.TrimPrefix(about, "eisb:"), "/")
					if len(parts) >= 3 {
						m.ELIPath = strings.Join(parts[:3], "/")
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return m
}

// extractSections walks the act body and returns its sections (Akoma-Ntoso
// <section> elements). In the eISB print HTML each section begins with an
// `<a name="secN">` anchor; its heading is the bold text immediately following,
// and its body runs until the next section anchor. Sub-paragraph anchors
// ("s1._p0") are ignored as section delimiters.
func extractSections(root *html.Node) []schema.Article {
	body := findBody(root)
	if body == nil {
		return nil
	}
	tokens := flatten(body)

	var arts []schema.Article
	var cur *schema.Article
	var buf strings.Builder
	flush := func() {
		if cur != nil {
			cur.Text = strings.Join(strings.Fields(buf.String()), " ")
			arts = append(arts, *cur)
		}
		buf.Reset()
	}
	for _, tk := range tokens {
		if tk.secNum != "" { // a section anchor starts a new section
			flush()
			a := schema.Article{Number: tk.secNum, Label: "Section " + tk.secNum}
			cur = &a
			continue
		}
		if cur != nil {
			buf.WriteString(tk.text)
			buf.WriteByte(' ')
		}
	}
	flush()
	if len(arts) == 0 {
		return nil
	}
	return arts
}

// token is either a section-start marker (secNum set) or a run of text.
type token struct {
	secNum string
	text   string
}

// flatten produces the in-order token stream of a body subtree: an `<a
// name="secN">` element emits a section marker; text nodes emit their text.
func flatten(root *html.Node) []token {
	var out []token
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "a") {
			if num := sectionAnchorNum(attr(n, "name")); num != "" {
				out = append(out, token{secNum: num})
			}
		}
		if n.Type == html.TextNode {
			if t := strings.TrimSpace(n.Data); t != "" {
				out = append(out, token{text: n.Data})
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return out
}

// sectionAnchorNum returns the section number if name is a top-level section
// anchor ("sec12" → "12"), or "" for anything else (e.g. "s1._p2", "sched1").
func sectionAnchorNum(name string) string {
	rest, ok := strings.CutPrefix(name, "sec")
	if !ok || rest == "" {
		return ""
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return rest
}

// findBody returns the <body> element, or the root if none is present.
func findBody(root *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "body") {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		return root
	}
	return found
}
