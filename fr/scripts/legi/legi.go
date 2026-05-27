// Package legi parses the French DILA LEGI open-data bulk dataset (consolidated
// codes, laws and regulations) into lex schema.Act values. It is pure and
// offline: archive download / file walking lives in the importer, parsing and
// mapping live here so they can be golden-tested without the network.
//
// LEGI is distributed as a tarball of one XML file per object. The objects we
// care about are:
//
//   - TEXTE_VERSION (LEGITEXT…): the consolidated version of a text — title,
//     nature, validity dates, in-force state. This is the FRBR expression.
//   - TEXTELR (the "struct" of a LEGITEXT…): the table of contents, listing the
//     member articles via <LIEN_ART> elements (id, num, etat, debut, fin).
//   - ARTICLE (LEGIARTI…): one article — number, state, dates, text body
//     (<BLOC_TEXTUEL><CONTENU>) and outgoing <LIENS> (citations / amendments).
//
// French legislative texts (lois, décrets, codes) are not protected by
// copyright (CPI art. L.122-5 2°); the dataset is published under the Licence
// Ouverte / Open Licence (Etalab), attribution to DILA. See ADR-0016 and
// fr/README.md.
package legi

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for France.
const Country schema.CountryCode = "fr"

// ELI language metadata for French.
const (
	langTag    = "fr"
	langAlpha3 = "FRA"

	// dateLayout is the LEGI date format (YYYY-MM-DD).
	dateLayout = "2006-01-02"

	// openEndDate is LEGI's sentinel "no end" date; treated as still-in-force.
	openEndDate = "2999-01-01"
)

// TexteVersion is a LEGI TEXTE_VERSION document: the consolidated expression of
// a text (a code, loi, décret…).
type TexteVersion struct {
	XMLName xml.Name `xml:"TEXTE_VERSION"`
	Common  struct {
		ID      string `xml:"ID"`
		Origine string `xml:"ORIGINE"`
		Nature  string `xml:"NATURE"`
	} `xml:"META>META_COMMUN"`
	Chronicle struct {
		CID       string `xml:"CID"`
		Num       string `xml:"NUM"`
		DatePubli string `xml:"DATE_PUBLI"`
		DateTexte string `xml:"DATE_TEXTE"`
	} `xml:"META>META_SPEC>META_TEXTE_CHRONICLE"`
	Version struct {
		Titre     string `xml:"TITRE"`
		TitreFull string `xml:"TITREFULL"`
		Nature    string `xml:"NATURE"`
		DateDebut string `xml:"DATE_DEBUT"`
		DateFin   string `xml:"DATE_FIN"`
		Etat      string `xml:"ETAT"`
	} `xml:"META>META_SPEC>META_TEXTE_VERSION"`
}

// LienArt is a <LIEN_ART> entry inside a TEXTELR STRUCT: a pointer to one
// member article version.
type LienArt struct {
	ID     string `xml:"id,attr"`
	Num    string `xml:"num,attr"`
	Etat   string `xml:"etat,attr"`
	Debut  string `xml:"debut,attr"`
	Fin    string `xml:"fin,attr"`
	Origin string `xml:"origine,attr"`
}

// TexteStruct is a LEGI TEXTELR document: the structure (table of contents) of
// a text, listing its member articles.
type TexteStruct struct {
	XMLName xml.Name  `xml:"TEXTELR"`
	Liens   []LienArt `xml:"STRUCT>LIEN_ART"`
}

// Lien is an outgoing <LIEN> from an article to another text/article.
type Lien struct {
	CIDTexte string `xml:"cidtexte,attr"`
	ID       string `xml:"id,attr"`
	Num      string `xml:"num,attr"`
	Sens     string `xml:"sens,attr"`     // "cite", "modifie", "abroge", …
	TypeLien string `xml:"typelien,attr"` // "CITATION", "MODIFICATION", "ABROGATION", …
	Texte    string `xml:",chardata"`
}

// Article is a LEGI ARTICLE document: one article version with its text and
// outgoing links.
type Article struct {
	XMLName xml.Name `xml:"ARTICLE"`
	Common  struct {
		ID      string `xml:"ID"`
		Origine string `xml:"ORIGINE"`
		Nature  string `xml:"NATURE"`
	} `xml:"META>META_COMMUN"`
	Meta struct {
		Num       string `xml:"NUM"`
		Etat      string `xml:"ETAT"`
		DateDebut string `xml:"DATE_DEBUT"`
		DateFin   string `xml:"DATE_FIN"`
	} `xml:"META>META_SPEC>META_ARTICLE"`
	Contenu string `xml:"BLOC_TEXTUEL>CONTENU"`
	Liens   []Lien `xml:"LIENS>LIEN"`
}

// ParseTexteVersion decodes a TEXTE_VERSION document.
func ParseTexteVersion(b []byte) (*TexteVersion, error) {
	var t TexteVersion
	if err := xml.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("legi: parse texte version: %w", err)
	}
	return &t, nil
}

// ParseTexteStruct decodes a TEXTELR document.
func ParseTexteStruct(b []byte) (*TexteStruct, error) {
	var t TexteStruct
	if err := xml.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("legi: parse texte struct: %w", err)
	}
	return &t, nil
}

// ParseArticle decodes an ARTICLE document.
func ParseArticle(b []byte) (*Article, error) {
	var a Article
	if err := xml.Unmarshal(b, &a); err != nil {
		return nil, fmt.Errorf("legi: parse article: %w", err)
	}
	return &a, nil
}

// TypeSlug maps a LEGI NATURE (plus the title) to an ELI type_document slug,
// mirroring the PL approach (codes detected by title/nature).
func TypeSlug(nature, title string) string {
	if strings.Contains(strings.ToLower(title), "code") || strings.EqualFold(nature, "CODE") {
		return "code"
	}
	switch strings.ToUpper(strings.TrimSpace(nature)) {
	case "LOI":
		return "loi"
	case "ORDONNANCE":
		return "ordonnance"
	case "DECRET":
		return "decret"
	case "ARRETE":
		return "arrete"
	default:
		return asciiSlug(nature)
	}
}

// asciiSlug folds a French label to an ASCII slug as a last-resort type slug.
func asciiSlug(s string) string {
	repl := strings.NewReplacer(
		"à", "a", "â", "a", "ä", "a", "ç", "c", "é", "e", "è", "e",
		"ê", "e", "ë", "e", "î", "i", "ï", "i", "ô", "o", "ö", "o",
		"ù", "u", "û", "u", "ü", "u", " ", "-",
	)
	out := repl.Replace(strings.ToLower(strings.TrimSpace(s)))
	if out == "" {
		return "texte"
	}
	return out
}

// parseDate parses a YYYY-MM-DD date (UTC). Empty or the open-end sentinel
// yields the zero time.
func parseDate(s string) time.Time {
	if s == "" || s == openEndDate {
		return time.Time{}
	}
	if t, err := time.Parse(dateLayout, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// yearOf returns the identifying year for a text, preferring DATE_TEXTE (the
// canonical date of the text) and falling back to DATE_PUBLI then DATE_DEBUT.
func yearOf(t *TexteVersion) int {
	for _, s := range []string{t.Chronicle.DateTexte, t.Chronicle.DatePubli, t.Version.DateDebut} {
		if d := parseDate(s); !d.IsZero() {
			return d.Year()
		}
	}
	return 0
}

// statusOf resolves a text's in-force status from its ETAT field. LEGI uses
// VIGUEUR (in force) and VIGUEUR_DIFF (in force, deferred) for live texts;
// ABROGE / PERIME / ANNULE for texts no longer in force.
func statusOf(etat string) schema.Status {
	switch strings.ToUpper(strings.TrimSpace(etat)) {
	case "VIGUEUR", "VIGUEUR_DIFF":
		return schema.StatusInForce
	case "ABROGE", "ABROGE_DIFF", "PERIME", "ANNULE", "DISJOINT", "MODIFIE_MORT_NE":
		return schema.StatusRepealed
	default:
		return schema.StatusUnknown
	}
}

// versionDate is the MANDATORY as-of date. For a consolidated LEGI text the
// best "in force as of" signal is the version's DATE_DEBUT (the date this
// consolidated state took effect); we fall back to DATE_PUBLI then DATE_TEXTE.
func versionDate(t *TexteVersion) time.Time {
	for _, s := range []string{t.Version.DateDebut, t.Chronicle.DatePubli, t.Chronicle.DateTexte} {
		if d := parseDate(s); !d.IsZero() {
			return d
		}
	}
	return time.Time{}
}

// title prefers TITREFULL, falling back to TITRE.
func (t *TexteVersion) title() string {
	if s := strings.TrimSpace(t.Version.TitreFull); s != "" {
		return s
	}
	return strings.TrimSpace(t.Version.Titre)
}

// nature prefers the version's NATURE, falling back to the common one.
func (t *TexteVersion) nature() string {
	if s := strings.TrimSpace(t.Version.Nature); s != "" {
		return s
	}
	return strings.TrimSpace(t.Common.Nature)
}

// cid returns the text's stable identifier (LEGITEXT…), preferring the
// chronicle CID and falling back to the common ID.
func (t *TexteVersion) cid() string {
	if s := strings.TrimSpace(t.Chronicle.CID); s != "" {
		return s
	}
	return strings.TrimSpace(t.Common.ID)
}

// ToAct assembles a schema.Act from a TEXTE_VERSION and its parsed articles.
// retrievedAt is recorded as lex:retrievedAt. Relations are taken from the
// articles' outgoing <LIENS>: CITATION → cites, MODIFICATION → amends,
// ABROGATION → repeals, each resolved by the target's CID.
func ToAct(t *TexteVersion, articles []schema.Article, liens []Lien, retrievedAt time.Time) (*schema.Act, error) {
	cid := t.cid()
	if cid == "" {
		return nil, fmt.Errorf("legi: texte has no identifier (CID/ID)")
	}
	year := yearOf(t)
	slug := TypeSlug(t.nature(), t.title())

	exp := &schema.Expression{
		Title:            t.title(),
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      versionDate(t),
		FirstInForceDate: parseDate(t.Version.DateDebut),
		Status:           statusOf(t.Version.Etat),
		SourceURL:        "https://www.legifrance.gouv.fr/codes/texte_lc/" + cid,
		RetrievedAt:      retrievedAt,
		Articles:         articles,
	}

	// Resolve outgoing links into relation edges. A LEGI <LIEN> points at
	// another text via cidtexte; without it we cannot mint a stable work URI,
	// so such links are skipped (next-phase: resolve article-only links to
	// their parent text).
	for _, l := range liens {
		target := strings.TrimSpace(l.CIDTexte)
		if target == "" {
			continue
		}
		// We do not have the target's nature/title here, so the type slug is
		// not yet known; record the citation/amendment/repeal against the bare
		// CID work URI (Year/type unknown → next-phase resolution). For v1 we
		// only emit edges whose kind we recognise.
		uri := schema.ResourceURI(Country, "texte", 0, target)
		switch strings.ToUpper(l.TypeLien) {
		case "CITATION":
			exp.Cites = append(exp.Cites, uri)
		case "MODIFICATION", "MODIFIE":
			exp.Amends = append(exp.Amends, uri)
		case "ABROGATION", "ABROGE":
			exp.Repeals = append(exp.Repeals, uri)
		}
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   slug,
		Year:       year,
		Number:     cid,
		IDLocal:    cid,
		Expression: exp,
	}, nil
}
