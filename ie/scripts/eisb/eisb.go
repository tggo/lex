// Package eisb parses the electronic Irish Statute Book (irishstatutebook.ie)
// native-ELI act pages into lex schema.Act values. It is pure and offline: HTTP
// fetching lives in the importer, parsing/mapping lives here so it can be
// golden-tested without the network. See ADR-0022 and ie/README.md for the
// source shape and the legal basis (Oireachtas (Open Data) PSI Licence,
// incorporating CC BY 4.0).
//
// Ireland publishes a native ELI vocabulary: each act page carries RDFa
// <meta> elements (eli:title, eli:date_document, eli:type_document,
// eli:has_part, eli:changes, …), so the mapping to docs/ontology.md is close to
// 1:1. The acts themselves are enumerated from the Houses of the Oireachtas
// open-data API (api.oireachtas.ie/v1/legislation), which carries each act's
// statutebookURI (its eISB ELI). Articles are Akoma-Ntoso <section> elements,
// rendered in the eISB HTML as `<a name="secN">` anchors.
package eisb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Ireland.
const Country schema.CountryCode = "ie"

// ELI language metadata for the English text of Irish acts. Acts are also
// published in Irish (Gaeilge, "ga"/"GLE"); v1 ingests the English expression,
// which the eISB serves at the .../en/print.html path. See ADR-0022.
const (
	langTag    = "en"
	langAlpha3 = "ENG"
)

// eISBHost is the canonical host of the Irish Statute Book.
const eISBHost = "http://www.irishstatutebook.ie"

// ListItem is one act enumerated from the Oireachtas open-data legislation API.
type ListItem struct {
	Year          int    // act_year, e.g. 2015
	Number        string // act_no, e.g. "60"
	Title         string // shortTitleEn
	StatuteBookID string // path part of statutebookURI, e.g. "2015/act/60"
}

// listEnvelope mirrors the subset of GET /v1/legislation we consume.
type listEnvelope struct {
	Head struct {
		Counts struct {
			ResultCount int `json:"resultCount"`
		} `json:"counts"`
	} `json:"head"`
	Results []struct {
		Bill struct {
			Act struct {
				ActYear        string `json:"actYear"`
				ActNo          string `json:"actNo"`
				ShortTitleEn   string `json:"shortTitleEn"`
				StatuteBookURI string `json:"statutebookURI"`
			} `json:"act"`
		} `json:"bill"`
	} `json:"results"`
}

// ActList is the parsed, paginated act listing.
type ActList struct {
	ResultCount int        // total acts for the queried year
	Items       []ListItem // acts on this page
}

// ParseActList decodes a legislation listing page from the Oireachtas API.
func ParseActList(b []byte) (*ActList, error) {
	var env listEnvelope
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, fmt.Errorf("eisb: parse act list: %w", err)
	}
	out := &ActList{ResultCount: env.Head.Counts.ResultCount}
	for _, r := range env.Results {
		a := r.Bill.Act
		if a.ActNo == "" || a.ActYear == "" {
			continue
		}
		y, err := strconv.Atoi(a.ActYear)
		if err != nil {
			continue
		}
		out.Items = append(out.Items, ListItem{
			Year:          y,
			Number:        a.ActNo,
			Title:         strings.Join(strings.Fields(a.ShortTitleEn), " "),
			StatuteBookID: statuteBookID(a.StatuteBookURI),
		})
	}
	return out, nil
}

// statuteBookID extracts the ELI path ("2015/act/60") from a statutebookURI
// like "http://www.irishstatutebook.ie/eli/2015/act/60".
func statuteBookID(uri string) string {
	if i := strings.Index(uri, "/eli/"); i >= 0 {
		return strings.Trim(uri[i+len("/eli/"):], "/")
	}
	return ""
}

// PrintPath returns the eISB English print-HTML path (without host) for an act
// ELI path ("2015/act/60"). This page carries the native-ELI RDFa metadata plus
// the section bodies.
func PrintPath(statuteBookID string) string {
	return "/eli/" + statuteBookID + "/enacted/en/print.html"
}

// PrintURL returns the absolute eISB English print-HTML URL for an act ELI path.
func PrintURL(statuteBookID string) string {
	return eISBHost + PrintPath(statuteBookID)
}

// Meta holds the RDFa eli:* facts extracted from an act page <head>.
type Meta struct {
	TypeDocument string   // eli:type_document resource (…resource-type#ACT)
	Title        string   // eli:title
	DateDocument string   // eli:date_document (YYYY-MM-DD) — the signing/enactment date
	Number       string   // eli:number
	ELIPath      string   // act ELI path ("2015/act/60"), recovered from a has_part/realizes URI
	Changes      []string // eli:changes targets (act ELI paths) — acts this act amends
}

// ParseAct parses an eISB act print page into a schema.Act. retrievedAt is
// recorded as lex:retrievedAt.
func ParseAct(htmlBytes []byte, retrievedAt time.Time) (*schema.Act, error) {
	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return nil, fmt.Errorf("eisb: parse html: %w", err)
	}
	meta := extractMeta(doc)
	if meta.ELIPath == "" {
		return nil, fmt.Errorf("eisb: could not determine act ELI from page metadata")
	}
	year, typeWord, num, err := splitELIPath(meta.ELIPath)
	if err != nil {
		return nil, err
	}

	slug := TypeSlug(meta.TypeDocument, typeWord)
	arts := extractSections(doc)

	exp := &schema.Expression{
		Title:            meta.Title,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      parseDate(meta.DateDocument),
		FirstInForceDate: parseDate(meta.DateDocument),
		Status:           schema.StatusInForce, // eISB serves the in-force consolidated text; repeals are a next-phase edge
		SourceURL:        eISBHost + "/eli/" + meta.ELIPath,
		RetrievedAt:      retrievedAt,
		Articles:         arts,
	}

	// eli:changes → the acts this act amends. Targets are resolved to lex work
	// URIs via their recovered ELI path. Sorted for determinism.
	changes := append([]string(nil), meta.Changes...)
	sort.Strings(changes)
	for _, c := range changes {
		uri, err := resourceURIFromELIPath(c)
		if err != nil {
			continue // unrecognised target shape; skip rather than fail the act
		}
		exp.Amends = append(exp.Amends, uri)
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   slug,
		Year:       year,
		Number:     num,
		IDLocal:    meta.ELIPath,
		Expression: exp,
	}, nil
}

// splitELIPath splits "2015/act/60" into (2015, "act", "60").
func splitELIPath(p string) (year int, typeWord, number string, err error) {
	parts := strings.Split(p, "/")
	if len(parts) != 3 {
		return 0, "", "", fmt.Errorf("eisb: malformed ELI path %q", p)
	}
	y, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", "", fmt.Errorf("eisb: ELI path %q has bad year: %w", p, err)
	}
	return y, parts[1], parts[2], nil
}

// resourceURIFromELIPath builds the lex work URI for an act ELI path
// ("1988/act/27"). Number is the act number within its year.
func resourceURIFromELIPath(p string) (string, error) {
	year, typeWord, number, err := splitELIPath(p)
	if err != nil {
		return "", err
	}
	return schema.ResourceURI(Country, TypeSlug("", typeWord), year, number), nil
}

// TypeSlug maps an eISB act type to an ELI type_document slug. typeDocResource
// is the eli:type_document resource URI (…resource-type#ACT / #SI); typeWord is
// the ELI-path type token ("act", "si"). The resource takes precedence.
func TypeSlug(typeDocResource, typeWord string) string {
	switch {
	case strings.HasSuffix(typeDocResource, "#ACT"):
		return "act"
	case strings.HasSuffix(typeDocResource, "#SI"):
		return "si"
	}
	switch strings.ToLower(strings.TrimSpace(typeWord)) {
	case "act":
		return "act"
	case "si":
		return "si"
	default:
		return "act"
	}
}

// parseDate parses a YYYY-MM-DD date (UTC). Empty/unparseable input yields the
// zero time (the store rejects a zero version_date, per the ontology invariant).
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
