// Package release fetches prebuilt lex datasets from GitHub Releases so users
// run a server over ready data instead of re-scraping official sources. A
// dataset is published as a gzip-compressed tarball of the dataset root
// (graph/ + index.fts); see ADR-0006/0010.
package release

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// DatasetTag is the fixed release tag that hosts prebuilt datasets, so the
// download URL stays stable across code releases.
const DatasetTag = "datasets"

// AssetURL builds the download URL for a country's prebuilt dataset.
func AssetURL(repo, country string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/lex-%s.tar.gz", repo, DatasetTag, country)
}

// Download fetches a .tar.gz dataset from url and extracts it into destRoot.
func Download(ctx context.Context, client *http.Client, url, destRoot string) error {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("release: download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("release: download %s: status %d", url, resp.StatusCode)
	}
	return ExtractTarGz(resp.Body, destRoot)
}

// ExtractTarGz extracts a gzip-compressed tar stream into dest, guarding against
// path traversal (zip-slip).
func ExtractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("release: gzip: %w", err)
	}
	defer gz.Close()

	cleanDest := filepath.Clean(dest)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("release: tar: %w", err)
		}
		target := filepath.Join(cleanDest, hdr.Name)
		if target != cleanDest && !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("release: unsafe path in archive: %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil { //nolint:gosec // size bounded by trusted release
				f.Close()
				return err
			}
			f.Close()
		}
	}
}
