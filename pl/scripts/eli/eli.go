// Package eli parses the Polish Sejm ELI API (api.sejm.gov.pl/eli) responses
// into lex schema.Act values. It is pure and offline: HTTP fetching lives in
// the importer CLI, parsing/mapping lives here so it can be golden-tested
// without the network. See ADR-0012 and pl/README.md for the API shape and the
// legal basis (Polish normative acts are not objects of copyright).
//
// Poland publishes a native ELI API, so the mapping to docs/ontology.md is
// close to 1:1: each act's ELI ("DU/2023/2777") gives publisher + year +
// position, and the references endpoint gives amends/repeals/cites edges.
package eli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Poland.
const Country schema.CountryCode = "pl"

// ELI language metadata for Polish.
const (
	langTag    = "pl"
	langAlpha3 = "POL"
)

// ListItem is one entry from GET /eli/acts/{publisher}/{year}.
type ListItem struct {
	ELI       string `json:"ELI"` // e.g. "DU/2023/2777"
	Publisher string `json:"publisher"`
	Year      int    `json:"year"`
	Pos       int    `json:"pos"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	TextHTML  bool   `json:"textHTML"`
}

// ActList is the paginated envelope returned by the year listing endpoint.
type ActList struct {
	Count      int        `json:"count"`      // items in this page
	TotalCount int        `json:"totalCount"` // total acts for the publisher/year
	Offset     int        `json:"offset"`
	Items      []ListItem `json:"items"`
}

// PublisherInfo is GET /eli/acts/{publisher}: the years available.
type PublisherInfo struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	Years     []int  `json:"years"`
}

// Detail is GET /eli/acts/{publisher}/{year}/{pos}: a single act's metadata.
type Detail struct {
	ELI            string `json:"ELI"`
	Publisher      string `json:"publisher"`
	Year           int    `json:"year"`
	Pos            int    `json:"pos"`
	Type           string `json:"type"`
	Title          string `json:"title"`
	Status         string `json:"status"`       // free text, e.g. "obowiązujący"
	InForce        string `json:"inForce"`      // "IN_FORCE" / "NOT_IN_FORCE"
	Promulgation   string `json:"promulgation"` // YYYY-MM-DD
	Announcement   string `json:"announcementDate"`
	EntryIntoForce string `json:"entryIntoForce"` // YYYY-MM-DD
	ChangeDate     string `json:"changeDate"`     // YYYY-MM-DDThh:mm:ss
	TextHTML       bool   `json:"textHTML"`
}

// refEntry is one target inside a references group.
type refEntry struct {
	Act struct {
		ELI   string `json:"ELI"`
		Type  string `json:"type"`
		Title string `json:"title"`
		Year  int    `json:"year"`
		Pos   int    `json:"pos"`
	} `json:"act"`
}

// References maps Polish relation names (e.g. "Akty zmienione") to their
// targets. It is GET /eli/acts/{publisher}/{year}/{pos}/references.
type References map[string][]refEntry

// ParseActList decodes a year-listing page.
func ParseActList(b []byte) (*ActList, error) {
	var l ActList
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, fmt.Errorf("eli: parse act list: %w", err)
	}
	return &l, nil
}

// ParsePublisherInfo decodes a publisher index (available years).
func ParsePublisherInfo(b []byte) (*PublisherInfo, error) {
	var p PublisherInfo
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("eli: parse publisher info: %w", err)
	}
	return &p, nil
}

// ParseDetail decodes an act detail document.
func ParseDetail(b []byte) (*Detail, error) {
	var d Detail
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("eli: parse detail: %w", err)
	}
	return &d, nil
}

// ParseReferences decodes the references document.
func ParseReferences(b []byte) (References, error) {
	var r References
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("eli: parse references: %w", err)
	}
	return r, nil
}

// parseELI splits an ELI like "DU/2023/2777" into its parts.
func parseELI(eli string) (publisher string, year int, pos string, err error) {
	parts := strings.Split(eli, "/")
	if len(parts) != 3 {
		return "", 0, "", fmt.Errorf("eli: malformed ELI %q", eli)
	}
	y, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, "", fmt.Errorf("eli: ELI %q has bad year: %w", eli, err)
	}
	return parts[0], y, parts[2], nil
}

// TypeSlug maps a Polish act type (plus its title) to an ELI type_document
// slug. Codes are formally a "Ustawa" but identified by the word "kodeks" in
// the title, mirroring the UA approach.
func TypeSlug(actType, title string) string {
	if strings.Contains(strings.ToLower(title), "kodeks") {
		return "kodeks"
	}
	switch t := strings.ToLower(strings.TrimSpace(actType)); t {
	case "ustawa":
		return "ustawa"
	case "rozporządzenie":
		return "rozporzadzenie"
	case "obwieszczenie":
		return "obwieszczenie"
	case "uchwała":
		return "uchwala"
	case "zarządzenie":
		return "zarzadzenie"
	case "umowa międzynarodowa":
		return "umowa-miedzynarodowa"
	default:
		return asciiSlug(t)
	}
}

// asciiSlug folds a Polish label to an ASCII slug as a last-resort type slug.
func asciiSlug(s string) string {
	repl := strings.NewReplacer(
		"ą", "a", "ć", "c", "ę", "e", "ł", "l", "ń", "n",
		"ó", "o", "ś", "s", "ź", "z", "ż", "z", " ", "-",
	)
	out := repl.Replace(strings.ToLower(strings.TrimSpace(s)))
	if out == "" {
		return "akt"
	}
	return out
}

// resourceURIFromELI builds the lex work URI for an act identified by ELI and
// type. Number is "<publisher>/<pos>" (e.g. "DU/2777"); the schema's per-segment
// escaping keeps the slash inside one path segment.
func resourceURIFromELI(eli, actType, title string) (string, error) {
	pub, year, pos, err := parseELI(eli)
	if err != nil {
		return "", err
	}
	return schema.ResourceURI(Country, TypeSlug(actType, title), year, pub+"/"+pos), nil
}

// statusOf resolves an act's in-force status, preferring the inForce enum and
// falling back to the free-text status field.
func statusOf(d *Detail) schema.Status {
	switch strings.ToUpper(d.InForce) {
	case "IN_FORCE":
		return schema.StatusInForce
	case "NOT_IN_FORCE":
		return schema.StatusRepealed
	}
	switch strings.ToLower(d.Status) {
	case "obowiązujący":
		return schema.StatusInForce
	case "uchylony", "uznany za uchylony", "wygaśnięcie aktu":
		return schema.StatusRepealed
	default:
		return schema.StatusUnknown
	}
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

// versionDate is the MANDATORY as-of date. The Sejm API does not expose a clean
// "consolidated as of" field, so we use changeDate (when the act record/text
// last changed) and fall back to the promulgation date.
func versionDate(d *Detail) time.Time {
	if d.ChangeDate != "" {
		if t, err := time.Parse("2006-01-02T15:04:05", d.ChangeDate); err == nil {
			return t.UTC()
		}
		// Some payloads use "YYYY-MM-DD hh:mm" — take the date part.
		if len(d.ChangeDate) >= 10 {
			if t := parseDate(d.ChangeDate[:10]); !t.IsZero() {
				return t
			}
		}
	}
	return parseDate(d.Promulgation)
}

// relationFor maps a Polish references group name to a schema relation
// predicate kind. The second result is false for groups lex does not model.
func relationFor(group string) (kind string, ok bool) {
	switch group {
	case "Akty zmienione":
		return "amends", true
	case "Akty uchylone":
		return "repeals", true
	case "Podstawa prawna", "Podstawa prawna z art.":
		return "cites", true
	default:
		return "", false
	}
}

// ToAct assembles a schema.Act from an act detail, its references, and its
// parsed articles. retrievedAt is recorded as lex:retrievedAt.
func ToAct(d *Detail, refs References, articles []schema.Article, retrievedAt time.Time) (*schema.Act, error) {
	pub, year, pos, err := parseELI(d.ELI)
	if err != nil {
		return nil, err
	}
	slug := TypeSlug(d.Type, d.Title)

	exp := &schema.Expression{
		Title:            d.Title,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      versionDate(d),
		FirstInForceDate: parseDate(d.EntryIntoForce),
		Status:           statusOf(d),
		SourceURL:        "https://eli.gov.pl/eli/" + d.ELI,
		RetrievedAt:      retrievedAt,
		Articles:         articles,
	}

	// Resolve references into relation edges. Iterate groups in sorted order so
	// the emitted edge slices are deterministic (map order is not).
	groups := make([]string, 0, len(refs))
	for g := range refs {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	for _, group := range groups {
		kind, ok := relationFor(group)
		if !ok {
			continue
		}
		for _, e := range refs[group] {
			if e.Act.ELI == "" {
				continue
			}
			uri, err := resourceURIFromELI(e.Act.ELI, e.Act.Type, e.Act.Title)
			if err != nil {
				return nil, fmt.Errorf("eli: reference of %s: %w", d.ELI, err)
			}
			switch kind {
			case "amends":
				exp.Amends = append(exp.Amends, uri)
			case "repeals":
				exp.Repeals = append(exp.Repeals, uri)
			case "cites":
				exp.Cites = append(exp.Cites, uri)
			}
		}
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   slug,
		Year:       year,
		Number:     pub + "/" + pos,
		IDLocal:    d.ELI,
		Expression: exp,
	}, nil
}
