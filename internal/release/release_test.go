package release

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractTarGz(t *testing.T) {
	dest := t.TempDir()
	arc := makeTarGz(t, map[string]string{
		"graph/MANIFEST": "badger",
		"index.fts":      "sqlite",
	})
	if err := ExtractTarGz(bytes.NewReader(arc), dest); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "index.fts")); err != nil || string(b) != "sqlite" {
		t.Errorf("index.fts = %q, err %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "graph", "MANIFEST")); err != nil || string(b) != "badger" {
		t.Errorf("graph/MANIFEST = %q, err %v", b, err)
	}
}

func TestExtractTarGz_blocksZipSlip(t *testing.T) {
	dest := t.TempDir()
	arc := makeTarGz(t, map[string]string{"../escape.txt": "evil"})
	if err := ExtractTarGz(bytes.NewReader(arc), dest); err == nil {
		t.Fatal("expected zip-slip path to be rejected")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escape.txt")); err == nil {
		t.Error("escape file was written outside dest")
	}
}

func TestDownload(t *testing.T) {
	arc := makeTarGz(t, map[string]string{"graph/MANIFEST": "badger", "index.fts": "sqlite"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lex-ua.tar.gz" {
			w.Write(arc)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dest := t.TempDir()
	if err := Download(context.Background(), srv.Client(), srv.URL+"/lex-ua.tar.gz", dest); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "index.fts")); string(b) != "sqlite" {
		t.Errorf("extracted index.fts = %q", b)
	}
	// Missing asset → error.
	if err := Download(context.Background(), srv.Client(), srv.URL+"/missing.tar.gz", t.TempDir()); err == nil {
		t.Error("expected error for 404 asset")
	}
}

func TestExtractTarGz_dirEntry(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "graph", Mode: 0o755, Typeflag: tar.TypeDir})
	body := "x"
	tw.WriteHeader(&tar.Header{Name: "graph/000001.vlog", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()

	dest := t.TempDir()
	if err := ExtractTarGz(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dest, "graph")); err != nil || !fi.IsDir() {
		t.Errorf("graph dir not created: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "graph", "000001.vlog")); string(b) != "x" {
		t.Errorf("nested file = %q", b)
	}
}

func TestExtractTarGz_badGzip(t *testing.T) {
	if err := ExtractTarGz(bytes.NewReader([]byte("not a gzip stream")), t.TempDir()); err == nil {
		t.Error("expected error on non-gzip input")
	}
}

func TestDownload_unreachable(t *testing.T) {
	// Port 0 is invalid to dial → client.Do returns an error.
	if err := Download(context.Background(), nil, "http://127.0.0.1:0/x.tar.gz", t.TempDir()); err == nil {
		t.Error("expected error for unreachable host")
	}
}

func TestAssetURL(t *testing.T) {
	got := AssetURL("tggo/lex", "ua")
	want := "https://github.com/tggo/lex/releases/download/datasets/lex-ua.tar.gz"
	if got != want {
		t.Errorf("AssetURL = %q, want %q", got, want)
	}
}
