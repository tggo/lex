// Package uslm parses United States Code USLM (United States Legislative
// Markup) XML — the official per-title bulk format published by the Office of
// the Law Revision Counsel (uscode.house.gov) — into lex schema.Act values. It
// is pure and offline: HTTP fetching lives in the importer CLI, parsing/mapping
// lives here so it can be golden-tested without the network. See ADR-0020 and
// us/README.md for the format and the legal basis (US Government edicts are in
// the public domain).
//
// Each USC title is modeled as one lex Act (the "work"); its USLM <section>
// elements become lex:Article nodes. Title/chapter context is preserved in the
// article label so search keeps the structural breadcrumb.
package uslm

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for the United States (federal).
const Country schema.CountryCode = "us"

// ELI language metadata for US English.
const (
	langTag    = "en"
	langAlpha3 = "ENG"
)

// Document is the root <uslm> element of a US Code title file.
type Document struct {
	XMLName xml.Name `xml:"uslm"`
	Meta    Meta     `xml:"meta"`
	Main    Main     `xml:"main"`
}

// Meta is the <meta> block carrying publication metadata.
type Meta struct {
	DocNumber          string `xml:"docNumber"`
	DocPublicationName string `xml:"docPublicationName"`
	Title              string `xml:"title"`    // dc:title, e.g. "Title 1 - General Provisions"
	Created            string `xml:"created"`  // dcterms:created, YYYY-MM-DD
	Modified           string `xml:"modified"` // dcterms:modified, RFC3339-ish
}

// Main wraps the document body; a per-title file has exactly one <title>.
type Main struct {
	Title Title `xml:"title"`
}

// Title is a USC title (the act/work).
type Title struct {
	Identifier string    `xml:"identifier,attr"` // e.g. "/us/usc/t1"
	Num        Num       `xml:"num"`
	Heading    string    `xml:"heading"`
	Chapters   []Chapter `xml:"chapter"`
	// Some titles place sections directly under the title (no chapter).
	Sections []Section `xml:"section"`
}

// Chapter groups sections within a title.
type Chapter struct {
	Identifier string    `xml:"identifier,attr"`
	Num        Num       `xml:"num"`
	Heading    string    `xml:"heading"`
	Sections   []Section `xml:"section"`
}

// Section is a single USC section — mapped to a lex:Article.
type Section struct {
	Identifier  string  `xml:"identifier,attr"` // e.g. "/us/usc/t1/s1"
	Status      string  `xml:"status,attr"`     // e.g. "repealed" (absent = operative)
	StartPeriod string  `xml:"startPeriod,attr"`
	Num         Num     `xml:"num"`
	Heading     string  `xml:"heading"`
	Content     Content `xml:"content"`
}

// Num is a numbered-element label carrying the @value and display text.
type Num struct {
	Value string `xml:"value,attr"`
	Text  string `xml:",chardata"`
}

// Content holds the section body; we keep the flattened text only.
type Content struct {
	Inner string `xml:",innerxml"`
}

// ParseDocument decodes a USLM per-title XML file.
func ParseDocument(b []byte) (*Document, error) {
	var d Document
	if err := xml.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("uslm: parse document: %w", err)
	}
	if d.XMLName.Local != "uslm" {
		return nil, fmt.Errorf("uslm: root element is %q, want uslm", d.XMLName.Local)
	}
	return &d, nil
}

// titleNumber extracts the USC title number from the title @identifier
// ("/us/usc/t1" → "1"). Falls back to the <num value="…">.
func titleNumber(t *Title) string {
	if n, ok := strings.CutPrefix(t.Identifier, "/us/usc/t"); ok && n != "" {
		return n
	}
	return strings.TrimSpace(t.Num.Value)
}

// sectionNumber returns the USC section number, preferring the <num @value>.
func sectionNumber(s *Section) string {
	if v := strings.TrimSpace(s.Num.Value); v != "" {
		return v
	}
	// Fall back to the tail of the identifier ("/us/usc/t1/s1" → "1").
	if i := strings.LastIndex(s.Identifier, "/s"); i >= 0 {
		return s.Identifier[i+2:]
	}
	return ""
}

// statusOf maps a section's @status to a schema.Status. USLM marks repealed,
// omitted, transferred, etc. via @status; an absent status is operative.
func statusOf(status string) schema.Status {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return schema.StatusInForce
	case "operational", "operative":
		return schema.StatusInForce
	case "repealed", "omitted", "transferred", "renumbered", "expired", "eliminated":
		return schema.StatusRepealed
	default:
		return schema.StatusUnknown
	}
}

// parseDate parses a YYYY-MM-DD date (UTC). Empty/invalid input yields zero.
func parseDate(s string) time.Time {
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// versionDate is the MANDATORY as-of date. USLM per-title files are release
// points: dcterms:modified / dcterms:created in <meta> record when the title
// text was published. We prefer dcterms:modified (the release-point date),
// falling back to dcterms:created, then to the first section's @startPeriod.
// Documented in ADR-0020.
func versionDate(d *Document) time.Time {
	if t := parseDate(d.Meta.Modified); !t.IsZero() {
		return t
	}
	if t := parseDate(d.Meta.Created); !t.IsZero() {
		return t
	}
	for _, s := range allSections(&d.Main.Title) {
		if t := parseDate(s.StartPeriod); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// allSections returns every <section> under a title, whether nested in chapters
// or placed directly under the title, in document order.
func allSections(t *Title) []Section {
	var out []Section
	out = append(out, t.Sections...)
	for _, ch := range t.Chapters {
		out = append(out, ch.Sections...)
	}
	return out
}

// titleStatus collapses the title's section statuses into a work-level status:
// in force if any section is operative, repealed only if every section is gone.
func titleStatus(secs []Section) schema.Status {
	any := false
	allRepealed := true
	for _, s := range secs {
		any = true
		if statusOf(s.Status) != schema.StatusRepealed {
			allRepealed = false
		}
	}
	if !any {
		return schema.StatusUnknown
	}
	if allRepealed {
		return schema.StatusRepealed
	}
	return schema.StatusInForce
}

// flatten reduces innerxml to plain text: strips tags, decodes entities, and
// collapses whitespace — the same shape the FTS index wants.
func flatten(inner string) string {
	if inner == "" {
		return ""
	}
	var b strings.Builder
	dec := xml.NewDecoder(strings.NewReader("<root>" + inner + "</root>"))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok {
			b.Write(cd)
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Articles builds lex:Article nodes from the title's sections, in document
// order, skipping sections without a usable number.
func Articles(t *Title) []schema.Article {
	secs := allSections(t)
	out := make([]schema.Article, 0, len(secs))
	for i := range secs {
		s := &secs[i]
		num := sectionNumber(s)
		if num == "" {
			continue
		}
		label := strings.TrimSpace(s.Num.Text)
		if h := strings.TrimSpace(s.Heading); h != "" {
			if label != "" {
				label += " " + h
			} else {
				label = h
			}
		}
		out = append(out, schema.Article{
			Number: num,
			Label:  label,
			Text:   flatten(s.Content.Inner),
		})
	}
	return out
}

// ToAct assembles a schema.Act from a parsed USLM title document.
// retrievedAt is recorded as lex:retrievedAt; sourceURL is the OLRC download
// path the importer fetched the title from.
func ToAct(d *Document, sourceURL string, retrievedAt time.Time) (*schema.Act, error) {
	t := &d.Main.Title
	num := titleNumber(t)
	if num == "" {
		return nil, fmt.Errorf("uslm: cannot determine title number from %q", t.Identifier)
	}

	title := strings.TrimSpace(d.Meta.Title)
	if title == "" {
		// Compose from the title's own num + heading, e.g. "Title 1—GENERAL …".
		title = strings.TrimSpace(strings.TrimSpace(t.Num.Text) + " " + strings.TrimSpace(t.Heading))
	}

	secs := allSections(t)
	exp := &schema.Expression{
		Title:       title,
		LangTag:     langTag,
		LangAlpha3:  langAlpha3,
		VersionDate: versionDate(d),
		Status:      titleStatus(secs),
		SourceURL:   sourceURL,
		RetrievedAt: retrievedAt,
		Articles:    Articles(t),
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   "usc-title",
		Year:       versionDate(d).Year(),
		Number:     "title-" + num,
		IDLocal:    "usc/title-" + num,
		Expression: exp,
	}, nil
}
