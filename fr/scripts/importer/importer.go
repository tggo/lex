// Package importer loads French legislation from the DILA LEGI open-data
// dataset into a lex Badger triplestore. Network access (tarball download) and
// the tar/gzip walk live here; parsing/mapping is in package legi and is tested
// offline. French legislative texts are not objects of copyright; LEGI is
// published under the Licence Ouverte / Open Licence (Etalab), attribution to
// DILA. See ADR-0016.
//
// LEGI is NOT served as per-text URLs: DILA publishes the corpus only as bulk
// gzip tarballs at https://echanges.dila.gouv.fr/OPENDATA/LEGI/ — one large
// global tarball (`Freemium_legi_global_*.tar.gz`, ~1.1 GB, ~twice a year) and
// small daily delta tarballs (`LEGI_YYYYMMDD-*.tar.gz`, hundreds of KB). This
// importer downloads a tarball, stream-walks it with archive/tar + compress/gzip,
// indexes the per-object XML files by the text CID (LEGITEXT…) they belong to,
// and feeds the bytes to the pure legi.Parse* functions.
//
// Inside a dump the files are laid out under a JORF text directory, e.g.
//
//	.../JORF/TEXT/<shard>/JORFTEXTnnn/texte/version/LEGITEXTnnn.xml  (TEXTE_VERSION)
//	.../JORF/TEXT/<shard>/JORFTEXTnnn/texte/struct/LEGITEXTnnn.xml   (TEXTELR)
//	.../JORF/TEXT/<shard>/JORFTEXTnnn/article/LEGI/ARTI/<shard>/LEGIARTInnn.xml (ARTICLE)
//
// so a text's articles are co-located under that text's JORFTEXT… subtree.
package importer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/tggo/lex/fr/scripts/legi"
	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/search"
	"github.com/tggo/lex/internal/store"
)

// Defaults for the live DILA LEGI open data.
const (
	// DefaultBase is the directory under which DILA publishes the LEGI
	// tarballs (global + daily deltas).
	DefaultBase = "https://echanges.dila.gouv.fr/OPENDATA/LEGI"
	DefaultUA   = "lex/0.1 (+https://github.com/tggo/lex)"
	maxRetries  = 4
)

// Config controls an import run.
type Config struct {
	OutDir    string // Badger store directory
	IndexPath string // FTS index file; if empty, no index is built
	Lang      string // search stemming language
	UA        string // HTTP User-Agent

	// Source of the tarball. Exactly one of DumpURL / DumpPath is used;
	// DumpPath (a local .tar.gz) takes precedence and skips the network.
	DumpURL  string // absolute URL of a .tar.gz to download
	DumpPath string // local path to an already-downloaded .tar.gz

	Client *http.Client // defaults to http.DefaultClient if nil
	Now    time.Time    // retrieval timestamp recorded on each act

	// TextCIDs filters the import to these LEGITEXT… ids. If empty, every
	// text found in the tarball is imported.
	TextCIDs []string

	// WithArticles also parses each text's co-located ARTICLE files.
	WithArticles bool
}

// Run downloads/opens the configured tarball, walks it, parses the matching
// texts and writes acts to the store. It returns the number of acts written.
func Run(ctx context.Context, cfg Config) (int, error) {
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now().UTC()
	}

	// Obtain a readable, seekable-enough source for the tar walk. We stream
	// the gzip directly; for a URL we download to a temp file first so a
	// transient connection drop mid-walk does not corrupt a partial import.
	rc, cleanup, err := cfg.openDump(ctx)
	if err != nil {
		return 0, err
	}
	defer cleanup()

	// Build the CID→entry index by walking the tarball once.
	idx, err := indexTar(rc)
	if err != nil {
		return 0, fmt.Errorf("walk tarball: %w", err)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	var idxFTS *search.Index
	if cfg.IndexPath != "" {
		idxFTS, err = search.OpenLang(cfg.IndexPath, cfg.Lang)
		if err != nil {
			return 0, err
		}
		defer idxFTS.Close()
	}

	cids := cfg.TextCIDs
	if len(cids) == 0 {
		cids = idx.cids()
	}

	total := 0
	for _, cid := range cids {
		te, ok := idx.byCID[cid]
		if !ok {
			return total, fmt.Errorf("import %s: text not found in tarball", cid)
		}
		act, err := buildAct(te, cfg.WithArticles, cfg.Now)
		if err != nil {
			return total, fmt.Errorf("import %s: %w", cid, err)
		}
		if err := st.AddAct(act); err != nil {
			return total, fmt.Errorf("import %s: %w", cid, err)
		}
		if idxFTS != nil {
			if err := idxFTS.ReplaceAct(act); err != nil {
				return total, fmt.Errorf("index %s: %w", cid, err)
			}
		}
		total++
	}
	return total, nil
}

// buildAct parses one text entry's version (+ struct + articles) into an Act.
func buildAct(te *textEntry, withArticles bool, now time.Time) (*schema.Act, error) {
	if te.version == nil {
		return nil, fmt.Errorf("text %s has no TEXTE_VERSION file in tarball", te.cid)
	}
	tv, err := legi.ParseTexteVersion(te.version)
	if err != nil {
		return nil, err
	}

	var arts []schema.Article
	var liens []legi.Lien
	if withArticles && te.struct_ != nil {
		tstruct, err := legi.ParseTexteStruct(te.struct_)
		if err != nil {
			return nil, err
		}
		byID := map[string]*legi.Article{}
		for id, raw := range te.articles {
			a, err := legi.ParseArticle(raw)
			if err != nil {
				return nil, fmt.Errorf("article %s: %w", id, err)
			}
			byID[a.Common.ID] = a
		}
		arts, liens = legi.BuildArticles(tstruct, byID)
	}

	return legi.ToAct(tv, arts, liens, now)
}

// openDump returns a reader over the configured tarball plus a cleanup func.
func (cfg Config) openDump(ctx context.Context) (io.Reader, func(), error) {
	if cfg.DumpPath != "" {
		f, err := os.Open(cfg.DumpPath)
		if err != nil {
			return nil, func() {}, err
		}
		return f, func() { f.Close() }, nil
	}
	if cfg.DumpURL == "" {
		return nil, func() {}, fmt.Errorf("no dump source: set DumpURL or DumpPath")
	}
	tmp, err := download(ctx, cfg.Client, cfg.UA, cfg.DumpURL)
	if err != nil {
		return nil, func() {}, err
	}
	f, err := os.Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return nil, func() {}, err
	}
	return f, func() { f.Close(); os.Remove(tmp) }, nil
}

// download fetches url to a temp file with the configured UA, retried on
// transient errors (429 / 5xx). It returns the temp file path.
func download(ctx context.Context, client *http.Client, ua, url string) (string, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", ua)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("download %s: %w", url, err)
			if !sleepBackoff(ctx, attempt) {
				return "", lastErr
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("download %s: status %d", url, resp.StatusCode)
			if !sleepBackoff(ctx, attempt) {
				return "", lastErr
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return "", fmt.Errorf("download %s: status %d", url, resp.StatusCode)
		}
		f, err := os.CreateTemp("", "legi-dump-*.tar.gz")
		if err != nil {
			resp.Body.Close()
			return "", err
		}
		_, err = io.Copy(f, resp.Body)
		resp.Body.Close()
		f.Close()
		if err != nil {
			os.Remove(f.Name())
			return "", fmt.Errorf("download %s: %w", url, err)
		}
		return f.Name(), nil
	}
	return "", lastErr
}

// sleepBackoff waits an exponential interval before the next retry. It returns
// false if the context is cancelled.
func sleepBackoff(ctx context.Context, attempt int) bool {
	d := time.Duration(1<<attempt) * 200 * time.Millisecond
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
