// Package boe parses the Spanish BOE "Legislación Consolidada" open-data API
// (Agencia Estatal Boletín Oficial del Estado) responses into lex schema.Act
// values. It is pure and offline: HTTP fetching lives in the importer CLI,
// parsing/mapping lives here so it can be golden-tested without the network.
// See ADR-0021 and es/README.md for the API shape and the legal basis (Spanish
// normative acts are not objects of copyright; BOE open-data reuse is permitted
// with attribution to the Agencia Estatal BOE).
//
// The API exposes three per-norm resources we consume:
//   - .../id/{id}/metadatos (JSON) — identity, rango, dates, consolidation state
//   - .../id/{id}/analisis  (JSON) — referencias (amends/repeals/cites edges)
//   - .../id/{id}/texto     (XML)  — the consolidated text as <bloque> elements
package boe

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tggo/lex/internal/schema"
)

// Country is the ISO code for Spain.
const Country schema.CountryCode = "es"

// ELI language metadata for (Castilian) Spanish.
const (
	langTag    = "es"
	langAlpha3 = "SPA"
)

// codeText is the BOE pattern of a coded enumeration carrying display text,
// e.g. <rango codigo="1300">Ley</rango> or {"codigo":"3","texto":"Finalizado"}.
type codeText struct {
	Codigo string `json:"codigo"`
	Texto  string `json:"texto"`
}

// Metadatos is one record from GET …/id/{id}/metadatos (data[0]).
type Metadatos struct {
	FechaActualizacion string   `json:"fecha_actualizacion"` // e.g. "20260527T082330Z"
	Identificador      string   `json:"identificador"`       // e.g. "BOE-A-2021-6945"
	Rango              codeText `json:"rango"`               // e.g. "Ley"
	FechaDisposicion   string   `json:"fecha_disposicion"`   // YYYYMMDD
	NumeroOficial      string   `json:"numero_oficial"`      // e.g. "6/2021"
	Titulo             string   `json:"titulo"`
	FechaPublicacion   string   `json:"fecha_publicacion"` // YYYYMMDD
	FechaVigencia      string   `json:"fecha_vigencia"`    // YYYYMMDD — entry into force
	EstatusDerogacion  string   `json:"estatus_derogacion"`
	VigenciaAgotada    string   `json:"vigencia_agotada"`
	EstadoConsol       codeText `json:"estado_consolidacion"`
	URLEli             string   `json:"url_eli"`
	URLHTMLConsolidada string   `json:"url_html_consolidada"`
}

// metadatosEnvelope is the {status,data:[…]} wrapper around metadatos records.
type metadatosEnvelope struct {
	Data []Metadatos `json:"data"`
}

// ListItem mirrors a metadatos record; the list endpoint returns the same shape.
type ListItem = Metadatos

// listEnvelope is the {status,data:[…]} wrapper of the listing endpoint.
type listEnvelope struct {
	Data []ListItem `json:"data"`
}

// refEntry is one referenced norm inside analisis.referencias.
type refEntry struct {
	IDNorma  string   `json:"id_norma"`
	Relacion codeText `json:"relacion"`
	Texto    string   `json:"texto"`
}

// Analisis is data[0] of GET …/id/{id}/analisis. The referencias structure is
// irregular in the source JSON (single object vs array), so we decode it
// loosely via json.RawMessage and normalise in parseRefList.
type Analisis struct {
	Referencias struct {
		Anteriores  json.RawMessage `json:"anteriores"`
		Posteriores json.RawMessage `json:"posteriores"`
	} `json:"referencias"`
}

type analisisEnvelope struct {
	Data []Analisis `json:"data"`
}

// ParseList decodes a listing page into its items.
func ParseList(b []byte) ([]ListItem, error) {
	var e listEnvelope
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("boe: parse list: %w", err)
	}
	return e.Data, nil
}

// ParseMetadatos decodes a per-norm metadatos document (first record).
func ParseMetadatos(b []byte) (*Metadatos, error) {
	var e metadatosEnvelope
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("boe: parse metadatos: %w", err)
	}
	if len(e.Data) == 0 {
		return nil, fmt.Errorf("boe: metadatos has no records")
	}
	return &e.Data[0], nil
}

// ParseAnalisis decodes a per-norm analisis document (first record). A missing
// or empty analisis yields a zero-value Analisis and no error.
func ParseAnalisis(b []byte) (*Analisis, error) {
	var e analisisEnvelope
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("boe: parse analisis: %w", err)
	}
	if len(e.Data) == 0 {
		return &Analisis{}, nil
	}
	return &e.Data[0], nil
}

// parseRefList normalises a referencias group (anteriores/posteriores) into a
// flat slice of refEntry. The BOE JSON nests entries one extra level under a
// per-direction key ("anterior"/"posterior") whose value is sometimes a single
// object and sometimes an array; we accept both.
func parseRefList(raw json.RawMessage) []refEntry {
	if len(raw) == 0 {
		return nil
	}
	// Shape A: object {"anterior":[…]} / {"posterior":{…}}.
	var asObj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObj); err == nil {
		var out []refEntry
		keys := make([]string, 0, len(asObj))
		for k := range asObj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out = append(out, decodeEntries(asObj[k])...)
		}
		return out
	}
	// Shape B: a bare array of grouping objects [{"anterior":[…]}, …].
	var asArr []json.RawMessage
	if err := json.Unmarshal(raw, &asArr); err == nil {
		var out []refEntry
		for _, el := range asArr {
			out = append(out, parseRefList(el)...)
		}
		return out
	}
	return nil
}

// decodeEntries reads either a single refEntry object or an array of them.
func decodeEntries(raw json.RawMessage) []refEntry {
	var one refEntry
	if err := json.Unmarshal(raw, &one); err == nil && one.IDNorma != "" {
		return []refEntry{one}
	}
	var many []refEntry
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	return nil
}

// TypeSlug maps a BOE "rango" label to an ELI type_document slug. Codes are
// identified by the word "código" in the title (mirroring the PL/UA approach).
func TypeSlug(rango, title string) string {
	if strings.Contains(strings.ToLower(title), "código") ||
		strings.Contains(strings.ToLower(title), "codigo") {
		return "codigo"
	}
	switch r := strings.ToLower(strings.TrimSpace(rango)); r {
	case "ley":
		return "ley"
	case "ley orgánica":
		return "ley-organica"
	case "real decreto":
		return "real-decreto"
	case "real decreto-ley":
		return "real-decreto-ley"
	case "real decreto legislativo":
		return "real-decreto-legislativo"
	case "decreto":
		return "decreto"
	case "orden":
		return "orden"
	case "resolución":
		return "resolucion"
	case "constitución":
		return "constitucion"
	default:
		return asciiSlug(r)
	}
}

// asciiSlug folds a Spanish label to an ASCII slug as a last-resort type slug.
func asciiSlug(s string) string {
	repl := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u",
		"ñ", "n", "ü", "u", " ", "-",
	)
	out := repl.Replace(strings.ToLower(strings.TrimSpace(s)))
	if out == "" {
		return "norma"
	}
	return out
}

// idYear extracts the year from a BOE identifier like "BOE-A-2021-6945".
func idYear(id string) (int, error) {
	parts := strings.Split(id, "-")
	if len(parts) < 4 {
		return 0, fmt.Errorf("boe: malformed identifier %q", id)
	}
	y, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, fmt.Errorf("boe: identifier %q has bad year: %w", id, err)
	}
	return y, nil
}

// resourceURIFor builds the lex work URI for a referenced norm. The work Number
// is the full BOE identifier; the type is unknown for bare references, so we
// use the generic "norma" slug — relations resolve to a stable, reconstructible
// URI even without re-fetching the target's rango.
func resourceURIFor(id string) (string, error) {
	year, err := idYear(id)
	if err != nil {
		return "", err
	}
	return schema.ResourceURI(Country, "norma", year, id), nil
}

// statusOf resolves an act's in-force status from the consolidation flags.
func statusOf(m *Metadatos) schema.Status {
	if strings.EqualFold(m.EstatusDerogacion, "S") || strings.EqualFold(m.VigenciaAgotada, "S") {
		return schema.StatusRepealed
	}
	if strings.EqualFold(m.EstatusDerogacion, "N") {
		return schema.StatusInForce
	}
	return schema.StatusUnknown
}

// parseDate parses a BOE YYYYMMDD date (UTC). Empty input yields the zero time.
func parseDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse("20060102", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// versionDate is the MANDATORY as-of date. BOE consolidated norms carry
// fecha_actualizacion — the timestamp of the last consolidation update — which
// is the cleanest "consolidated as of" signal. It falls back to the disposition
// date when absent.
func versionDate(m *Metadatos) time.Time {
	if m.FechaActualizacion != "" {
		if t, err := time.Parse("20060102T150405Z", m.FechaActualizacion); err == nil {
			return t.UTC()
		}
		if len(m.FechaActualizacion) >= 8 {
			if t := parseDate(m.FechaActualizacion[:8]); !t.IsZero() {
				return t
			}
		}
	}
	return parseDate(m.FechaDisposicion)
}

// relationFor maps a BOE relation label (analisis referencias relacion.texto)
// to a schema relation kind. dir is "anteriores" (this norm acts on a prior
// one) or "posteriores" (a later norm acts on this one). The second result is
// false for relations lex does not model.
func relationFor(label, dir string) (kind string, ok bool) {
	l := strings.ToUpper(strings.TrimSpace(label))
	switch {
	case strings.Contains(l, "DEROGA"):
		if dir == "posteriores" {
			return "repealed_by", true
		}
		return "repeals", true
	case strings.Contains(l, "MODIFICA"):
		if dir == "posteriores" {
			return "amended_by", true
		}
		return "amends", true
	default:
		// Everything else (AMPLÍA, CITA, DESARROLLA, …) is a citation edge.
		if dir == "posteriores" {
			return "", false // inbound non-amend/repeal edges are not modelled
		}
		return "cites", true
	}
}

// ToAct assembles a schema.Act from a norm's metadatos, analisis, and parsed
// articles. retrievedAt is recorded as lex:retrievedAt.
func ToAct(m *Metadatos, an *Analisis, articles []schema.Article, retrievedAt time.Time) (*schema.Act, error) {
	year, err := idYear(m.Identificador)
	if err != nil {
		return nil, err
	}
	slug := TypeSlug(m.Rango.Texto, m.Titulo)

	src := m.URLEli
	if src == "" {
		src = m.URLHTMLConsolidada
	}

	exp := &schema.Expression{
		Title:            m.Titulo,
		LangTag:          langTag,
		LangAlpha3:       langAlpha3,
		VersionDate:      versionDate(m),
		FirstInForceDate: parseDate(m.FechaVigencia),
		Status:           statusOf(m),
		SourceURL:        src,
		RetrievedAt:      retrievedAt,
		Articles:         articles,
	}
	if exp.Status == schema.StatusRepealed {
		exp.NoLongerInForce = versionDate(m)
	}

	if an != nil {
		addEdges(exp, parseRefList(an.Referencias.Anteriores), "anteriores")
		addEdges(exp, parseRefList(an.Referencias.Posteriores), "posteriores")
	}

	return &schema.Act{
		Country:    Country,
		TypeSlug:   slug,
		Year:       year,
		Number:     m.Identificador,
		IDLocal:    m.Identificador,
		Expression: exp,
	}, nil
}

// addEdges resolves a list of references into relation edges on exp.
func addEdges(exp *schema.Expression, refs []refEntry, dir string) {
	for _, e := range refs {
		if e.IDNorma == "" {
			continue
		}
		kind, ok := relationFor(e.Relacion.Texto, dir)
		if !ok {
			continue
		}
		uri, err := resourceURIFor(e.IDNorma)
		if err != nil {
			continue
		}
		switch kind {
		case "amends":
			exp.Amends = append(exp.Amends, uri)
		case "amended_by":
			exp.AmendedBy = append(exp.AmendedBy, uri)
		case "repeals":
			exp.Repeals = append(exp.Repeals, uri)
		case "repealed_by":
			exp.RepealedBy = append(exp.RepealedBy, uri)
		case "cites":
			exp.Cites = append(exp.Cites, uri)
		}
	}
}

// xmlBloques is the document shape of GET …/id/{id}/texto.
type xmlBloques struct {
	XMLName xml.Name `xml:"response"`
	Data    struct {
		Texto struct {
			Bloques []xmlBloque `xml:"bloque"`
		} `xml:"texto"`
	} `xml:"data"`
}

type xmlBloque struct {
	ID       string       `xml:"id,attr"`
	Tipo     string       `xml:"tipo,attr"`
	Titulo   string       `xml:"titulo,attr"`
	Versions []xmlVersion `xml:"version"`
}

type xmlVersion struct {
	Ps []xmlPara `xml:"p"`
}

type xmlPara struct {
	Text string `xml:",chardata"`
	// inner inline markup (e.g. <em>) contributes its chardata too.
	Inner []innerText `xml:",any"`
}

type innerText struct {
	Text string `xml:",chardata"`
}
