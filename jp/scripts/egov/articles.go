package egov

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tggo/lex/internal/schema"
)

// lawDataResponse is the envelope of GET /api/2/law_data/{id}. Only the
// structured body is decoded.
type lawDataResponse struct {
	LawFullText node `json:"law_full_text"`
}

// node mirrors e-Gov's XML-as-JSON shape: every element is {tag, attr,
// children}, where each child is either a JSON string (text) or another node.
type node struct {
	Tag      string            `json:"tag"`
	Attr     map[string]string `json:"attr"`
	Children []json.RawMessage `json:"children"`
}

// child is one decoded child: exactly one of text or elem is meaningful.
type child struct {
	text   string
	elem   *node
	isText bool
}

// decodeChildren splits raw children into text vs element nodes.
func (n node) decodeChildren() []child {
	out := make([]child, 0, len(n.Children))
	for _, raw := range n.Children {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			out = append(out, child{text: s, isText: true})
			continue
		}
		var e node
		if err := json.Unmarshal(raw, &e); err == nil {
			out = append(out, child{elem: &e})
		}
	}
	return out
}

// ParseArticles decodes a law_data response and returns one schema.Article per
// 条 (Article) node, in document order. Returns zero articles (not an error)
// for bodies that have no 条 structure.
func ParseArticles(lawDataJSON []byte) ([]schema.Article, error) {
	var resp lawDataResponse
	if err := json.Unmarshal(lawDataJSON, &resp); err != nil {
		return nil, fmt.Errorf("egov: parse law_data: %w", err)
	}
	var out []schema.Article
	var walk func(n node)
	walk = func(n node) {
		if n.Tag == "Article" {
			out = append(out, toArticle(n))
			return // 条 do not nest
		}
		for _, c := range n.decodeChildren() {
			if c.elem != nil {
				walk(*c.elem)
			}
		}
	}
	walk(resp.LawFullText)
	return out, nil
}

// toArticle maps one 条 node into a schema.Article. The label is the 条 title
// plus its caption (e.g. "第一条（基本原則）"); the text is the body — every
// paragraph/item, one per line — with the caption and title omitted so they are
// not duplicated into the FTS body.
func toArticle(n node) schema.Article {
	var title, caption string
	var blocks []string
	for _, c := range n.decodeChildren() {
		if c.elem == nil {
			continue
		}
		switch c.elem.Tag {
		case "ArticleTitle":
			title = leafText(*c.elem)
		case "ArticleCaption":
			caption = leafText(*c.elem)
		default:
			if t := strings.TrimSpace(leafText(*c.elem)); t != "" {
				blocks = append(blocks, t)
			}
		}
	}
	return schema.Article{
		Number: n.Attr["Num"],
		Label:  title + caption,
		Text:   strings.Join(blocks, "\n"),
	}
}

// leafText concatenates every text leaf under n, in document order. Japanese
// text has no inter-word spaces, so leaves are joined without a separator.
func leafText(n node) string {
	var b strings.Builder
	var walk func(n node)
	walk = func(n node) {
		for _, c := range n.decodeChildren() {
			if c.isText {
				b.WriteString(c.text)
			} else if c.elem != nil {
				walk(*c.elem)
			}
		}
	}
	walk(n)
	return b.String()
}
