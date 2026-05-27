// Package ogd parses the Verkhovna Rada open-data "primary acts" datasets
// (cards.json, texts.json, perv*.txt) into lex schema.Act values. It is pure
// and offline: fetching lives in the importer CLI, parsing/mapping lives here
// so it can be golden-tested without the network. See ADR-0009 and
// ua/README.md for the dataset shape and license (CC BY 4.0).
package ogd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Card is a record from cards.json (only the fields lex uses).
type Card struct {
	Dokid  int    `json:"dokid"`  // internal document id, joins to TextInfo
	Nreg   string `json:"nreg"`   // registration number, e.g. "4840-20" (our Number)
	NVlas  string `json:"n_vlas"` // official act number, e.g. "4840-IX" (IDLocal)
	Nazva  string `json:"nazva"`  // title
	Orgdat int    `json:"orgdat"` // adoption date, yyyymmdd
	Pridat int    `json:"pridat"` // acceptance date, yyyymmdd
	Perv   int    `json:"perv"`   // which list: 0 repealed, 1 primary, 2 treaty
	Links  string `json:"links"`  // references to other acts (deferred)
}

// TextInfo is a record from texts.json: the current redaction of an act's text.
type TextInfo struct {
	Dokid  int    `json:"dokid"`
	Datred int    `json:"datred"` // redaction date, yyyymmdd — the as-of date
	File   string `json:"file"`   // e.g. "d553665.htm"
	Nreg   string `json:"nreg"`
}

// ParseCards decodes cards.json.
func ParseCards(b []byte) ([]Card, error) {
	var cs []Card
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, fmt.Errorf("ogd: parse cards: %w", err)
	}
	return cs, nil
}

// ParseTexts decodes texts.json.
func ParseTexts(b []byte) ([]TextInfo, error) {
	var ts []TextInfo
	if err := json.Unmarshal(b, &ts); err != nil {
		return nil, fmt.Errorf("ogd: parse texts: %w", err)
	}
	return ts, nil
}

// ParseIDList reads a perv*.txt list of registration numbers, one per line,
// into a set.
func ParseIDList(b []byte) map[string]bool {
	out := map[string]bool{}
	for _, ln := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			out[s] = true
		}
	}
	return out
}

// StatusIndex resolves an act's in-force status from the OGD lists. perv1 and
// perv2 (treaties) are in force; perv0 is repealed/inactive.
type StatusIndex struct {
	inForce  map[string]bool
	repealed map[string]bool
}

// NewStatusIndex builds an index. Pass the union of perv1+perv2 as inForce.
func NewStatusIndex(inForce, repealed map[string]bool) StatusIndex {
	return StatusIndex{inForce: inForce, repealed: repealed}
}

// Status reports the status of a registration number.
func (si StatusIndex) Status(nreg string) schema.Status {
	switch {
	case si.repealed[nreg]:
		return schema.StatusRepealed
	case si.inForce[nreg]:
		return schema.StatusInForce
	default:
		return schema.StatusUnknown
	}
}

// TypeSlug derives the ELI act-type slug from the title. All primary acts are
// formally laws; codes and the Constitution are distinguished by name.
func TypeSlug(nazva string) string {
	low := strings.ToLower(nazva)
	switch {
	case strings.Contains(low, "конституц"):
		return "konstytutsiya"
	case strings.Contains(low, "кодекс"):
		return "kodeks"
	default:
		return "zakon"
	}
}

// parseYmd converts a yyyymmdd integer to a UTC date.
func parseYmd(n int) (time.Time, error) {
	if n <= 0 {
		return time.Time{}, fmt.Errorf("ogd: empty date")
	}
	y, m, d := n/10000, (n/100)%100, n%100
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, fmt.Errorf("ogd: invalid date %d", n)
	}
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC), nil
}

// ToAct maps one card (with its optional text record and resolved status) into
// a schema.Act. VersionDate comes from the text redaction date when available,
// otherwise the adoption date. FirstInForceDate, articles, and relations are
// not populated here (separate ingestion passes).
func ToAct(c Card, t *TextInfo, st schema.Status, retrievedAt time.Time) (*schema.Act, error) {
	if c.Nreg == "" {
		return nil, fmt.Errorf("ogd: card dokid=%d has no nreg", c.Dokid)
	}
	adopted, err := parseYmd(c.Orgdat)
	if err != nil {
		return nil, fmt.Errorf("ogd: card %s orgdat: %w", c.Nreg, err)
	}
	versionDate := adopted
	if t != nil && t.Datred > 0 {
		if d, derr := parseYmd(t.Datred); derr == nil {
			versionDate = d
		}
	}
	return &schema.Act{
		Country:  "ua",
		TypeSlug: TypeSlug(c.Nazva),
		Year:     adopted.Year(),
		Number:   c.Nreg,
		IDLocal:  c.NVlas,
		Expression: &schema.Expression{
			Title:       c.Nazva,
			LangTag:     "uk",
			LangAlpha3:  "UKR",
			VersionDate: versionDate,
			Status:      st,
			SourceURL:   "https://zakon.rada.gov.ua/laws/show/" + c.Nreg,
			RetrievedAt: retrievedAt,
		},
	}, nil
}

// BuildActs parses the cards and texts datasets, joins them by document id, and
// maps each into a schema.Act with status resolved from si.
func BuildActs(cardsJSON, textsJSON []byte, si StatusIndex, retrievedAt time.Time) ([]*schema.Act, error) {
	cards, err := ParseCards(cardsJSON)
	if err != nil {
		return nil, err
	}
	texts, err := ParseTexts(textsJSON)
	if err != nil {
		return nil, err
	}
	byDok := make(map[int]TextInfo, len(texts))
	for _, t := range texts {
		byDok[t.Dokid] = t
	}
	out := make([]*schema.Act, 0, len(cards))
	for _, c := range cards {
		var tp *TextInfo
		if t, ok := byDok[c.Dokid]; ok {
			tp = &t
		}
		act, err := ToAct(c, tp, si.Status(c.Nreg), retrievedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, act)
	}
	return out, nil
}
