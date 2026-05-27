// Package akn parses Finland's Finlex open-data Akoma Ntoso (AKN) statute
// documents into lex schema.Act values. It is pure and offline: HTTP fetching
// lives in the importer CLI, parsing/mapping lives here so it can be
// golden-tested without the network. See ADR-0019 and fi/README.md for the API
// shape and the legal basis (Finnish normative acts are not objects of
// copyright; the open dataset is CC BY 4.0).
//
// Finlex publishes consolidated statutes ("ajantasa") as Akoma Ntoso 3.0 XML
// with a finlex: proprietary extension. Each document carries an ELI alias, the
// statute type, in-force status, entry-into-force date, the title, the body
// sections (§ "pykälä"), and amend/legal-basis relations under <proprietary>.
package akn

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Finland.
const Country schema.CountryCode = "fi"

// ELI language metadata for Finnish. Finlex also publishes Swedish ("swe")
// expressions; v1 ingests the Finnish ("fin") expression as primary.
const (
	langTag    = "fi"
	langAlpha3 = "FIN"
)

// document mirrors the akomaNtoso/act subtree we consume. Fields are matched by
// local element name (the decoder ignores namespaces), so both the default AKN
// namespace and the finlex: prefix resolve here.
type document struct {
	XMLName xml.Name `xml:"akomaNtoso"`
	Act     act      `xml:"act"`
}

type act struct {
	Meta    meta    `xml:"meta"`
	Preface preface `xml:"preface"`
	Body    body    `xml:"body"`
}

type meta struct {
	Identification identification `xml:"identification"`
	References     references     `xml:"references"`
	Proprietary    proprietary    `xml:"proprietary"`
}

type identification struct {
	Work       frbr `xml:"FRBRWork"`
	Expression frbr `xml:"FRBRExpression"`
}

// frbr collects the FRBR* children we need from a Work/Expression block.
type frbr struct {
	URIs    []frbrValue `xml:"FRBRuri"`
	Aliases []frbrAlias `xml:"FRBRalias"`
	Dates   []frbrDate  `xml:"FRBRdate"`
	Number  frbrValue   `xml:"FRBRnumber"`
}

type frbrValue struct {
	Value string `xml:"value,attr"`
}

type frbrAlias struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type frbrDate struct {
	Date string `xml:"date,attr"`
	Name string `xml:"name,attr"`
}

// references holds the TLCConcept entries that resolve typeStatute refersTo ids
// to a human "showAs" label (e.g. "Laki", "Asetus").
type references struct {
	Concepts []tlcConcept `xml:"TLCConcept"`
}

type tlcConcept struct {
	EID    string `xml:"eId,attr"`
	Href   string `xml:"href,attr"`
	ShowAs string `xml:"showAs,attr"`
}

type proprietary struct {
	DocumentYear  string        `xml:"documentYear"`
	TypeStatute   refersTo      `xml:"typeStatute"`
	IsInForce     valueAttr     `xml:"isInForce"`
	InForce       inForceBlock  `xml:"inForce"`
	NoLongerForce noLongerBlock `xml:"noLongerInForce"`
	AmendedBy     []statuteRefs `xml:"amendedBy"`
	RepealedBy    []statuteRefs `xml:"repealedBy"`
	IssuedUnder   []statuteRefs `xml:"issuedUnderActs"`
}

type refersTo struct {
	RefersTo string `xml:"refersTo,attr"`
}

type valueAttr struct {
	Value string `xml:"value,attr"`
}

type inForceBlock struct {
	DateEntry dateAttr `xml:"dateEntryIntoForce"`
}

type noLongerBlock struct {
	Date dateAttr `xml:"dateNoLongerInForce"`
}

type dateAttr struct {
	Date string `xml:"date,attr"`
}

// statuteRefs groups one relation block (e.g. <amendedBy>) holding one or more
// <statuteReference> entries, each with a <ref href="/akn/fi/act/statute/Y/N">.
type statuteRefs struct {
	Refs []statuteReference `xml:"statuteReference"`
}

type statuteReference struct {
	Ref refHref `xml:"ref"`
}

type refHref struct {
	Href string `xml:"href,attr"`
}

type preface struct {
	DocTitle  string `xml:"p>docTitle"`
	DocNumber string `xml:"p>docNumber"`
}

type body struct {
	Containers []hcontainer `xml:"hcontainer"`
}

type hcontainer struct {
	Name     string       `xml:"name,attr"`
	Sections []section    `xml:"section"`
	Children []hcontainer `xml:"hcontainer"`
}

type section struct {
	EID         string       `xml:"eId,attr"`
	Num         string       `xml:"num"`
	Heading     string       `xml:"heading"`
	Subsections []subsection `xml:"subsection"`
	Content     content      `xml:"content"`
}

type subsection struct {
	Content content `xml:"content"`
}

type content struct {
	Inner string `xml:",innerxml"`
}

// Document is a parsed AKN statute ready to map to schema.Act.
type Document struct {
	doc document
}

// Parse decodes a Finlex Akoma Ntoso statute document. It accepts both a bare
// <akomaNtoso> document and one wrapped in the list envelope.
func Parse(b []byte) (*Document, error) {
	var d document
	if err := xml.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("akn: parse document: %w", err)
	}
	if d.Act.Meta.Identification.Work.eliAlias() == "" && d.Act.Preface.DocTitle == "" {
		return nil, fmt.Errorf("akn: document has neither ELI nor title")
	}
	return &Document{doc: d}, nil
}

// list is the <AknXmlList><Results> envelope wrapping several documents.
type list struct {
	XMLName xml.Name   `xml:"AknXmlList"`
	Docs    []document `xml:"Results>akomaNtoso"`
}

// ParseList decodes the list envelope into its constituent documents.
func ParseList(b []byte) ([]*Document, error) {
	var l list
	if err := xml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("akn: parse list: %w", err)
	}
	out := make([]*Document, 0, len(l.Docs))
	for i := range l.Docs {
		out = append(out, &Document{doc: l.Docs[i]})
	}
	return out, nil
}

func (f frbr) eliAlias() string {
	for _, a := range f.Aliases {
		if a.Name == "eli" {
			return a.Value
		}
	}
	return ""
}

func (f frbr) date(name string) string {
	for _, d := range f.Dates {
		if d.Name == name {
			return d.Date
		}
	}
	return ""
}

// firstURI returns the Work/Expression FRBRuri value, the canonical AKN path.
func (f frbr) firstURI() string {
	if len(f.URIs) > 0 {
		return f.URIs[0].Value
	}
	return ""
}

// Identity returns the statute's year and position number, the pair that
// identifies the act in the Finlex collection (e.g. 2019, "469").
func (d *Document) Identity() (year int, number string, err error) {
	year, err = d.year()
	if err != nil {
		return 0, "", err
	}
	number, err = d.number()
	if err != nil {
		return 0, "", err
	}
	return year, number, nil
}

// year extracts the statute year from documentYear, falling back to the work
// FRBRuri path (.../statute-consolidated/<year>/<number>).
func (d *Document) year() (int, error) {
	if y := strings.TrimSpace(d.doc.Act.Meta.Proprietary.DocumentYear); y != "" {
		if n, err := strconv.Atoi(y); err == nil {
			return n, nil
		}
	}
	yr, _, err := splitAknPath(d.doc.Act.Meta.Identification.Work.firstURI())
	return yr, err
}

// number is the statute position number (FRBRnumber), e.g. "469".
func (d *Document) number() (string, error) {
	if n := strings.TrimSpace(d.doc.Act.Meta.Identification.Work.Number.Value); n != "" {
		return n, nil
	}
	_, num, err := splitAknPath(d.doc.Act.Meta.Identification.Work.firstURI())
	if err != nil {
		return "", err
	}
	return num, nil
}

// splitAknPath pulls year and number from an AKN path like
// "/akn/fi/act/statute-consolidated/2019/469" (trailing segments ignored).
func splitAknPath(path string) (year int, number string, err error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// find the year/number pair following the "statute"/"statute-consolidated"
	// segment.
	for i := 0; i+2 < len(parts)+1 && i+1 < len(parts); i++ {
		if strings.HasPrefix(parts[i], "statute") {
			if i+2 >= len(parts) {
				break
			}
			y, e := strconv.Atoi(parts[i+1])
			if e != nil {
				return 0, "", fmt.Errorf("akn: bad year in path %q: %w", path, e)
			}
			return y, parts[i+2], nil
		}
	}
	return 0, "", fmt.Errorf("akn: cannot parse year/number from path %q", path)
}

// TypeSlug maps the finlex typeStatute concept (its TLCConcept showAs label) to
// an ELI type_document slug. The proprietary block carries refersTo="#act"
// etc.; the references block resolves that id to a Finnish label.
func (d *Document) TypeSlug() string {
	id := strings.TrimPrefix(d.doc.Act.Meta.Proprietary.TypeStatute.RefersTo, "#")
	label := ""
	for _, c := range d.doc.Act.Meta.References.Concepts {
		if c.EID == id {
			label = c.ShowAs
			break
		}
	}
	return typeSlug(id, label)
}

// typeSlug folds the concept id and/or label into a stable ELI slug.
func typeSlug(conceptID, label string) string {
	switch strings.ToLower(strings.TrimSpace(conceptID)) {
	case "act":
		return "laki"
	case "decree":
		return "asetus"
	}
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "laki":
		return "laki"
	case "asetus":
		return "asetus"
	}
	if s := asciiSlug(label); s != "" {
		return s
	}
	if s := asciiSlug(conceptID); s != "" {
		return s
	}
	return "saados"
}

// asciiSlug folds a Finnish label to a lowercase ASCII slug.
func asciiSlug(s string) string {
	repl := strings.NewReplacer("ä", "a", "ö", "o", "å", "a", " ", "-")
	out := repl.Replace(strings.ToLower(strings.TrimSpace(s)))
	return out
}

// statusOf resolves the in-force status from finlex:isInForce / noLongerInForce.
func (d *Document) statusOf() schema.Status {
	p := d.doc.Act.Meta.Proprietary
	if p.NoLongerForce.Date.Date != "" {
		return schema.StatusRepealed
	}
	switch strings.ToLower(strings.TrimSpace(p.IsInForce.Value)) {
	case "true":
		return schema.StatusInForce
	case "false":
		return schema.StatusRepealed
	}
	return schema.StatusUnknown
}

// parseDate parses a YYYY-MM-DD date (UTC). Empty input yields the zero time.
func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// versionDate is the MANDATORY as-of date. We use the expression's
// dateConsolidated (the point in time the consolidated text reflects), falling
// back to its dateIssued, then the work's dateIssued. Documented in ADR-0019.
func (d *Document) versionDate() time.Time {
	exp := d.doc.Act.Meta.Identification.Expression
	if v := parseDate(exp.date("dateConsolidated")); !v.IsZero() {
		return v
	}
	if v := parseDate(exp.date("dateIssued")); !v.IsZero() {
		return v
	}
	return parseDate(d.doc.Act.Meta.Identification.Work.date("dateIssued"))
}

// firstInForce is the work's entry-into-force date.
func (d *Document) firstInForce() time.Time {
	return parseDate(d.doc.Act.Meta.Proprietary.InForce.DateEntry.Date)
}

func (d *Document) noLongerInForce() time.Time {
	return parseDate(d.doc.Act.Meta.Proprietary.NoLongerForce.Date.Date)
}

// articles walks the body's section (§ "pykälä") elements into schema.Articles.
func (d *Document) articles() []schema.Article {
	var out []schema.Article
	var walk func(c hcontainer)
	walk = func(c hcontainer) {
		for _, s := range c.Sections {
			out = append(out, sectionToArticle(s))
		}
		for _, child := range c.Children {
			walk(child)
		}
	}
	for _, c := range d.doc.Act.Body.Containers {
		walk(c)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// sectionToArticle maps one § section to a lex Article. Number is the bare
// numeral from <num> (e.g. "1" from "1 §"); the heading and all subsection/
// content text are concatenated into lex:text.
func sectionToArticle(s section) schema.Article {
	num := sectionNumber(s.Num)
	var parts []string
	if h := normalize(s.Heading); h != "" {
		parts = append(parts, h)
	}
	for _, sub := range s.Subsections {
		if t := normalize(stripTags(sub.Content.Inner)); t != "" {
			parts = append(parts, t)
		}
	}
	if t := normalize(stripTags(s.Content.Inner)); t != "" {
		parts = append(parts, t)
	}
	return schema.Article{
		Number: num,
		Label:  normalize(s.Num),
		Text:   strings.Join(parts, " "),
	}
}

// sectionNumber pulls the leading numeral (incl. letter suffix like "10a") out
// of a "<n> §" num string.
func sectionNumber(num string) string {
	fields := strings.Fields(num)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// stripTags removes XML tags from an innerxml fragment, keeping text content.
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
	return b.String()
}

// normalize collapses whitespace runs to single spaces and trims.
func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// relationURIs resolves a set of relation blocks into lex Resource URIs. Each
// <ref href="/akn/fi/act/statute/<year>/<number>"> becomes a work URI. The
// target's precise type slug is not known from the ref alone, so a neutral
// "statute" slug is used; targets are linked by year/number and gain their
// precise type when that act is itself ingested. See ADR-0019 (relations).
func relationURIs(blocks []statuteRefs) []string {
	var out []string
	for _, blk := range blocks {
		for _, r := range blk.Refs {
			y, n, err := splitAknPath(r.Ref.Href)
			if err != nil {
				continue
			}
			out = append(out, schema.ResourceURI(Country, "statute", y, strconv.Itoa(y)+"/"+n))
		}
	}
	sort.Strings(out)
	return out
}

// ToAct assembles a schema.Act from a parsed document. retrievedAt is recorded
// as lex:retrievedAt. withArticles controls whether body sections are parsed.
func (d *Document) ToAct(retrievedAt time.Time, withArticles bool) (*schema.Act, error) {
	year, err := d.year()
	if err != nil {
		return nil, err
	}
	num, err := d.number()
	if err != nil {
		return nil, err
	}
	slug := d.TypeSlug()

	vd := d.versionDate()
	if vd.IsZero() {
		return nil, fmt.Errorf("akn: %d/%s has no version date", year, num)
	}

	var arts []schema.Article
	if withArticles {
		arts = d.articles()
	}

	eli := d.doc.Act.Meta.Identification.Work.eliAlias()
	exp := &schema.Expression{
		Title:            normalize(d.doc.Act.Preface.DocTitle),
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      vd,
		FirstInForceDate: d.firstInForce(),
		Status:           d.statusOf(),
		SourceURL:        eli,
		RetrievedAt:      retrievedAt,
		Articles:         arts,
	}
	if exp.Status == schema.StatusRepealed {
		exp.NoLongerInForce = d.noLongerInForce()
	}

	p := d.doc.Act.Meta.Proprietary
	exp.AmendedBy = relationURIs(p.AmendedBy)
	exp.RepealedBy = relationURIs(p.RepealedBy)
	exp.Cites = relationURIs(p.IssuedUnder)

	// Number = "<year>/<position>" so the (year, number) pair is the natural
	// Finlex citation form (e.g. "2019/469" ↔ commonly written "469/2019").
	number := strconv.Itoa(year) + "/" + num
	return &schema.Act{
		Country:    Country,
		TypeSlug:   slug,
		Year:       year,
		Number:     number,
		IDLocal:    eli,
		Expression: exp,
	}, nil
}
