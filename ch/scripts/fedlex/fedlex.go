// Package fedlex parses SPARQL JSON results from the Swiss Fedlex linked-data
// endpoint (https://fedlex.data.admin.ch/sparqlendpoint) into lex schema.Act
// values. It is pure and offline: HTTP fetching lives in the importer CLI,
// parsing/mapping lives here so it can be golden-tested without the network.
// See ADR-0017 and ch/README.md for the data shape and the legal basis (Swiss
// official federal legislation is not protected by copyright).
//
// Fedlex publishes its Classified Compilation (Systematische Rechtssammlung,
// SR) as RDF using the JoLux ontology. We query eli:LegalResource-equivalent
// ConsolidationAbstract works together with their German title, short title,
// SR notation, and in-force dates, then map each into ELI RDF per
// docs/ontology.md. Swiss law is multilingual (de/fr/it); we take German as the
// primary expression (LangTag "de", alpha-3 "DEU") and note the others.
package fedlex

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Switzerland.
const Country schema.CountryCode = "ch"

// ELI language metadata for the primary (German) expression. Swiss federal law
// is equally authentic in German, French, and Italian; v1 stores the German
// consolidated text and records the others as a next-phase note (see README).
const (
	langTag    = "de"
	langAlpha3 = "DEU"
)

// LanguageDEU is the Publications Office authority URI for German, used to
// select the primary expression in the SPARQL query.
const LanguageDEU = "http://publications.europa.eu/resource/authority/language/DEU"

// binding is one SPARQL JSON solution: a map of variable name to term.
type binding map[string]term

// term is a single SPARQL JSON RDF term (uri / literal / typed-literal).
type term struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Datatype string `json:"datatype"`
	Lang     string `json:"xml:lang"`
}

// results is the SPARQL 1.1 JSON results envelope.
type results struct {
	Head struct {
		Vars []string `json:"vars"`
	} `json:"head"`
	Results struct {
		Bindings []binding `json:"bindings"`
	} `json:"results"`
}

// ParseResults decodes a SPARQL 1.1 JSON results document into its bindings.
func ParseResults(b []byte) ([]binding, error) {
	var r results
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("fedlex: parse sparql results: %w", err)
	}
	return r.Results.Bindings, nil
}

// val returns the value of variable v, or "" if unbound.
func (b binding) val(v string) string { return b[v].Value }

// Val returns the value of SPARQL variable v in this solution, or "" if it is
// unbound. Exported so the importer can read variables (e.g. a resolved file
// URL) from a parsed result set.
func (b binding) Val(v string) string { return b[v].Value }

// TypeSlug maps a Swiss act, identified by its short title and full title, to
// an ELI type_document slug. Switzerland does not carry a single machine "type"
// field on the SR work, so we infer from the German title/abbreviation, the
// same title-driven approach used for codes in the UA/PL importers.
func TypeSlug(titleShort, title string) string {
	lt := strings.ToLower(title)
	switch {
	case strings.Contains(lt, "gesetzbuch"):
		return "gesetzbuch" // a code, e.g. ZGB / StGB / OR (Obligationenrecht)
	case strings.Contains(lt, "bundesverfassung"):
		return "bundesverfassung" // the Federal Constitution
	case strings.Contains(lt, "bundesgesetz"):
		return "bundesgesetz" // a federal act
	case strings.Contains(lt, "verordnung"):
		return "verordnung" // an ordinance
	case strings.Contains(lt, "bundesbeschluss"):
		return "bundesbeschluss" // a federal decree
	case strings.Contains(lt, "reglement"):
		return "reglement" // a regulation
	default:
		return "erlass" // generic enactment fallback
	}
}

// parseDate parses an xsd:date (YYYY-MM-DD, UTC). Empty input yields the zero
// time; unparseable input also yields the zero time.
func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// statusOf resolves an act's in-force status. Fedlex marks a repealed SR work
// with jolux:dateNoLongerInForce; an absent date means the consolidated text is
// the current, in-force one.
func statusOf(dateNoLongerInForce time.Time) schema.Status {
	if dateNoLongerInForce.IsZero() {
		return schema.StatusInForce
	}
	return schema.StatusRepealed
}

// versionDate is the MANDATORY as-of date. Fedlex's SR consolidation works do
// not expose a single clean "consolidated as of" field at this query level, so
// we use the repeal date when the act is no longer in force (the text is frozen
// as of repeal) and otherwise the entry-into-force date of the consolidated
// text. Point-in-time jolux:dateApplicability snapshots are a next phase
// (ADR-0017). Either way version_date is always populated; a record with no
// usable date is dropped by ToActs (the ontology forbids guessing).
func versionDate(entryInForce, noLongerInForce time.Time) time.Time {
	if !noLongerInForce.IsZero() {
		return noLongerInForce
	}
	return entryInForce
}

// yearOf derives the URI year from the version date. The SR notation is the
// stable identity; the year only namespaces the URI per docs/ontology.md.
func yearOf(versionDate time.Time) int {
	if versionDate.IsZero() {
		return 0
	}
	return versionDate.Year()
}

// toAct maps one deduplicated SPARQL solution into a schema.Act, or returns
// ok=false if the row lacks the mandatory pieces (SR notation, title, or a
// usable version date) and must be skipped.
func toAct(b binding, retrievedAt time.Time) (*schema.Act, bool) {
	sr := strings.TrimSpace(b.val("srNotation"))
	title := strings.TrimSpace(b.val("title"))
	ccURI := strings.TrimSpace(b.val("cc"))
	if sr == "" || title == "" || ccURI == "" {
		return nil, false
	}

	entry := parseDate(b.val("dateEntryInForce"))
	noLonger := parseDate(b.val("dateNoLongerInForce"))
	vdate := versionDate(entry, noLonger)
	if vdate.IsZero() {
		return nil, false // MANDATORY version_date unavailable → drop, never guess
	}

	titleShort := strings.TrimSpace(b.val("titleShort"))
	status := statusOf(noLonger)

	exp := &schema.Expression{
		Title:            title,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      vdate,
		FirstInForceDate: entry,
		Status:           status,
		SourceURL:        ccURI, // the Fedlex ELI work URI is its human/linked-data page
		RetrievedAt:      retrievedAt,
	}
	if status == schema.StatusRepealed {
		exp.NoLongerInForce = noLonger
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   TypeSlug(titleShort, title),
		Year:       yearOf(vdate),
		Number:     sr, // the SR systematic number is Switzerland's stable id
		IDLocal:    "SR " + sr,
		Expression: exp,
	}, true
}

// ToActs maps a full SPARQL result set into acts. The endpoint returns one row
// per work after server-side GROUP BY, but we defensively deduplicate by SR
// notation (keeping the first solution) so a non-grouped query still yields one
// act per work. Output is sorted by SR notation for determinism.
func ToActs(bindings []binding, retrievedAt time.Time) []*schema.Act {
	seen := map[string]bool{}
	var acts []*schema.Act
	for _, b := range bindings {
		sr := strings.TrimSpace(b.val("srNotation"))
		if sr == "" || seen[sr] {
			continue
		}
		act, ok := toAct(b, retrievedAt)
		if !ok {
			continue
		}
		seen[sr] = true
		acts = append(acts, act)
	}
	sort.Slice(acts, func(i, j int) bool {
		return srLess(acts[i].Number, acts[j].Number)
	})
	return acts
}

// srLess orders SR notations numerically by their dotted segments so "210"
// sorts before "311.0" and "916.344" naturally.
func srLess(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		ai, aerr := strconv.Atoi(as[i])
		bi, berr := strconv.Atoi(bs[i])
		if aerr != nil || berr != nil {
			if as[i] != bs[i] {
				return as[i] < bs[i]
			}
			continue
		}
		if ai != bi {
			return ai < bi
		}
	}
	return len(as) < len(bs)
}
