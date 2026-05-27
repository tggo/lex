// Package frl parses Federal Register of Legislation responses
// (api.prod.legislation.gov.au) into lex schema.Act values. It is pure and
// offline: HTTP fetching lives in the importer, parsing/mapping lives here so
// it can be golden-tested without the network. See ADR-0024 and au/README.md
// for the API shape and the legal basis (Commonwealth legislative material is
// published under CC BY 4.0).
//
// Australia exposes a clean OData JSON API. Each title (act/regulation) has a
// stable register id (e.g. "C1901A00002") giving year + number; the current
// Version gives the point-in-time as-of date, in-force status, and the
// amend/repeal edges that affected it.
package frl

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Australia.
const Country schema.CountryCode = "au"

// ELI language metadata for Australian English.
const (
	langTag    = "en"
	langAlpha3 = "ENG"
)

// SourceBase is the human-facing page base; lex:sourceURL = SourceBase + id.
const SourceBase = "https://www.legislation.gov.au/"

// ListItem is one entry from GET /v1/titles (the OData Titles collection).
type ListItem struct {
	ID         string `json:"id"` // register id, e.g. "C1901A00002"
	Name       string `json:"name"`
	Collection string `json:"collection"` // "Act", "LegislativeInstrument", …
	SeriesType string `json:"seriesType"`
	Year       int    `json:"year"`
	Number     int    `json:"number"`
	Status     string `json:"status"`
	IsInForce  bool   `json:"isInForce"`
}

// TitleList is the OData envelope returned by GET /v1/titles.
type TitleList struct {
	Count int        `json:"@odata.count"` // total matching titles (with $count=true)
	Value []ListItem `json:"value"`
}

// Detail is GET /v1/titles/{id}: a single title's metadata.
type Detail struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Collection  string `json:"collection"`
	SeriesType  string `json:"seriesType"`
	Year        int    `json:"year"`
	Number      int    `json:"number"`
	Status      string `json:"status"` // "InForce", "Repealed", …
	IsInForce   bool   `json:"isInForce"`
	IsPrincipal bool   `json:"isPrincipal"`
	MakingDate  string `json:"makingDate"` // YYYY-MM-DDThh:mm:ss
}

// affectedByTitle is the target act of a version reason.
type affectedByTitle struct {
	TitleID    string `json:"titleId"`
	Name       string `json:"name"`
	Year       int    `json:"year"`
	Number     int    `json:"number"`
	SeriesType string `json:"seriesType"`
}

// reason is one entry in a Version's reasons array: an act that affected this
// title (amended or repealed it) at this version's start.
type reason struct {
	Affect          string           `json:"affect"` // "Amend", "Repeal", …
	Markdown        string           `json:"markdown"`
	AffectedByTitle *affectedByTitle `json:"affectedByTitle"`
}

// Version is GET /v1/Versions/Default.Find(...): a point-in-time compilation.
// Its Start is the as-of date; reasons carry the edges that produced it.
type Version struct {
	TitleID           string   `json:"titleId"`
	Start             string   `json:"start"` // YYYY-MM-DDThh:mm:ss — version as-of date
	End               string   `json:"end"`
	IsCurrent         bool     `json:"isCurrent"`
	Name              string   `json:"name"`
	Status            string   `json:"status"`
	RegisterID        string   `json:"registerId"`
	CompilationNumber string   `json:"compilationNumber"`
	Reasons           []reason `json:"reasons"`
}

// ParseTitleList decodes a GET /v1/titles page.
func ParseTitleList(b []byte) (*TitleList, error) {
	var l TitleList
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("frl: parse title list: %w", err)
	}
	return &l, nil
}

// ParseDetail decodes a title detail document.
func ParseDetail(b []byte) (*Detail, error) {
	var d Detail
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("frl: parse detail: %w", err)
	}
	return &d, nil
}

// ParseVersion decodes a single Version document.
func ParseVersion(b []byte) (*Version, error) {
	var v Version
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("frl: parse version: %w", err)
	}
	return &v, nil
}

// TypeSlug maps an FRL collection/seriesType to an ELI type_document slug.
func TypeSlug(collection, seriesType string) string {
	c := strings.TrimSpace(collection)
	if c == "" {
		c = seriesType
	}
	switch strings.ToLower(c) {
	case "act":
		return "act"
	case "legislativeinstrument", "legislative instrument":
		return "legislative-instrument"
	case "notifiableinstrument", "notifiable instrument":
		return "notifiable-instrument"
	case "administrativearrangementsorder", "prerogativeinstrument":
		return asciiSlug(c)
	case "constitution":
		return "constitution"
	default:
		return asciiSlug(c)
	}
}

// asciiSlug folds a CamelCase/spaced label to a hyphenated lowercase slug.
func asciiSlug(s string) string {
	var b strings.Builder
	prevLower := false
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= 'A' && r <= 'Z':
			if prevLower {
				b.WriteByte('-')
			}
			b.WriteRune(r + ('a' - 'A'))
			prevLower = false
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevLower = true
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
			prevLower = false
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "act"
	}
	return out
}

// statusOf resolves an FRL status string to a schema.Status.
func statusOf(status string) schema.Status {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "inforce", "in force":
		return schema.StatusInForce
	case "repealed", "ceased", "expired", "notinforce", "no longer in force":
		return schema.StatusRepealed
	default:
		return schema.StatusUnknown
	}
}

// parseDateTime parses an FRL "YYYY-MM-DDThh:mm:ss" timestamp (UTC). It also
// accepts a bare "YYYY-MM-DD". Empty input yields the zero time.
func parseDateTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.UTC()
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// versionDate is the MANDATORY as-of date. We use the current Version's start
// (when this compilation took effect), falling back to the detail's makingDate.
func versionDate(v *Version, d *Detail) time.Time {
	if v != nil {
		if t := parseDateTime(v.Start); !t.IsZero() {
			return t
		}
	}
	if d != nil {
		return parseDateTime(d.MakingDate)
	}
	return time.Time{}
}

// resourceURIForTitle builds the lex work URI for an affected/affecting title.
// Number is the register id (globally unique and stable on the FRL).
func resourceURIForTitle(t *affectedByTitle) string {
	return schema.ResourceURI(Country, TypeSlug("", t.SeriesType), t.Year, t.TitleID)
}

// ToAct assembles a schema.Act from a title detail and its current version.
// version may be nil (no compilation found); then status/version_date fall back
// to the detail. retrievedAt is recorded as lex:retrievedAt.
//
// Articles (sections) are intentionally absent: the FRL API exposes full text
// only as binary Word/PDF/Epub, with no structured section channel, so section
// text is deferred to a later phase (see ADR-0024).
func ToAct(d *Detail, v *Version, retrievedAt time.Time) (*schema.Act, error) {
	if d == nil || d.ID == "" {
		return nil, fmt.Errorf("frl: nil or unidentified detail")
	}

	status := statusOf(d.Status)
	if v != nil {
		if s := statusOf(v.Status); s != schema.StatusUnknown {
			status = s
		}
	}

	exp := &schema.Expression{
		Title:            d.Name,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      versionDate(v, d),
		FirstInForceDate: parseDateTime(d.MakingDate),
		Status:           status,
		SourceURL:        SourceBase + d.ID,
		RetrievedAt:      retrievedAt,
	}

	// A version's reasons list the acts that AFFECTED this title (this title was
	// amended/repealed BY them). Iterate in a deterministic order.
	if v != nil {
		rs := make([]reason, len(v.Reasons))
		copy(rs, v.Reasons)
		sort.SliceStable(rs, func(i, j int) bool {
			ti, tj := rs[i].AffectedByTitle, rs[j].AffectedByTitle
			ii, jj := "", ""
			if ti != nil {
				ii = ti.TitleID
			}
			if tj != nil {
				jj = tj.TitleID
			}
			return ii < jj
		})
		for _, r := range rs {
			if r.AffectedByTitle == nil || r.AffectedByTitle.TitleID == "" {
				continue
			}
			uri := resourceURIForTitle(r.AffectedByTitle)
			switch strings.ToLower(r.Affect) {
			case "amend":
				exp.AmendedBy = append(exp.AmendedBy, uri)
			case "repeal":
				exp.RepealedBy = append(exp.RepealedBy, uri)
			}
		}
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   TypeSlug(d.Collection, d.SeriesType),
		Year:       d.Year,
		Number:     d.ID,
		IDLocal:    d.ID,
		Expression: exp,
	}, nil
}
