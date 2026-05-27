package eli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/net/html"

	"github.com/tggo/lex/internal/schema"
)

// StructNode is a node in the document tree from GET …/struct. Articles are
// nodes with Type=="arti"; their Symbol (e.g. "arti_1") matches the id of the
// corresponding element in the text.html body.
type StructNode struct {
	ID       string       `json:"id"`
	Symbol   string       `json:"symbol"`
	Type     string       `json:"type"`
	Name     string       `json:"name"`  // e.g. "1", "10a"
	Title    string       `json:"title"` // e.g. "Art. 1."
	Children []StructNode `json:"children"`
}

// ParseStruct decodes the struct document, a top-level JSON array of nodes.
func ParseStruct(b []byte) ([]StructNode, error) {
	var ns []StructNode
	if err := json.Unmarshal(b, &ns); err != nil {
		return nil, fmt.Errorf("eli: parse struct: %w", err)
	}
	return ns, nil
}

// articleNodes walks the struct tree depth-first and returns the article nodes
// (Type=="arti") in document order.
func articleNodes(nodes []StructNode) []StructNode {
	var out []StructNode
	var walk func([]StructNode)
	walk = func(ns []StructNode) {
		for _, n := range ns {
			if n.Type == "arti" {
				out = append(out, n)
			}
			if len(n.Children) > 0 {
				walk(n.Children)
			}
		}
	}
	walk(nodes)
	return out
}

// ParseArticles extracts article structure from an act's struct tree and its
// text.html body. Each article node's Symbol is matched against the element
// carrying the exact id="<symbol>" in the HTML; that element's subtree text
// (heading plus body, including any quoted provisions) becomes lex:text.
func ParseArticles(structJSON, htmlBytes []byte) ([]schema.Article, error) {
	nodes, err := ParseStruct(structJSON)
	if err != nil {
		return nil, err
	}
	arts := articleNodes(nodes)
	if len(arts) == 0 {
		return nil, nil
	}

	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, fmt.Errorf("eli: parse html: %w", err)
	}
	byID := indexByID(doc)

	out := make([]schema.Article, 0, len(arts))
	for _, a := range arts {
		id := a.Symbol
		if id == "" {
			id = a.ID
		}
		n, ok := byID[id]
		if !ok {
			continue // no body for this article in the HTML
		}
		out = append(out, schema.Article{
			Number: a.Name,
			Label:  strings.TrimSpace(a.Title),
			Text:   nodeText(n),
		})
	}
	return out, nil
}

// indexByID maps each element's exact id attribute to its node. Matching is on
// the full attribute value, so id="arti_1" never matches id="arti_1-arti_10a".
func indexByID(root *html.Node) map[string]*html.Node {
	m := map[string]*html.Node{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if a.Key == "id" {
					if _, seen := m[a.Val]; !seen {
						m[a.Val] = n
					}
					break
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

// nodeText returns the concatenated text of a node's subtree with runs of
// whitespace (incl. non-breaking spaces) collapsed to single spaces.
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
