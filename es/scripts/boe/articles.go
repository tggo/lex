package boe

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/tggo/lex/internal/schema"
)

// ParseArticles extracts the article-like blocks from a norm's texto XML. Each
// <bloque tipo="precepto"> becomes one schema.Article: its titulo attribute is
// the label, a leading number is derived from it, and the concatenated text of
// its latest <version> child becomes lex:text. Preamble/signature blocks
// (tipo "preambulo"/"firma") are skipped — they are not articles.
func ParseArticles(textoXML []byte) ([]schema.Article, error) {
	var doc xmlBloques
	dec := xml.NewDecoder(bytes.NewReader(textoXML))
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("boe: parse texto xml: %w", err)
	}
	var out []schema.Article
	for _, b := range doc.Data.Texto.Bloques {
		if b.Tipo != "precepto" {
			continue
		}
		text := blockText(b)
		if text == "" {
			continue
		}
		out = append(out, schema.Article{
			Number: numberFromTitle(b.Titulo, b.ID),
			Label:  strings.TrimSpace(b.Titulo),
			Text:   text,
		})
	}
	return out, nil
}

// blockText returns the collapsed text of a bloque's last <version> (the
// current consolidated wording); earlier versions are historical redactions.
func blockText(b xmlBloque) string {
	if len(b.Versions) == 0 {
		return ""
	}
	v := b.Versions[len(b.Versions)-1]
	var sb strings.Builder
	for _, p := range v.Ps {
		writeWords(&sb, p.Text)
		for _, in := range p.Inner {
			writeWords(&sb, in.Text)
		}
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

// writeWords appends s followed by a space when s carries non-space content.
func writeWords(sb *strings.Builder, s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	sb.WriteString(s)
	sb.WriteByte(' ')
}

// numberFromTitle derives a short article number from a BOE block title such as
// "Artículo 6. Código personal." → "6", or "Disposición adicional única" → its
// id when no leading numeral is present.
func numberFromTitle(title, id string) string {
	fields := strings.Fields(strings.TrimSpace(title))
	for _, f := range fields {
		f = strings.Trim(f, ".")
		if f == "" {
			continue
		}
		if isNumeric(f) {
			return f
		}
	}
	return id
}

func isNumeric(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}
