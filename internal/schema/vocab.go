// Package schema is the code form of the lex ontology contract documented in
// docs/ontology.md. It defines the RDF vocabulary (namespaces, classes,
// predicates) and the deterministic URI scheme that every country importer
// must follow. It deliberately has no dependency on any RDF engine: it is the
// shared language spoken by importers (which emit) and the store (which reads).
package schema

import "strings"

// Namespaces used across the graph. See docs/ontology.md.
const (
	NSeli  = "http://data.europa.eu/eli/ontology#"  // European Legislation Identifier
	NSdct  = "http://purl.org/dc/terms/"            // Dublin Core terms
	NSskos = "http://www.w3.org/2004/02/skos/core#" // SKOS labels
	NSxsd  = "http://www.w3.org/2001/XMLSchema#"    // XSD datatypes
	NSlex  = "https://lex.dev/ontology#"            // lex extensions (Article, provenance)

	// NSid is the base for minted instance URIs (acts, expressions, articles).
	NSid = "https://lex.dev/eli/"
)

// Classes.
const (
	ClassLegalResource   = NSeli + "LegalResource"   // an act as an abstract work (FRBR)
	ClassLegalExpression = NSeli + "LegalExpression" // a consolidated version at a date
	ClassArticle         = NSlex + "Article"         // an individual article (lex extension)
)

// Predicates.
const (
	// FRBR realization between Resource and Expression.
	PredIsRealizedBy = NSeli + "is_realized_by"
	PredRealizes     = NSeli + "realizes"

	// Identity & typing.
	PredTypeDocument = NSeli + "type_document"
	PredIdLocal      = NSeli + "id_local"

	// Bibliographic.
	PredTitle    = NSdct + "title"
	PredLanguage = NSeli + "language"

	// Temporal / status (the versioning backbone).
	PredVersionDate           = NSeli + "version_date"
	PredFirstDateEntryInForce = NSeli + "first_date_entry_in_force"
	PredInForce               = NSeli + "in_force"
	PredDateNoLongerInForce   = NSeli + "date_no_longer_in_force"

	// Relationships (graph edges, queried via SPARQL).
	PredAmends       = NSeli + "amends"
	PredAmendedBy    = NSeli + "amended_by"
	PredRepeals      = NSeli + "repeals"
	PredRepealedBy   = NSeli + "repealed_by"
	PredCites        = NSeli + "cites"
	PredConsolidates = NSeli + "consolidates"

	// lex extensions.
	PredSourceURL   = NSlex + "sourceURL"
	PredRetrievedAt = NSlex + "retrievedAt"
	PredHasArticle  = NSlex + "hasArticle"
	PredArticleText = NSlex + "text"
	PredArticleNum  = NSlex + "number"

	PredPrefLabel = NSskos + "prefLabel"
)

// ELI in-force status individuals.
const (
	InForceInForce    = NSeli + "InForce-inForce"
	InForceNotInForce = NSeli + "InForce-notInForce"
)

// ELI language authority URI for a 3-letter (alpha-3, upper) code.
// e.g. LanguageURI("UKR") for Ukrainian.
func LanguageURI(alpha3Upper string) string {
	return "http://publications.europa.eu/resource/authority/language/" + strings.ToUpper(alpha3Upper)
}
