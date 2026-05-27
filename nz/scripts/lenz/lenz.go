// Package lenz parses New Zealand Legislation (PCO LENZ) XML into lex
// schema.Act values. It is pure and offline: HTTP fetching lives in the
// importer CLI, parsing/mapping lives here so it can be golden-tested without
// the network. See ADR-0025 and nz/README.md for the source shape and the
// legal basis (New Zealand Acts are not objects of copyright; the XML is
// published by the Parliamentary Counsel Office under CC BY 4.0 via NZGOAL).
//
// New Zealand publishes each act as an XML document under a stable, format-
// suffixed URL, e.g.
//
//	https://www.legislation.govt.nz/act/public/1990/0109/latest/whole.xml
//
// The document root is <act> carrying year/number attributes; a <cover> block
// holds the title, assent and commencement dates; the <body> holds <prov>
// provision elements whose articles are sections (<label>/<heading>/<body>).
package lenz

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for New Zealand.
const Country schema.CountryCode = "nz"

// ELI language metadata for New Zealand legislation (English).
const (
	langTag    = "en"
	langAlpha3 = "ENG"
)

// Act is the parsed LENZ act document (the root <act> element of whole.xml).
// Only the fields lex needs are decoded; the rest of the rich LENZ vocabulary
// is ignored for v1 (section text is kept verbatim inside lex:text).
type Act struct {
	XMLName xml.Name `xml:"act"`
	// Attributes on the root carry the stable identity. Different PCO exports
	// spell the year/number either as attributes or inside <cover>; we read
	// the attributes first and fall back to the cover.
	YearAttr   string `xml:"year,attr"`
	NumberAttr string `xml:"act.no,attr"`
	IDAttr     string `xml:"id,attr"`

	Cover Cover  `xml:"cover"`
	Body  Body   `xml:"body"`
	Type  string `xml:"-"` // filled from list category (public/local/private)
}

// Cover holds the act's front-matter metadata.
type Cover struct {
	Title          string `xml:"title"`
	Year           string `xml:"year"`
	Number         string `xml:"act.no"`
	DocumentType   string `xml:"document-type"`
	AssentDate     string `xml:"assent-date"`   // YYYY-MM-DD
	CommencementDt string `xml:"commencement"`  // YYYY-MM-DD, when present
	VersionDate    string `xml:"version-date"`  // YYYY-MM-DD, the as-of date
	RepealDate     string `xml:"repeal-date"`   // YYYY-MM-DD, when repealed
	Repealed       string `xml:"repealed,attr"` // "yes"/"true" when repealed
}

// Body holds the provision tree. Provisions may sit at the top level or be
// grouped under structural containers (<part>, <subpart>), which we descend.
type Body struct {
	Provs []Prov `xml:"prov"`
	Parts []Prov `xml:"part"`
}

// Prov is a provision. Sections are provisions; the LENZ format also nests
// provisions under parts/subparts, which we flatten depth-first.
type Prov struct {
	Label    string   `xml:"label"`
	Heading  string   `xml:"heading"`
	Body     ProvBody `xml:"body"`
	Provs    []Prov   `xml:"prov"`    // nested provisions
	Parts    []Prov   `xml:"part"`    // nested structural containers
	Subparts []Prov   `xml:"subpart"` // nested structural containers
}

// ProvBody is the textual body of a provision; we keep its full inner text.
type ProvBody struct {
	Inner string `xml:",innerxml"`
}

// ParseAct decodes a whole-act XML document.
func ParseAct(b []byte) (*Act, error) {
	var a Act
	if err := xml.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("lenz: parse act: %w", err)
	}
	return &a, nil
}

// ListItem is one entry from the legislation index (sitemap/listing). It
// carries enough to build the per-act XML URL and the lex identity.
type ListItem struct {
	Category string // "public" / "local" / "private"
	Year     int
	Number   string // 4-digit, zero-padded, e.g. "0109"
	Title    string
}

// List is the index envelope parsed from the listing endpoint.
type List struct {
	Items []ListItem
}

// listXML mirrors the listing XML shape: <legislation><item .../></legislation>.
type listXML struct {
	XMLName xml.Name      `xml:"legislation"`
	Items   []listItemXML `xml:"item"`
}

type listItemXML struct {
	Category string `xml:"category,attr"`
	Year     int    `xml:"year,attr"`
	Number   string `xml:"number,attr"`
	Title    string `xml:"title,attr"`
}

// ParseList decodes a legislation index document.
func ParseList(b []byte) (*List, error) {
	var lx listXML
	if err := xml.Unmarshal(b, &lx); err != nil {
		return nil, fmt.Errorf("lenz: parse list: %w", err)
	}
	out := &List{Items: make([]ListItem, 0, len(lx.Items))}
	for _, it := range lx.Items {
		out.Items = append(out.Items, ListItem{
			Category: it.Category,
			Year:     it.Year,
			Number:   it.Number,
			Title:    it.Title,
		})
	}
	return out, nil
}

// TypeSlug maps a NZ act category/type to an ELI type_document slug. Acts whose
// title contains "Code" are slugged as a code, mirroring the PL/UA approach.
func TypeSlug(category, title string) string {
	if strings.Contains(strings.ToLower(title), " code") ||
		strings.HasSuffix(strings.ToLower(strings.TrimSpace(title)), "code") {
		return "code"
	}
	switch c := strings.ToLower(strings.TrimSpace(category)); c {
	case "public", "":
		return "public-act"
	case "local":
		return "local-act"
	case "private":
		return "private-act"
	case "imperial":
		return "imperial-act"
	case "provincial":
		return "provincial-act"
	default:
		return asciiSlug(c) + "-act"
	}
}

// asciiSlug folds a label to an ASCII slug as a last-resort type slug.
func asciiSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := b.String()
	if out == "" {
		return "akt"
	}
	return out
}

// number returns the act's identifying number, preferring the root attribute
// then the cover. The PCO zero-padded form (e.g. "0038") is kept verbatim so
// the lex URI stays reconstructible into the source whole.xml URL.
func (a *Act) number() string {
	n := a.NumberAttr
	if n == "" {
		n = a.Cover.Number
	}
	return strings.TrimSpace(n)
}

// year returns the act's year, preferring the root attribute then the cover.
func (a *Act) year() int {
	if y, err := strconv.Atoi(strings.TrimSpace(a.YearAttr)); err == nil {
		return y
	}
	if y, err := strconv.Atoi(strings.TrimSpace(a.Cover.Year)); err == nil {
		return y
	}
	return 0
}

// statusOf resolves the in-force status from the cover. A present repeal date
// or an explicit repealed flag means no longer in force; otherwise in force.
func statusOf(c Cover) schema.Status {
	if c.RepealDate != "" {
		return schema.StatusRepealed
	}
	switch strings.ToLower(strings.TrimSpace(c.Repealed)) {
	case "yes", "true", "1":
		return schema.StatusRepealed
	}
	return schema.StatusInForce
}

// parseDate parses a YYYY-MM-DD date (UTC). Empty/invalid input yields the zero
// time.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// versionDate is the MANDATORY as-of date. PCO publishes each consolidated
// reprint with a <version-date> (the "as at" date of the reprint); we use that.
// When absent we fall back to the commencement date, then the assent date.
func versionDate(c Cover) time.Time {
	if t := parseDate(c.VersionDate); !t.IsZero() {
		return t
	}
	if t := parseDate(c.CommencementDt); !t.IsZero() {
		return t
	}
	return parseDate(c.AssentDate)
}

// flattenProvs walks the provision tree depth-first, returning leaf sections
// (provisions carrying a label) in document order.
func flattenProvs(provs []Prov) []Prov {
	var out []Prov
	var walk func([]Prov)
	walk = func(ps []Prov) {
		for _, p := range ps {
			if strings.TrimSpace(p.Label) != "" {
				out = append(out, p)
			}
			walk(p.Provs)
			walk(p.Parts)
			walk(p.Subparts)
		}
	}
	walk(provs)
	return out
}

// sectionNumber extracts the bare section number from a label like "1" or
// "10A" or "s 1" — keeping it as the provision's identifier.
func sectionNumber(label string) string {
	label = strings.TrimSpace(label)
	label = strings.TrimPrefix(label, "s ")
	label = strings.TrimPrefix(label, "S ")
	return strings.TrimSpace(label)
}

// Articles converts the act's provision sections into lex articles. The plain
// text of each section (heading plus body) becomes lex:text.
func (a *Act) Articles() []schema.Article {
	top := append(append([]Prov{}, a.Body.Provs...), a.Body.Parts...)
	provs := flattenProvs(top)
	if len(provs) == 0 {
		return nil
	}
	out := make([]schema.Article, 0, len(provs))
	for _, p := range provs {
		num := sectionNumber(p.Label)
		if num == "" {
			continue
		}
		text := stripTags(p.Body.Inner)
		if h := strings.TrimSpace(p.Heading); h != "" {
			text = strings.TrimSpace(h + " " + text)
		}
		out = append(out, schema.Article{
			Number: num,
			Label:  "Section " + num,
			Text:   text,
		})
	}
	return out
}

// stripTags removes XML tags from an inner-XML fragment and collapses
// whitespace, yielding plain text suitable for FTS.
func stripTags(s string) string {
	var b strings.Builder
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
				b.WriteRune(r)
			}
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// SourceURL builds the human-facing legislation.govt.nz URL for an act.
func SourceURL(category string, year int, number string) string {
	if category == "" {
		category = "public"
	}
	return fmt.Sprintf("https://www.legislation.govt.nz/act/%s/%d/%s/latest/whole.html",
		category, year, number)
}

// ToAct assembles a schema.Act from a parsed act and its list metadata.
// category/title come from the listing; retrievedAt is recorded as
// lex:retrievedAt. Relations are not exposed by the plain whole.xml export and
// are deferred (see ADR-0025 "Consequences").
func ToAct(item ListItem, a *Act, retrievedAt time.Time) (*schema.Act, error) {
	title := item.Title
	if title == "" {
		title = strings.TrimSpace(a.Cover.Title)
	}
	year := item.Year
	if year == 0 {
		year = a.year()
	}
	if year == 0 {
		return nil, fmt.Errorf("lenz: act %q has no resolvable year", item.Title)
	}
	number := item.Number
	if number == "" {
		number = a.number()
	}
	if number == "" {
		return nil, fmt.Errorf("lenz: act %q has no resolvable number", title)
	}

	slug := TypeSlug(item.Category, title)
	vd := versionDate(a.Cover)
	if vd.IsZero() {
		return nil, fmt.Errorf("lenz: act %q has no version date (ontology invariant)", title)
	}

	exp := &schema.Expression{
		Title:            title,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      vd,
		FirstInForceDate: firstInForce(a.Cover),
		Status:           statusOf(a.Cover),
		NoLongerInForce:  parseDate(a.Cover.RepealDate),
		SourceURL:        SourceURL(item.Category, year, number),
		RetrievedAt:      retrievedAt,
		Articles:         a.Articles(),
	}

	// id_local is the NZ canonical identifier "act/<category>/<year>/<number>".
	cat := item.Category
	if cat == "" {
		cat = "public"
	}
	idLocal := fmt.Sprintf("act/%s/%d/%s", cat, year, item.Number)

	return &schema.Act{
		Country:    Country,
		TypeSlug:   slug,
		Year:       year,
		Number:     number,
		IDLocal:    idLocal,
		Expression: exp,
	}, nil
}

// firstInForce picks the act's entry-into-force date: commencement when known,
// else the assent date.
func firstInForce(c Cover) time.Time {
	if t := parseDate(c.CommencementDt); !t.IsZero() {
		return t
	}
	return parseDate(c.AssentDate)
}
