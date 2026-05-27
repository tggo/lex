// Package clml parses legislation.gov.uk CLML (Crown Legislation Markup
// Language) XML and Atom list feeds into lex schema.Act values. It is pure and
// offline: HTTP fetching lives in the importer CLI, parsing/mapping lives here
// so it can be golden-tested without the network. See ADR-0015 and
// uk/README.md for the channel shape and the legal basis (Open Government
// Licence v3.0).
//
// legislation.gov.uk (The National Archives) publishes native ELI identifiers,
// so the mapping to docs/ontology.md is close to 1:1: each act's path
// ("ukpga/2023/57") gives type + year + number, and inline <Citation> elements
// give cites edges.
package clml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// bytesReader is a tiny helper so scanCitations reads from the byte slice.
func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// Country is the ISO code for the United Kingdom.
const Country schema.CountryCode = "uk"

// ELI language metadata for UK legislation (English).
const (
	langTag    = "en"
	langAlpha3 = "ENG"
)

// site is the canonical human/source host; lex:sourceURL points here.
const site = "https://www.legislation.gov.uk/"

// Feed is the subset of an Atom list feed (…/data.feed) we consume. Each entry
// identifies one act available under the queried type/year.
type Feed struct {
	XMLName   xml.Name    `xml:"feed"`
	Page      int         `xml:"page"`      // leg:page — current page (1-based)
	MorePages int         `xml:"morePages"` // leg:morePages — total page count
	Entries   []FeedEntry `xml:"entry"`
}

// HasMore reports whether there is a page after this one.
func (f *Feed) HasMore() bool { return f.Page > 0 && f.Page < f.MorePages }

// FeedEntry is one act listed in a data.feed page.
type FeedEntry struct {
	ID    string `xml:"id"`    // e.g. http://www.legislation.gov.uk/id/ukpga/2023/57
	Title string `xml:"title"` // act title
	Year  struct {
		Value string `xml:"Value,attr"`
	} `xml:"Year"`
	Number struct {
		Value string `xml:"Value,attr"`
	} `xml:"Number"`
	MainType struct {
		Value string `xml:"Value,attr"`
	} `xml:"DocumentMainType"`
}

// ParseFeed decodes a data.feed Atom document.
func ParseFeed(b []byte) (*Feed, error) {
	var f Feed
	if err := xml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("clml: parse feed: %w", err)
	}
	return &f, nil
}

// Path returns the act path ("<type>/<year>/<number>") for a feed entry,
// derived from its IdURI. It is the inverse of the legislation.gov.uk id URI
// scheme: http://www.legislation.gov.uk/id/<type>/<year>/<number>.
func (e FeedEntry) Path() (string, bool) {
	i := strings.Index(e.ID, "/id/")
	if i < 0 {
		return "", false
	}
	rest := strings.Trim(e.ID[i+len("/id/"):], "/")
	if strings.Count(rest, "/") < 2 {
		return "", false
	}
	parts := strings.SplitN(rest, "/", 4)
	// Keep exactly type/year/number; ignore any trailing version/segment.
	return strings.Join(parts[:3], "/"), true
}

// Legislation is the subset of a CLML act document (…/data.xml) we consume.
type Legislation struct {
	XMLName           xml.Name `xml:"Legislation"`
	DocumentURI       string   `xml:"DocumentURI,attr"`
	IdURI             string   `xml:"IdURI,attr"`
	RestrictStartDate string   `xml:"RestrictStartDate,attr"`
	Metadata          struct {
		Identifier string `xml:"identifier"`
		Title      string `xml:"title"`
		Modified   string `xml:"modified"`
		Valid      string `xml:"valid"`
		// PrimaryMetadata wraps the document classification, year/number, and
		// enactment date (ukm:PrimaryMetadata in CLML).
		EnactmentDate struct {
			Date string `xml:"Date,attr"`
		} `xml:"PrimaryMetadata>EnactmentDate"`
		Year struct {
			Value string `xml:"Value,attr"`
		} `xml:"PrimaryMetadata>Year"`
		Number struct {
			Value string `xml:"Value,attr"`
		} `xml:"PrimaryMetadata>Number"`
		MainType struct {
			Value string `xml:"Value,attr"`
		} `xml:"PrimaryMetadata>DocumentClassification>DocumentMainType"`
		Status struct {
			Value string `xml:"Value,attr"`
		} `xml:"PrimaryMetadata>DocumentClassification>DocumentStatus"`
	} `xml:"Metadata"`
	// P1groups are the act's enacting sections (Body/P1group/P1 → one section).
	// Quoted/amended provisions live elsewhere (Commentaries) and are excluded.
	P1groups []P1group `xml:"Primary>Body>P1group"`
	// Citations are inline references to other legislation → eli:cites. They are
	// scattered through Commentaries, so they are gathered by a separate scan
	// (see ParseLegislation) rather than a fixed path.
	Citations []Citation `xml:"-"`
}

// P1group is a CLML section group: a Title plus one P1 carrying the section
// number; the group's text becomes the article body.
type P1group struct {
	Title string `xml:"Title"`
	P1    struct {
		ID      string `xml:"id,attr"`
		Pnumber string `xml:"Pnumber"`
	} `xml:"P1"`
	// Inner is the section body as raw inner XML; flattenText turns it into the
	// whitespace-collapsed plain text fed into lex:text (sub-paragraphs,
	// amendments, and citations included).
	Inner string `xml:",innerxml"`
}

// Citation is an inline reference to another piece of legislation.
type Citation struct {
	URI   string `xml:"URI,attr"`   // http://www.legislation.gov.uk/id/<type>/<year>/<number>
	Class string `xml:"Class,attr"` // e.g. UnitedKingdomPublicGeneralAct
	Year  string `xml:"Year,attr"`
	Num   string `xml:"Number,attr"`
}

// ParseLegislation decodes a CLML act document. Citation elements appear at
// many depths (notably inside Commentaries), so they are collected by a second
// streaming scan over the whole document rather than via a fixed struct path.
func ParseLegislation(b []byte) (*Legislation, error) {
	var l Legislation
	if err := xml.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("clml: parse legislation: %w", err)
	}
	cites, err := scanCitations(b)
	if err != nil {
		return nil, err
	}
	l.Citations = cites
	return &l, nil
}

// scanCitations streams the document and returns every <Citation> element's
// attributes, regardless of nesting depth.
func scanCitations(b []byte) ([]Citation, error) {
	dec := xml.NewDecoder(bytesReader(b))
	var out []Citation
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("clml: scan citations: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Citation" {
			continue
		}
		var c Citation
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "URI":
				c.URI = a.Value
			case "Class":
				c.Class = a.Value
			case "Year":
				c.Year = a.Value
			case "Number":
				c.Num = a.Value
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// Path returns the act path ("<type>/<year>/<number>") from the IdURI.
func (l *Legislation) Path() (string, error) {
	src := l.IdURI
	if src == "" {
		src = l.DocumentURI
	}
	i := strings.Index(src, "/id/")
	if i >= 0 {
		src = src[i+len("/id/"):]
	} else if j := strings.Index(src, "legislation.gov.uk/"); j >= 0 {
		src = src[j+len("legislation.gov.uk/"):]
	}
	rest := strings.Trim(src, "/")
	parts := strings.SplitN(rest, "/", 4)
	if len(parts) < 3 {
		return "", fmt.Errorf("clml: cannot derive path from %q", l.IdURI)
	}
	return strings.Join(parts[:3], "/"), nil
}

// splitPath splits "<type>/<year>/<number>" into its parts.
func splitPath(path string) (typ string, year int, number string, err error) {
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		return "", 0, "", fmt.Errorf("clml: malformed path %q", path)
	}
	y, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", fmt.Errorf("clml: path %q has bad year: %w", path, err)
	}
	return parts[0], y, parts[2], nil
}

// TypeSlug maps a legislation.gov.uk type code (the first path segment, e.g.
// "ukpga", "uksi") to the ELI type_document slug. The codes are already short,
// stable, ASCII slugs, so the mapping is identity with a curated allow-set for
// documentation; unknown codes fall through unchanged.
func TypeSlug(typeCode string) string {
	return strings.ToLower(strings.TrimSpace(typeCode))
}

// classToType maps a Citation Class to its legislation.gov.uk type code, so a
// citation URI without an explicit type segment can still be resolved. The
// citation URI already contains the type segment in practice, so this is only a
// fallback.
func classToType(class string) string {
	switch class {
	case "UnitedKingdomPublicGeneralAct":
		return "ukpga"
	case "UnitedKingdomStatutoryInstrument":
		return "uksi"
	case "UnitedKingdomLocalAct":
		return "ukla"
	case "ScottishAct":
		return "asp"
	case "WelshNationalAssemblyAct", "AnafGeneddol":
		return "anaw"
	case "NorthernIrelandAct":
		return "nia"
	default:
		return ""
	}
}

// parseDate parses a YYYY-MM-DD date (UTC). Empty/invalid input yields the zero
// time.
func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// versionDate is the MANDATORY as-of date. legislation.gov.uk serves
// point-in-time consolidations; the document's RestrictStartDate is the date
// from which the served text is in force (i.e. "this consolidation is valid
// from"). We prefer it, then dct:valid, then the enactment date. See ADR-0015.
func versionDate(l *Legislation) time.Time {
	for _, c := range []string{l.RestrictStartDate, l.Metadata.Valid, l.Metadata.EnactmentDate.Date} {
		if t := parseDate(c); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

// statusOf resolves an act's in-force status. CLML's DocumentStatus
// ("revised"/"final") describes editorial state, not whether the act is in
// force, and the point-in-time API only serves text that is in force as of the
// requested date; we therefore treat a served consolidation as in force.
// Repeal is exposed as a separate point-in-time concern (see ADR-0015 — next
// phase), so we do not infer repeal here.
func statusOf(*Legislation) schema.Status {
	return schema.StatusInForce
}

// citationURIToResource resolves a Citation to a lex Resource URI, or "" if it
// cannot be resolved (e.g. a non-legislation reference).
func citationURIToResource(c Citation) string {
	src := c.URI
	i := strings.Index(src, "/id/")
	if i < 0 {
		return ""
	}
	rest := strings.Trim(src[i+len("/id/"):], "/")
	parts := strings.Split(rest, "/")
	var typ, num string
	var year int
	if len(parts) >= 3 {
		// /id/<type>/<year>/<number>[/...]
		y, err := strconv.Atoi(parts[1])
		if err != nil {
			return ""
		}
		typ, year, num = parts[0], y, parts[2]
	} else {
		// Fallback: derive type from Class, year/number from attrs.
		typ = classToType(c.Class)
		y, err := strconv.Atoi(c.Year)
		if err != nil || typ == "" || c.Num == "" {
			return ""
		}
		year, num = y, c.Num
	}
	return schema.ResourceURI(Country, TypeSlug(typ), year, num)
}

// ToAct assembles a schema.Act from a parsed CLML document. retrievedAt is
// recorded as lex:retrievedAt.
func ToAct(l *Legislation, retrievedAt time.Time) (*schema.Act, error) {
	path, err := l.Path()
	if err != nil {
		return nil, err
	}
	typ, year, number, err := splitPath(path)
	if err != nil {
		return nil, err
	}

	exp := &schema.Expression{
		Title:            strings.TrimSpace(l.Metadata.Title),
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      versionDate(l),
		FirstInForceDate: parseDate(l.Metadata.EnactmentDate.Date),
		Status:           statusOf(l),
		SourceURL:        site + path,
		RetrievedAt:      retrievedAt,
		Articles:         Articles(l),
	}

	// Inline citations → eli:cites. Deduplicate and keep deterministic order.
	seen := map[string]bool{}
	self := schema.ResourceURI(Country, TypeSlug(typ), year, number)
	for _, c := range l.Citations {
		uri := citationURIToResource(c)
		if uri == "" || uri == self || seen[uri] {
			continue
		}
		seen[uri] = true
		exp.Cites = append(exp.Cites, uri)
	}
	sortStrings(exp.Cites)

	return &schema.Act{
		Country:    Country,
		TypeSlug:   TypeSlug(typ),
		Year:       year,
		Number:     number,
		IDLocal:    path,
		Expression: exp,
	}, nil
}
