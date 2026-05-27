package legilux

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"github.com/tggo/lex/internal/schema"
)

// FullTextPath is the suffix appended to a work URI to reach its French HTML
// manifestation, the machine-readable full text Legilux serves from its
// filestore (XHTML with richtext_* classes). Acts that have no HTML embodiment
// (e.g. old scanned Mémorial pages that exist only as PDF) return the site's
// Angular shell instead, which contains no richtext_article elements and parses
// to zero articles. See lu/README.md.
const FullTextPath = "/fr/html"

// FullTextURL returns the French HTML manifestation URL for a work URI.
func FullTextURL(workURI string) string {
	return strings.TrimRight(workURI, "/") + FullTextPath
}

// attr returns the value of the first attribute whose key matches name
// case-insensitively, or "".
func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// hasClass reports whether n's class attribute contains the given class token.
func hasClass(n *html.Node, class string) bool {
	for _, f := range strings.Fields(attr(n, "class")) {
		if f == class {
			return true
		}
	}
	return false
}

// artNumRE recovers the article number from a Legilux article id such as
// "art_1er" → "1er", "art_2" → "2", "art_2bis" → "2bis".
var artNumRE = regexp.MustCompile(`^art_(.+)$`)

// ParseArticles extracts an act's articles from its French HTML manifestation.
// Each article is a `<div class="richtext_article">` carrying a non-empty
// `id="art_<num>"` (the act's own articles; nested quoted articles inserted by
// an amendment carry an empty id and are kept as part of their host article's
// text, not emitted separately). The number comes from the id; the label is the
// leading "richtext_num_article" paragraph (e.g. "Art. 1er."); the text is the
// article's full visible text. Returns nil if the document has no such articles
// (e.g. an administrative notice or a PDF-only act's Angular shell).
func ParseArticles(body []byte) ([]schema.Article, error) {
	root, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}

	var arts []schema.Article
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && strings.EqualFold(n.Data, "div") && hasClass(n, "richtext_article") {
			m := artNumRE.FindStringSubmatch(attr(n, "id"))
			if m != nil {
				num := strings.TrimSpace(m[1])
				if num != "" {
					arts = append(arts, articleFrom(n, num))
					return // do not descend: nested quoted articles belong to this one's text
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	if len(arts) == 0 {
		return nil, nil
	}
	return arts, nil
}

// articleFrom builds an Article from a richtext_article div with number num.
func articleFrom(div *html.Node, num string) schema.Article {
	label := strings.TrimSpace(normalizeSpace(numArticleText(div)))
	if label == "" {
		label = "Art. " + num
	}
	return schema.Article{
		Number: num,
		Label:  label,
		Text:   normalizeSpace(textOf(div)),
	}
}

// numArticleText returns the visible text of the first descendant paragraph
// classed "richtext_num_article" (the "Art. N." heading), or "".
func numArticleText(n *html.Node) string {
	var found string
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.ElementNode && hasClass(n, "richtext_num_article") {
			found = textOf(n)
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(n)
	return found
}

// textOf returns the concatenated text content of a subtree.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			b.WriteByte(' ')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// normalizeSpace collapses whitespace runs (including the zero-width space and
// non-breaking space Legilux sprinkles into anchors) to single ASCII spaces.
func normalizeSpace(s string) string {
	s = strings.NewReplacer("​", " ", " ", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
