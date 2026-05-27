package ogd

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"github.com/tggo/lex/internal/schema"
)

// headingRe matches an article heading paragraph like "Стаття 1. Визначення
// термінів". Group 1 is the article number (e.g. "1", "1-1"); group 2 is the
// heading's title text, if any.
var headingRe = regexp.MustCompile(`^Стаття\s+([^\s.]+)\.?\s*(.*)$`)

// ParseArticles extracts article structure from an act's HTML body (the
// data.rada.gov.ua `text/d<dokid>.htm` format). Each "Стаття N" heading starts
// an article; following paragraphs up to the next heading are its text. Content
// before the first heading (preamble) is ignored.
func ParseArticles(htmlBytes []byte) ([]schema.Article, error) {
	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, fmt.Errorf("ogd: parse html: %w", err)
	}
	paras := collectParagraphs(doc)
	return splitArticles(paras), nil
}

// collectParagraphs returns the normalized text of each <p> element, in order.
func collectParagraphs(root *html.Node) []string {
	var paras []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "p" {
			if t := nodeText(n); t != "" {
				paras = append(paras, t)
			}
			return // a <p> holds no nested <p>; its text is already captured
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return paras
}

// nodeText returns the concatenated text of a node's subtree with runs of
// whitespace collapsed to single spaces.
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
	return strings.Join(strings.Fields(b.String()), " ")
}

func splitArticles(paras []string) []schema.Article {
	var arts []schema.Article
	cur := -1
	for _, p := range paras {
		if m := headingRe.FindStringSubmatch(p); m != nil {
			arts = append(arts, schema.Article{
				Number: strings.TrimSuffix(m[1], "."),
				Label:  p,
			})
			cur = len(arts) - 1
			continue
		}
		if cur >= 0 {
			if arts[cur].Text != "" {
				arts[cur].Text += "\n"
			}
			arts[cur].Text += p
		}
	}
	return arts
}
