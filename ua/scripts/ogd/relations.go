package ogd

import (
	"bufio"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/tggo/lex/internal/schema"
)

// Relation type codes from the OGD `vidnosh` dictionary, mapped to ELI
// predicates. The codes describe the relation from the *source* act's view
// (its `links` field). Only the clear-cut amend/repeal codes are classified;
// everything else is the safe generic "cites" (a reference). See ADR-0027.
//
//	1  Змінює документ            -> amends
//	4  Скасовує документ          -> repeals
//	22 Визнає нечинним            -> repeals
//	25 Припиняє дію               -> repeals
//	29 Визнає нечинним крім …     -> repeals
//	others (0,2,3,6,17,26,…)      -> cites
type relKind int

const (
	relCite relKind = iota
	relAmend
	relRepeal
)

var relCodeKind = map[int]relKind{
	1:  relAmend,
	4:  relRepeal,
	22: relRepeal,
	25: relRepeal,
	29: relRepeal,
}

func kindOf(code int) relKind {
	if k, ok := relCodeKind[code]; ok {
		return k
	}
	return relCite
}

// Document type codes from the OGD `typ` dictionary, mapped to ELI type slugs.
// Unknown types fall back to the generic "akt".
var typeSlugByCode = map[int]string{
	1: "zakon", 150: "zakon",
	21: "kodeks", 124: "kodeks",
	100: "konstytutsiya", 216: "konstytutsiya",
	2: "postanova", 3: "ukaz", 9: "nakaz", 6: "rozporyadzhennya",
	22: "rishennya", 17: "uhoda", 20: "konventsiya", 18: "protokol",
	14: "statut", 11: "polozhennya", 13: "pravyla", 10: "instruktsiya", 16: "dohovir",
}

func slugForType(code int) string {
	if s, ok := typeSlugByCode[code]; ok {
		return s
	}
	return "akt"
}

// DocRef is the minimal identity of any document, from the global doc index.
type DocRef struct {
	Nreg     string
	TypeCode int
	Year     int
}

// ResourceURI mints the lex resource URI for a referenced document.
func (d DocRef) ResourceURI() string {
	return schema.ResourceURI("ua", slugForType(d.TypeCode), d.Year, d.Nreg)
}

// ParseDocIndex reads the global document-cards file (`doc.txt`, tab-separated,
// already decoded to UTF-8) into a dokid → DocRef map. Columns (per doc-stru):
// 0 dokid, 1 nreg, 2 title, 3 status, 4 types(|-list), 5 organs("orgid:date:num|…").
func ParseDocIndex(r io.Reader) (map[int]DocRef, error) {
	idx := make(map[int]DocRef)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24) // long lines (large titles)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 6 {
			continue
		}
		dokid, err := strconv.Atoi(cols[0])
		if err != nil {
			continue
		}
		idx[dokid] = DocRef{
			Nreg:     cols[1],
			TypeCode: firstInt(cols[4]),
			Year:     yearFromOrgans(cols[5]),
		}
	}
	return idx, sc.Err()
}

// firstInt parses the first element of a "|"-separated integer list.
func firstInt(field string) int {
	if field == "" {
		return 0
	}
	n, _ := strconv.Atoi(strings.SplitN(field, "|", 2)[0])
	return n
}

// yearFromOrgans extracts the adoption year from an organs field like
// "70:20260526:..." or "1:20030116:435-15|...".
func yearFromOrgans(field string) int {
	first := strings.SplitN(field, "|", 2)[0]
	parts := strings.Split(first, ":")
	if len(parts) < 2 || len(parts[1]) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(parts[1][:4])
	return y
}

// LinkRef is one resolved-or-not reference from a card's `links` field: a target
// document id with its relation-type codes.
type LinkRef struct {
	TargetDokid int
	Codes       []int
}

// ParseLinks parses a card `links` field of the form "<dokid>#code:count|…##".
func ParseLinks(field string) []LinkRef {
	field = strings.Trim(field, "#")
	if field == "" {
		return nil
	}
	parts := strings.SplitN(field, "#", 2)
	dokid, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil
	}
	ref := LinkRef{TargetDokid: dokid}
	if len(parts) > 1 {
		for _, pair := range strings.Split(parts[1], "|") {
			if c, err := strconv.Atoi(strings.SplitN(pair, ":", 2)[0]); err == nil {
				ref.Codes = append(ref.Codes, c)
			}
		}
	}
	return []LinkRef{ref}
}

// ResolveRelations turns a card's links into ELI relation edges (target resource
// URIs), classifying each by its relation-type codes. Targets absent from the
// index, or lacking a year, are skipped (we never mint a guessed URI).
func ResolveRelations(field string, docIdx map[int]DocRef) (amends, repeals, cites []string) {
	amSet, reSet, ciSet := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, l := range ParseLinks(field) {
		ref, ok := docIdx[l.TargetDokid]
		if !ok || ref.Year == 0 || ref.Nreg == "" {
			continue
		}
		uri := ref.ResourceURI()
		if len(l.Codes) == 0 {
			ciSet[uri] = true
			continue
		}
		for _, code := range l.Codes {
			switch kindOf(code) {
			case relAmend:
				amSet[uri] = true
			case relRepeal:
				reSet[uri] = true
			default:
				ciSet[uri] = true
			}
		}
	}
	return sortedKeys(amSet), sortedKeys(reSet), sortedKeys(ciSet)
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
