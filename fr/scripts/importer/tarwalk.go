package importer

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
)

// textEntry holds the raw XML bytes for one LEGI text, collected from a single
// pass over a tarball: its TEXTE_VERSION, its TEXTELR struct, and the ARTICLE
// files co-located under its JORFTEXT… subtree (keyed by LEGIARTI… id).
type textEntry struct {
	cid      string
	version  []byte            // texte/version/LEGITEXT….xml
	struct_  []byte            // texte/struct/LEGITEXT….xml
	articles map[string][]byte // LEGIARTI… → ARTICLE xml
}

// tarIndex maps a text CID to its collected files.
type tarIndex struct {
	byCID map[string]*textEntry
}

// cids returns the indexed text CIDs in sorted order (stable iteration).
func (ti *tarIndex) cids() []string {
	out := make([]string, 0, len(ti.byCID))
	for c := range ti.byCID {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (ti *tarIndex) entry(jorf string) *textEntry {
	// Articles live under JORFTEXT… but are keyed to the text by the parent
	// JORFTEXT directory; we index everything by the JORFTEXT id so version,
	// struct and articles for the same text land in the same entry.
	te := ti.byCID[jorf]
	if te == nil {
		te = &textEntry{articles: map[string][]byte{}}
		ti.byCID[jorf] = te
	}
	return te
}

var (
	// A LEGITEXT… file living under texte/version or texte/struct.
	reVersion = regexp.MustCompile(`(?:^|/)texte/version/(LEGITEXT\d+)\.xml$`)
	reStruct  = regexp.MustCompile(`(?:^|/)texte/struct/(LEGITEXT\d+)\.xml$`)
	// An ARTICLE file under …/article/LEGI/ARTI/<shard>/LEGIARTI….xml.
	reArticle = regexp.MustCompile(`(?:^|/)article/.*?(LEGIARTI\d+)\.xml$`)
	// The JORFTEXT… directory segment that groups one text's files.
	reJORF = regexp.MustCompile(`(JORFTEXT\d+)`)
)

// indexTar walks a gzip-compressed tar stream once and builds the CID→entry
// index. Files are grouped by the JORFTEXT… directory segment in their path
// (the real DILA layout), and the index is then rekeyed to the LEGITEXT… CID
// found in each text's version/struct filename.
func indexTar(r io.Reader) (*tarIndex, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	// First group everything by the JORFTEXT directory, since articles only
	// carry the JORFTEXT in their path, not the LEGITEXT CID.
	byJORF := &tarIndex{byCID: map[string]*textEntry{}}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		name := path.Clean(hdr.Name)
		if !strings.HasSuffix(name, ".xml") {
			continue
		}

		jm := reJORF.FindStringSubmatch(name)
		if jm == nil {
			continue // not part of a JORF text subtree
		}
		jorf := jm[1]

		switch {
		case reVersion.MatchString(name):
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			te := byJORF.entry(jorf)
			te.version = b
			te.cid = reVersion.FindStringSubmatch(name)[1]
		case reStruct.MatchString(name):
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			te := byJORF.entry(jorf)
			te.struct_ = b
			if te.cid == "" {
				te.cid = reStruct.FindStringSubmatch(name)[1]
			}
		case reArticle.MatchString(name):
			b, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			te := byJORF.entry(jorf)
			id := reArticle.FindStringSubmatch(name)[1]
			te.articles[id] = b
		}
	}

	// Rekey by LEGITEXT CID. Texts whose version/struct revealed a CID are
	// keyed by it; entries that only had articles (no version) are dropped,
	// they cannot be turned into an act.
	out := &tarIndex{byCID: map[string]*textEntry{}}
	for _, te := range byJORF.byCID {
		if te.cid == "" {
			continue
		}
		out.byCID[te.cid] = te
	}
	return out, nil
}
