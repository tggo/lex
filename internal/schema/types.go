package schema

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CountryCode is an ISO 3166-1 alpha-2 lowercase code, e.g. "ua".
type CountryCode string

// Status is an act's in-force state.
type Status int

const (
	StatusUnknown Status = iota
	StatusInForce
	StatusRepealed
)

// InForceURI maps the status to its ELI individual, or "" if unknown.
func (s Status) InForceURI() string {
	switch s {
	case StatusInForce:
		return InForceInForce
	case StatusRepealed:
		return InForceNotInForce
	default:
		return ""
	}
}

// Act is the FRBR work — stable identity across amendments.
type Act struct {
	Country    CountryCode // "ua"
	TypeSlug   string      // country-mapped, e.g. "zakon", "kodeks"
	Year       int         // adoption/registration year used in the URI
	Number     string      // native id, e.g. "435-15" (eli:id_local)
	IDLocal    string      // source's native id, usually == Number
	Expression *Expression // current consolidated version
}

// Expression is one consolidated version of an Act at a point in time.
type Expression struct {
	Title            string    // dct:title (natural language)
	LangTag          string    // BCP-47, e.g. "uk"
	LangAlpha3       string    // ELI authority code, e.g. "UKR"
	VersionDate      time.Time // eli:version_date (MANDATORY — as-of date)
	FirstInForceDate time.Time // eli:first_date_entry_in_force (zero if unknown)
	Status           Status
	NoLongerInForce  time.Time // only meaningful when Status == StatusRepealed
	SourceURL        string    // lex:sourceURL (human page)
	RetrievedAt      time.Time // lex:retrievedAt
	Articles         []Article

	// Relations to other acts, by their Resource URI.
	Amends       []string
	AmendedBy    []string // this expression was amended by these acts
	Repeals      []string
	RepealedBy   []string // this expression was repealed by these acts
	Cites        []string
	Consolidates []string
}

// Article is an individual article inside an expression (lex extension).
type Article struct {
	Number string // lex:number, e.g. "1"
	Label  string // skos:prefLabel, e.g. "Стаття 1"
	Text   string // lex:text — plain text, fed into FTS5
}

// segment escapes a URI path segment so ids containing '/' (e.g. "254к/96-вр")
// stay a single segment. Cyrillic is kept; only reserved chars are escaped.
func segment(s string) string {
	return url.PathEscape(s)
}

// ResourceURI builds the stable work URI:
//
//	https://lex.dev/eli/<cc>/<type>/<year>/<number>
func ResourceURI(cc CountryCode, typeSlug string, year int, number string) string {
	return NSid + segment(string(cc)) + "/" + segment(typeSlug) + "/" +
		itoa(year) + "/" + segment(number)
}

// ResourceURI returns the URI for this act's work node.
func (a *Act) ResourceURI() string {
	return ResourceURI(a.Country, a.TypeSlug, a.Year, a.Number)
}

// ParseResourceURI is the inverse of ResourceURI. It recovers the identifying
// parts of a work URI, undoing the per-segment escaping.
func ParseResourceURI(uri string) (cc CountryCode, typeSlug string, year int, number string, err error) {
	rest, ok := strings.CutPrefix(uri, NSid)
	if !ok {
		return "", "", 0, "", fmt.Errorf("schema: %q is not a lex resource URI", uri)
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 4 {
		return "", "", 0, "", fmt.Errorf("schema: resource URI has %d segments, want 4: %q", len(parts), uri)
	}
	ccRaw, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", "", 0, "", fmt.Errorf("schema: bad country segment: %w", err)
	}
	typeSlug, err = url.PathUnescape(parts[1])
	if err != nil {
		return "", "", 0, "", fmt.Errorf("schema: bad type segment: %w", err)
	}
	year, err = strconv.Atoi(parts[2])
	if err != nil {
		return "", "", 0, "", fmt.Errorf("schema: bad year segment %q: %w", parts[2], err)
	}
	number, err = url.PathUnescape(parts[3])
	if err != nil {
		return "", "", 0, "", fmt.Errorf("schema: bad number segment: %w", err)
	}
	return CountryCode(ccRaw), typeSlug, year, number, nil
}

// ExpressionURI builds the version URI:
//
//	<resource>/<version_date(YYYY-MM-DD)>/<lang>
func ExpressionURI(resourceURI string, versionDate time.Time, langTag string) string {
	return resourceURI + "/" + versionDate.Format("2006-01-02") + "/" + segment(langTag)
}

// ExpressionURI returns the URI for this act's current expression.
func (a *Act) ExpressionURI() string {
	return ExpressionURI(a.ResourceURI(), a.Expression.VersionDate, a.Expression.LangTag)
}

// ArticleURI builds an article URI: <expression>/art_<number>.
func ArticleURI(expressionURI, number string) string {
	return expressionURI + "/art_" + segment(number)
}

func itoa(i int) string {
	// small, allocation-light int->string for non-negative years
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
