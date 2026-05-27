package frl

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/tggo/lex/internal/schema"
)

// ParseArticles extracts section structure from one of an act's EPUB document
// HTML files (the OPC-generated `.../epub/OEBPS/document_N/document_N.html`
// served by the Federal Register of Legislation). Each section begins with a
// `<p class="ActHead5">` heading whose `<span class="CharSectno">` carries the
// section number; the section body is every paragraph that follows up to the
// next section heading. Material before the first section heading (cover page,
// table of contents, long title) is ignored.
//
// It is pure and offline: the importer fetches the bytes; mapping lives here so
// it can be golden-tested without the network.
func ParseArticles(htmlBytes []byte) ([]schema.Article, error) {
	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, fmt.Errorf("frl: parse document html: %w", err)
	}
	return splitSections(collectBlocks(doc)), nil
}

// block is one paragraph-level element: either a section heading (its number
// and heading text) or a run of body text.
type block struct {
	secNum  string // non-empty => this block opens a new section
	secHead string // heading text for a section block
	text    string // body text for a non-section block
}

// collectBlocks walks the document in order, emitting one block per <p>. A <p
// class="ActHead5"> becomes a heading block; any other <p> with visible text
// becomes a body block. Table cells are flattened into body text so penalty
// tables and commencement tables are not lost.
func collectBlocks(root *html.Node) []block {
	var blocks []block
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "p" {
			if hasClass(n, "ActHead5") {
				num, head := sectionHeading(n)
				if num != "" {
					blocks = append(blocks, block{secNum: num, secHead: head})
					return
				}
			}
			if t := nodeText(n); t != "" {
				blocks = append(blocks, block{text: t})
			}
			return // a <p> holds no nested <p>; its text is captured
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return blocks
}

// splitSections turns the ordered block stream into articles: each heading
// block opens an article; following body blocks accumulate into its text until
// the next heading.
func splitSections(blocks []block) []schema.Article {
	var arts []schema.Article
	cur := -1
	for _, b := range blocks {
		if b.secNum != "" {
			label := "Section " + b.secNum
			if b.secHead != "" {
				label += " " + b.secHead
			}
			arts = append(arts, schema.Article{Number: b.secNum, Label: label})
			cur = len(arts) - 1
			continue
		}
		if cur < 0 {
			continue // preamble before the first section
		}
		if arts[cur].Text != "" {
			arts[cur].Text += "\n"
		}
		arts[cur].Text += b.text
	}
	if len(arts) == 0 {
		return nil
	}
	return arts
}

// sectionHeading reads a `<p class="ActHead5">` heading and returns its section
// number (from the `CharSectno` span) and the remaining heading text. A heading
// with no CharSectno (e.g. an unnumbered Part/Division head reusing the class)
// yields ("", "").
func sectionHeading(n *html.Node) (num, head string) {
	var headParts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "span" {
			t := nodeText(n)
			if hasClass(n, "CharSectno") {
				num = strings.TrimSpace(t)
			} else if t != "" {
				headParts = append(headParts, t)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	if num == "" {
		return "", ""
	}
	return num, strings.Join(strings.Fields(strings.Join(headParts, " ")), " ")
}

// hasClass reports whether n carries the given class token.
func hasClass(n *html.Node, class string) bool {
	for _, a := range n.Attr {
		if a.Key != "class" {
			continue
		}
		for _, c := range strings.Fields(a.Val) {
			if c == class {
				return true
			}
		}
	}
	return false
}

// nodeText returns the concatenated text of a node's subtree with runs of
// whitespace (including the non-breaking spaces the OPC HTML uses for
// indentation) collapsed to single spaces.
func nodeText(n *html.Node) string {
	var b strings.Builder
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(n)
	return strings.Join(strings.Fields(strings.ReplaceAll(b.String(), " ", " ")), " ")
}
