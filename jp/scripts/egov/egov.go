// Package egov parses the Japanese e-Gov 法令API v2 law-list response
// (GET /api/2/laws) into lex schema.Act values. It is pure and offline:
// fetching lives in the importer CLI, parsing/mapping lives here so it can be
// golden-tested without the network. See ADR-0011 and jp/README.md for the API
// shape and license (e-Gov 法令検索, CC BY 4.0-compatible).
package egov

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/tggo/lex/internal/schema"
)

// lawsResponse is the envelope of GET /api/2/laws. Only the fields lex uses are
// decoded; unknown fields (paging, kana, abbrev, …) are ignored.
type lawsResponse struct {
	Laws []lawEntry `json:"laws"`
}

type lawEntry struct {
	LawInfo      lawInfo      `json:"law_info"`
	RevisionInfo revisionInfo `json:"revision_info"`
}

// lawInfo identifies the act (the FRBR work).
type lawInfo struct {
	LawType          string `json:"law_type"`          // e.g. "Act", "CabinetOrder"
	LawID            string `json:"law_id"`            // stable id, e.g. "129AC0000000089"
	LawNum           string `json:"law_num"`           // human number, e.g. 明治二十九年法律第八十九号
	PromulgationDate string `json:"promulgation_date"` // yyyy-mm-dd
}

// revisionInfo describes the current consolidated revision (the expression).
type revisionInfo struct {
	LawRevisionID         string `json:"law_revision_id"` // expression id, used to fetch law_data
	LawTitle              string `json:"law_title"`
	Category              string `json:"category"`
	EnforcementDate       string `json:"amendment_enforcement_date"` // yyyy-mm-dd — the as-of date
	RepealStatus          string `json:"repeal_status"`              // "None" or "Repeal"/...
	CurrentRevisionStatus string `json:"current_revision_status"`    // e.g. "CurrentEnforced"
	AmendmentLawID        string `json:"amendment_law_id"`           // law_id that produced this revision
}

// Record pairs a mapped Act with the e-Gov ids the importer needs for follow-up
// passes: RevisionID to fetch the act's full text (GET /api/2/law_data/{id}),
// and AmendedByLawID — the law_id that produced this revision — which the
// importer resolves into an eli:amended_by edge once the full law list is known.
type Record struct {
	Act            *schema.Act
	RevisionID     string
	AmendedByLawID string
}

// TypeSlug maps an e-Gov law_type to an ELI type_document slug. Known types use
// fixed slugs; any other (or empty) value falls back to the kebab-cased type,
// or "law" when empty — the scraper never drops an act over an unknown type.
func TypeSlug(lawType string) string {
	switch lawType {
	case "Constitution":
		return "constitution"
	case "Act":
		return "act"
	case "CabinetOrder":
		return "cabinet-order"
	case "ImperialOrder":
		return "imperial-order"
	case "MinisterialOrdinance":
		return "ministerial-ordinance"
	case "Rule":
		return "rule"
	case "":
		return "law"
	default:
		return kebab(lawType)
	}
}

// kebab converts a CamelCase identifier to kebab-case ("FooBar" -> "foo-bar").
func kebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// status resolves in-force state. A repealed revision wins; otherwise a current
// enforced revision is in force; anything else is unknown.
func status(ri revisionInfo) schema.Status {
	if ri.RepealStatus != "" && ri.RepealStatus != "None" {
		return schema.StatusRepealed
	}
	if ri.CurrentRevisionStatus == "CurrentEnforced" {
		return schema.StatusInForce
	}
	return schema.StatusUnknown
}

// parseDate parses a "yyyy-mm-dd" date as UTC. Empty input is a (false) miss.
func parseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// toAct maps one law entry into a schema.Act. It returns ok=false (drop) when
// the mandatory version date is absent — per the ontology, the scraper never
// guesses an as-of date.
func toAct(e lawEntry, retrievedAt time.Time) (*schema.Act, bool) {
	versionDate, ok := parseDate(e.RevisionInfo.EnforcementDate)
	if !ok {
		return nil, false
	}
	year := versionDate.Year()
	if p, ok := parseDate(e.LawInfo.PromulgationDate); ok {
		year = p.Year() // the work's identifying year is its promulgation year
	}
	return &schema.Act{
		Country:  "jp",
		TypeSlug: TypeSlug(e.LawInfo.LawType),
		Year:     year,
		Number:   e.LawInfo.LawID,
		IDLocal:  e.LawInfo.LawID,
		Expression: &schema.Expression{
			Title:       e.RevisionInfo.LawTitle,
			LangTag:     "ja",
			LangAlpha3:  "JPN",
			VersionDate: versionDate,
			Status:      status(e.RevisionInfo),
			SourceURL:   "https://laws.e-gov.go.jp/law/" + e.LawInfo.LawID,
			RetrievedAt: retrievedAt,
		},
	}, true
}

// BuildRecords decodes a GET /api/2/laws response and maps each law's current
// revision into a Record (Act + revision id). Entries without a version date
// are dropped.
func BuildRecords(lawsJSON []byte, retrievedAt time.Time) ([]Record, error) {
	var resp lawsResponse
	if err := json.Unmarshal(lawsJSON, &resp); err != nil {
		return nil, fmt.Errorf("egov: parse laws: %w", err)
	}
	out := make([]Record, 0, len(resp.Laws))
	for _, e := range resp.Laws {
		if act, ok := toAct(e, retrievedAt); ok {
			amendedBy := e.RevisionInfo.AmendmentLawID
			if amendedBy == e.LawInfo.LawID {
				amendedBy = "" // a self-reference (initial enactment) is not an amendment
			}
			out = append(out, Record{
				Act:            act,
				RevisionID:     e.RevisionInfo.LawRevisionID,
				AmendedByLawID: amendedBy,
			})
		}
	}
	return out, nil
}

// BuildActs is BuildRecords without the revision ids — the act values only.
func BuildActs(lawsJSON []byte, retrievedAt time.Time) ([]*schema.Act, error) {
	recs, err := BuildRecords(lawsJSON, retrievedAt)
	if err != nil {
		return nil, err
	}
	acts := make([]*schema.Act, len(recs))
	for i, r := range recs {
		acts[i] = r.Act
	}
	return acts, nil
}
