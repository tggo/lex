// Package ris parses the Austrian RIS OGD API (data.bka.gv.at/ris/api) responses
// into lex schema.Act values. It is pure and offline: HTTP fetching lives in the
// importer CLI, parsing/mapping lives here so it can be golden-tested without the
// network. See ADR-0023 and at/README.md for the API shape and the legal basis
// (RIS open data, CC BY 4.0 — Bundeskanzleramt).
//
// The RIS model differs from a 1:1 ELI API: the consolidated federal-law
// application "Bundesnormen" (Applikation=BrKons) stores each § (paragraph) of a
// law as a separate "Norm" document (a NOR id). All documents of one law share a
// stable Gesetzesnummer (the law's work id). So one lex schema.Act =
// one Gesetzesnummer, and each § document with body text = one schema.Article.
// The "§ 0" head document carries the law's title and Stammnorm metadata.
package ris

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Austria.
const Country schema.CountryCode = "at"

// ELI language metadata for (Austrian) German.
const (
	langTag    = "de"
	langAlpha3 = "DEU"
)

// SearchResult is the envelope of GET /Bundesrecht?Applikation=BrKons&...
// The RIS API serializes its internal XML to JSON, hence the nested shape and
// the "#text"/"@attr" conventions.
type SearchResult struct {
	OgdSearchResult struct {
		OgdDocumentResults struct {
			Hits struct {
				PageNumber string `json:"@pageNumber"`
				PageSize   string `json:"@pageSize"`
				Text       string `json:"#text"` // total hit count
			} `json:"Hits"`
			// RIS emits a single object when there is exactly one hit and an
			// array otherwise; decode lazily via json.RawMessage.
			Refs json.RawMessage `json:"OgdDocumentReference"`
		} `json:"OgdDocumentResults"`
	} `json:"OgdSearchResult"`
}

// DocumentReference is one § document within a law's result set.
type DocumentReference struct {
	Data struct {
		Metadaten struct {
			Technisch struct {
				ID string `json:"ID"` // NOR id
			} `json:"Technisch"`
			Allgemein struct {
				Veroeffentlicht string `json:"Veroeffentlicht"`
				Geaendert       string `json:"Geaendert"`
				DokumentUrl     string `json:"DokumentUrl"`
			} `json:"Allgemein"`
			Bundesrecht struct {
				Kurztitel string `json:"Kurztitel"`
				Titel     string `json:"Titel"`
				Eli       string `json:"Eli"`
				BrKons    BrKons `json:"BrKons"`
			} `json:"Bundesrecht"`
		} `json:"Metadaten"`
		Dokumentliste Dokumentliste `json:"Dokumentliste"`
	} `json:"Data"`
}

// BrKons holds the consolidated-federal-law specific metadata of a § document.
type BrKons struct {
	Kundmachungsorgan       string `json:"Kundmachungsorgan"`
	Typ                     string `json:"Typ"` // "BG", "V", "BVG", ...
	Dokumenttyp             string `json:"Dokumenttyp"`
	ArtikelParagraphAnlage  string `json:"ArtikelParagraphAnlage"`  // "§ 1", "§ 0"
	Paragraphnummer         string `json:"Paragraphnummer"`         // "1"
	StammnormBgblnummer     string `json:"StammnormBgblnummer"`     // "727/1990"
	Inkrafttretensdatum     string `json:"Inkrafttretensdatum"`     // YYYY-MM-DD
	Ausserkrafttretensdatum string `json:"Ausserkrafttretensdatum"` // YYYY-MM-DD (repealed)
	Gesetzesnummer          string `json:"Gesetzesnummer"`          // "10007061" (law work id)
}

// Dokumentliste carries the content-document URLs (XML/HTML/RTF/PDF).
type Dokumentliste struct {
	ContentReference struct {
		Urls struct {
			ContentUrl []ContentUrl `json:"ContentUrl"`
		} `json:"Urls"`
	} `json:"ContentReference"`
}

// ContentUrl is one downloadable rendering of a document.
type ContentUrl struct {
	DataType string `json:"DataType"` // "Xml", "Html", ...
	Url      string `json:"Url"`
}

// XMLURL returns the document's XML content URL, or "" if absent.
func (d *DocumentReference) XMLURL() string { return d.contentURL("Xml") }

func (d *DocumentReference) contentURL(dataType string) string {
	for _, u := range d.Data.Dokumentliste.ContentReference.Urls.ContentUrl {
		if u.DataType == dataType {
			return u.Url
		}
	}
	return ""
}

// IsHead reports whether this is the law's "§ 0" head document, which carries
// the title and Stammnorm metadata but no article body.
func (d *DocumentReference) IsHead() bool {
	return strings.TrimSpace(d.Data.Metadaten.Bundesrecht.BrKons.Paragraphnummer) == "0"
}

// ParseSearchResult decodes a Bundesrecht search page into the contained
// document references. RIS returns a bare object for a single hit and an array
// for many; both are handled.
func ParseSearchResult(b []byte) (*SearchResult, []DocumentReference, error) {
	var sr SearchResult
	if err := json.Unmarshal(b, &sr); err != nil {
		return nil, nil, fmt.Errorf("ris: parse search result: %w", err)
	}
	raw := sr.OgdSearchResult.OgdDocumentResults.Refs
	if len(raw) == 0 {
		return &sr, nil, nil
	}
	var many []DocumentReference
	if err := json.Unmarshal(raw, &many); err == nil {
		return &sr, many, nil
	}
	var one DocumentReference
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, nil, fmt.Errorf("ris: parse document references: %w", err)
	}
	return &sr, []DocumentReference{one}, nil
}

// TypeSlug maps an Austrian RIS document type (BrKons.Typ) plus the law's title
// to an ELI type_document slug. Laws whose title names them a "Gesetzbuch" (a
// code) get the "gesetzbuch" slug, mirroring the kodeks override in other
// countries.
func TypeSlug(typ, title string) string {
	lt := strings.ToLower(title)
	if strings.Contains(lt, "gesetzbuch") {
		return "gesetzbuch"
	}
	switch strings.ToUpper(strings.TrimSpace(typ)) {
	case "BG":
		return "bundesgesetz"
	case "BVG":
		return "bundesverfassungsgesetz"
	case "V":
		return "verordnung"
	case "K":
		return "kundmachung"
	case "VBG", "VEREINBARUNG":
		return "vereinbarung"
	case "":
		return "norm"
	default:
		return asciiSlug(typ)
	}
}

// asciiSlug folds a German label to an ASCII slug as a last-resort type slug.
func asciiSlug(s string) string {
	repl := strings.NewReplacer(
		"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", " ", "-", "/", "-",
	)
	out := repl.Replace(strings.ToLower(strings.TrimSpace(s)))
	if out == "" {
		return "norm"
	}
	return out
}

// yearFromBgbl extracts the promulgation year from a "<number>/<year>" Stammnorm
// reference (e.g. "727/1990" → 1990). Returns 0 if it cannot be parsed.
func yearFromBgbl(s string) int {
	parts := strings.Split(strings.TrimSpace(s), "/")
	if len(parts) == 0 {
		return 0
	}
	if y, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1])); err == nil {
		return y
	}
	return 0
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

// statusOf resolves a law's in-force status from its head document. An
// Ausserkrafttretensdatum (date no longer in force) means repealed; otherwise,
// if the law has an entry-into-force date, it is in force.
func statusOf(head BrKons) schema.Status {
	if strings.TrimSpace(head.Ausserkrafttretensdatum) != "" {
		return schema.StatusRepealed
	}
	if strings.TrimSpace(head.Inkrafttretensdatum) != "" {
		return schema.StatusInForce
	}
	return schema.StatusUnknown
}

// versionDate is the MANDATORY as-of date. RIS gives no single "consolidated as
// of" field, so we take the latest per-document "Geaendert" (last changed) date
// across the law's documents, falling back to the head's entry-into-force date.
func versionDate(docs []DocumentReference, head BrKons) time.Time {
	var latest time.Time
	for _, d := range docs {
		if t := parseDate(d.Data.Metadaten.Allgemein.Geaendert); t.After(latest) {
			latest = t
		}
	}
	if !latest.IsZero() {
		return latest
	}
	return parseDate(head.Inkrafttretensdatum)
}

// ToAct assembles a schema.Act for one law from its § documents (all sharing one
// Gesetzesnummer) and their parsed article bodies. articles is keyed by the
// document's NOR id. retrievedAt is recorded as lex:retrievedAt.
//
// Relations (amends/repeals/cites): RIS records changing acts only as free-text
// BGBl. references (BrKons.Aenderung, NovellenBeziehung), which carry no stable
// Gesetzesnummer for the target. They cannot be resolved to lex work URIs
// without a BGBl→Gesetzesnummer lookup, so relation edges are deferred to a
// later phase (see ADR-0023).
func ToAct(docs []DocumentReference, articles map[string]schema.Article, retrievedAt time.Time) (*schema.Act, error) {
	if len(docs) == 0 {
		return nil, fmt.Errorf("ris: no documents for act")
	}

	// The head (§ 0) document carries the law title and Stammnorm metadata.
	head := docs[0]
	for _, d := range docs {
		if d.IsHead() {
			head = d
			break
		}
	}
	hb := head.Data.Metadaten.Bundesrecht
	hk := hb.BrKons

	gn := hk.Gesetzesnummer
	if gn == "" {
		return nil, fmt.Errorf("ris: document %s has no Gesetzesnummer", head.Data.Metadaten.Technisch.ID)
	}

	title := hb.Kurztitel
	if title == "" {
		title = stripBR(hb.Titel)
	}

	exp := &schema.Expression{
		Title:            title,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      versionDate(docs, hk),
		FirstInForceDate: parseDate(hk.Inkrafttretensdatum),
		Status:           statusOf(hk),
		SourceURL:        gesamteRechtsvorschriftURL(gn),
		RetrievedAt:      retrievedAt,
	}
	if exp.Status == schema.StatusRepealed {
		exp.NoLongerInForce = parseDate(hk.Ausserkrafttretensdatum)
	}

	// Articles: one per non-head § document that has body text. Document order
	// is preserved; the store sorts articles numerically.
	for _, d := range docs {
		if d.IsHead() {
			continue
		}
		art, ok := articles[d.Data.Metadaten.Technisch.ID]
		if !ok {
			continue
		}
		if art.Number == "" {
			art.Number = strings.TrimSpace(d.Data.Metadaten.Bundesrecht.BrKons.Paragraphnummer)
		}
		if art.Label == "" {
			art.Label = strings.TrimSpace(d.Data.Metadaten.Bundesrecht.BrKons.ArtikelParagraphAnlage)
		}
		if art.Text == "" {
			continue
		}
		exp.Articles = append(exp.Articles, art)
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   TypeSlug(hk.Typ, title),
		Year:       yearFromBgbl(hk.StammnormBgblnummer),
		Number:     gn,
		IDLocal:    gn,
		Expression: exp,
	}, nil
}

// gesamteRechtsvorschriftURL builds the human consolidated-law page URL for a
// Gesetzesnummer.
func gesamteRechtsvorschriftURL(gn string) string {
	return "https://www.ris.bka.gv.at/GeltendeFassung.wxe?Abfrage=Bundesnormen&Gesetzesnummer=" + gn
}

// stripBR removes RIS "<br/>" separators (the Titel field joins the long title
// and the Stammnorm reference with one) and collapses whitespace.
func stripBR(s string) string {
	s = strings.ReplaceAll(s, "<br/>", " ")
	return strings.Join(strings.Fields(s), " ")
}
