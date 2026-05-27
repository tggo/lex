// Package legilux parses Luxembourg Legilux SPARQL results
// (data.legilux.public.lu) into lex schema.Act values. It is pure and offline:
// the SPARQL HTTP querying lives in the importer CLI, parsing/mapping lives here
// so it can be golden-tested without the network. See ADR-0018 and lu/README.md
// for the endpoint shape and the legal basis (Luxembourg normative acts are
// open data, CC BY, attributed to Legilux / État du Grand-Duché de Luxembourg).
//
// Legilux publishes RDF using the JOLux + ELI models. The FRBR Work carries
// identity, type, and dates; its French Expression carries the title; the
// modifies/repeals/cites/consolidates edges connect Works. The mapping to
// docs/ontology.md is therefore close to 1:1.
package legilux

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Luxembourg.
const Country schema.CountryCode = "lu"

// ELI language metadata. Legilux is multilingual (fr, de, lb); French is the
// primary expression we ingest. See README.
const (
	langTag    = "fr"
	langAlpha3 = "FRA"
)

// jolux namespace and the predicates the importer queries.
const (
	NSjolux = "http://data.legilux.public.lu/resource/ontology/jolux#"

	predModifies     = NSjolux + "modifies"
	predRepeals      = NSjolux + "repeals"
	predCites        = NSjolux + "cites"
	predConsolidates = NSjolux + "consolidates"

	// eliBase is the prefix of every Legilux ELI work URI; the path after it is
	// the act's native identifier.
	eliBase = "http://data.legilux.public.lu/eli/"
)

// Binding is one SPARQL result value (the SPARQL 1.1 JSON results shape).
type Binding struct {
	Type     string `json:"type"`     // "uri" | "literal" | "typed-literal"
	Value    string `json:"value"`    // the lexical value
	XMLLang  string `json:"xml:lang"` // language tag, when present
	Datatype string `json:"datatype"` // datatype URI, when present
}

// Results is the SPARQL 1.1 JSON results envelope.
type Results struct {
	Head struct {
		Vars []string `json:"vars"`
	} `json:"head"`
	Results struct {
		Bindings []map[string]Binding `json:"bindings"`
	} `json:"results"`
}

// ParseResults decodes a SPARQL JSON results document.
func ParseResults(b []byte) (*Results, error) {
	var r Results
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("legilux: parse sparql results: %w", err)
	}
	return &r, nil
}

// Rows returns the result bindings.
func (r *Results) Rows() []map[string]Binding { return r.Results.Bindings }

// val returns the value of the named binding, or "" if absent.
func val(row map[string]Binding, name string) string {
	if b, ok := row[name]; ok {
		return b.Value
	}
	return ""
}

// typeSlugFromAuthority maps a Legilux resource-type authority URI (its trailing
// code, e.g. "RGD", "LOI") to an ELI type_document slug. Codes (a stable text
// identified by the path segment "code") map to "code", mirroring the UA/PL
// approach of giving codes their own slug.
func typeSlugFromAuthority(typeURI, workURI string) string {
	code := strings.ToLower(lastSegment(typeURI))
	if strings.Contains(workURI, "/code/") {
		return "code"
	}
	switch code {
	case "loi":
		return "loi"
	case "rgd":
		return "rgd"
	case "argd":
		return "argd"
	case "amin", "rmin":
		return code
	case "agd":
		return "agd"
	case "a":
		return "arrete"
	case "reg_ue":
		return "reg-ue"
	case "dir_ue":
		return "dir-ue"
	case "":
		return "acte"
	default:
		return code
	}
}

// lastSegment returns the final '/'-separated segment of s.
func lastSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// idLocal returns the act's native identifier: the path of the work URI after
// the Legilux ELI base (e.g. "etat/leg/rgd/2020/03/18/a165/jo"). It is unique
// per act and the schema's per-segment escaping keeps the slashes intact.
func idLocal(workURI string) string {
	return strings.TrimPrefix(workURI, eliBase)
}

// statusOf resolves an act's in-force status from its application-status
// authority URI (its trailing code).
func statusOf(statusURI string) schema.Status {
	switch lastSegment(statusURI) {
	case "in-force", "applicable":
		return schema.StatusInForce
	case "no-longer-in-force", "no-longer-in-force-implicit", "not-applicable":
		return schema.StatusRepealed
	default:
		return schema.StatusUnknown
	}
}

// parseDate parses an xsd:date (YYYY-MM-DD), tolerating a trailing time. Empty
// input yields the zero time.
func parseDate(s string) time.Time {
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

// yearOf extracts the adoption year from the dateDocument; if absent it falls
// back to the year segment of the work URI.
func yearOf(dateDoc, workURI string) int {
	if d := parseDate(dateDoc); !d.IsZero() {
		return d.Year()
	}
	// .../<type>/<year>/<mm>/<dd>/... — scan for a 4-digit segment.
	for _, seg := range strings.Split(idLocal(workURI), "/") {
		if len(seg) == 4 {
			if y, err := strconv.Atoi(seg); err == nil {
				return y
			}
		}
	}
	return 0
}

// resourceURIFromWork builds the lex work URI for a Legilux work URI given its
// type authority and adoption year.
func resourceURIFromWork(workURI, typeURI, dateDoc string) string {
	return schema.ResourceURI(Country, typeSlugFromAuthority(typeURI, workURI),
		yearOf(dateDoc, workURI), idLocal(workURI))
}

// relationFor maps a JOLux relation predicate to a schema relation kind. The
// second result is false for predicates lex does not model.
func relationFor(pred string) (kind string, ok bool) {
	switch pred {
	case predModifies:
		return "amends", true
	case predRepeals:
		return "repeals", true
	case predCites:
		return "cites", true
	case predConsolidates:
		return "consolidates", true
	default:
		return "", false
	}
}

// ActRow is one act from the acts-page query, before relations are attached.
type ActRow struct {
	WorkURI  string
	TypeURI  string
	Title    string
	LangURI  string
	DateDoc  string
	Entry    string
	NoLonger string
	Status   string
}

// ParseActRows extracts act rows from an acts-page SPARQL result. The variable
// names match the importer's query (work, type, title, lang, dateDoc, entry,
// noLonger, status).
func ParseActRows(res *Results) []ActRow {
	out := make([]ActRow, 0, len(res.Rows()))
	for _, row := range res.Rows() {
		out = append(out, ActRow{
			WorkURI:  val(row, "work"),
			TypeURI:  val(row, "type"),
			Title:    val(row, "title"),
			LangURI:  val(row, "lang"),
			DateDoc:  val(row, "dateDoc"),
			Entry:    val(row, "entry"),
			NoLonger: val(row, "noLonger"),
			Status:   val(row, "status"),
		})
	}
	return out
}

// ParseRelations extracts relation edges from a relations SPARQL result (vars
// rel, target) and resolves each target to a lex work URI. The relations query
// returns JOLux predicate URIs in ?rel and target work URIs in ?target. Targets
// outside the Legilux ELI base (e.g. EU directives) are kept verbatim as URIs.
func ParseRelations(res *Results) (amends, repeals, cites, consolidates []string) {
	for _, row := range res.Rows() {
		kind, ok := relationFor(val(row, "rel"))
		if !ok {
			continue
		}
		target := val(row, "target")
		if target == "" {
			continue
		}
		// Resolve Legilux targets to lex work URIs; keep foreign URIs as-is.
		ruri := target
		if strings.HasPrefix(target, eliBase) {
			ruri = schema.ResourceURI(Country, "", yearOf("", target), idLocal(target))
		}
		switch kind {
		case "amends":
			amends = append(amends, ruri)
		case "repeals":
			repeals = append(repeals, ruri)
		case "cites":
			cites = append(cites, ruri)
		case "consolidates":
			consolidates = append(consolidates, ruri)
		}
	}
	sort.Strings(amends)
	sort.Strings(repeals)
	sort.Strings(cites)
	sort.Strings(consolidates)
	return amends, repeals, cites, consolidates
}

// ToAct assembles a schema.Act from an act row and its parsed relations.
// retrievedAt is recorded as lex:retrievedAt. Articles are deferred (next
// phase); see ADR-0018. Returns nil if the row lacks a version date (the
// ontology requires one) — the caller skips such records.
func ToAct(r ActRow, amends, repeals, cites, consolidates []string, retrievedAt time.Time) *schema.Act {
	vdate := versionDate(r)
	if vdate.IsZero() {
		return nil
	}
	exp := &schema.Expression{
		Title:            r.Title,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      vdate,
		FirstInForceDate: parseDate(r.Entry),
		Status:           statusOf(r.Status),
		SourceURL:        r.WorkURI,
		RetrievedAt:      retrievedAt,
		Amends:           amends,
		Repeals:          repeals,
		Cites:            cites,
		Consolidates:     consolidates,
	}
	if exp.Status == schema.StatusRepealed {
		exp.NoLongerInForce = parseDate(r.NoLonger)
	}
	return &schema.Act{
		Country:    Country,
		TypeSlug:   typeSlugFromAuthority(r.TypeURI, r.WorkURI),
		Year:       yearOf(r.DateDoc, r.WorkURI),
		Number:     idLocal(r.WorkURI),
		IDLocal:    idLocal(r.WorkURI),
		Expression: exp,
	}
}

// versionDate is the MANDATORY as-of date. Legilux dates: dateDocument is the
// adoption/publication date of the act text. JOLux exposes no separate
// "consolidated as of" field on the base Act (consolidations are distinct Work
// nodes), so for the current consolidated metadata we use dateDocument, which is
// always present for an act and is the stable as-of anchor. See ADR-0018.
func versionDate(r ActRow) time.Time {
	if d := parseDate(r.DateDoc); !d.IsZero() {
		return d
	}
	// Fall back to the entry-into-force date if dateDocument is somehow absent.
	return parseDate(r.Entry)
}
