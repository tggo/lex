package importer

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
	"time"

	"github.com/tggo/lex/internal/schema"
	"github.com/tggo/lex/internal/store"
)

const sampleCID = "LEGITEXT000006070721"

// fixtureTarball is the committed tiny .tar.gz: the legi package's real-shaped
// XML fixtures arranged in the real DILA directory layout (a JORFTEXT… subtree
// with texte/version, texte/struct and article/LEGI/ARTI files).
const fixtureTarball = "testdata/legi_delta_sample.tar.gz"

func baseCfg(t *testing.T) Config {
	t.Helper()
	return Config{
		DumpPath: fixtureTarball,
		OutDir:   filepath.Join(t.TempDir(), "graph"),
		UA:       "lex-test",
		Now:      time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		TextCIDs: []string{sampleCID},
	}
}

func TestRun_endToEnd(t *testing.T) {
	cfg := baseCfg(t)
	cfg.WithArticles = true

	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d acts, want 1", n)
	}

	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a, err := st.GetAct(schema.ResourceURI("fr", "code", 1804, sampleCID))
	if err != nil {
		t.Fatalf("GetAct: %v", err)
	}
	if a.IDLocal != sampleCID {
		t.Errorf("idLocal = %q", a.IDLocal)
	}
	if a.Expression.Status != schema.StatusInForce {
		t.Errorf("status = %v, want InForce", a.Expression.Status)
	}
	if len(a.Expression.Articles) != 2 {
		t.Errorf("articles = %d, want 2", len(a.Expression.Articles))
	}
	if a.Expression.Articles[0].Number != "1" {
		t.Errorf("article[0].Number = %q, want 1 (struct order)", a.Expression.Articles[0].Number)
	}
}

func TestRun_withoutArticles(t *testing.T) {
	cfg := baseCfg(t) // WithArticles defaults false
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}
	st, err := store.Open(cfg.OutDir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := st.GetAct(schema.ResourceURI("fr", "code", 1804, sampleCID))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Expression.Articles) != 0 {
		t.Errorf("articles = %d, want 0 (articles disabled)", len(a.Expression.Articles))
	}
}

// TestRun_allTexts imports every text found in the tarball (no -cids filter).
func TestRun_allTexts(t *testing.T) {
	cfg := baseCfg(t)
	cfg.TextCIDs = nil
	cfg.WithArticles = true
	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d acts, want 1 (all texts in fixture)", n)
	}
}

func TestRun_defaultsClientAndNow(t *testing.T) {
	cfg := baseCfg(t)
	cfg.Client = nil
	cfg.Now = time.Time{}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run with defaults: %v", err)
	}
}

func TestRun_cidNotInTarball(t *testing.T) {
	cfg := baseCfg(t)
	cfg.TextCIDs = []string{"LEGITEXT999999999999"}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error for CID absent from tarball")
	}
}

func TestRun_noSource(t *testing.T) {
	cfg := baseCfg(t)
	cfg.DumpPath = ""
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when no dump source configured")
	}
}

func TestRun_missingDumpFile(t *testing.T) {
	cfg := baseCfg(t)
	cfg.DumpPath = filepath.Join(t.TempDir(), "nope.tar.gz")
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error when dump file missing")
	}
}

func TestRun_notGzip(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.tar.gz")
	if err := os.WriteFile(bad, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := baseCfg(t)
	cfg.DumpPath = bad
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error for non-gzip dump")
	}
}

// TestRun_downloadFromURL drives the HTTP download path by serving the
// committed fixture tarball over httptest.
func TestRun_downloadFromURL(t *testing.T) {
	body, err := os.ReadFile(fixtureTarball)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg := baseCfg(t)
	cfg.DumpPath = ""
	cfg.DumpURL = srv.URL + "/LEGI_20240115-000000.tar.gz"
	cfg.Client = srv.Client()
	cfg.WithArticles = true

	n, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run from URL: %v", err)
	}
	if n != 1 {
		t.Fatalf("imported %d acts, want 1", n)
	}
}

func TestRun_downloadNotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	cfg := baseCfg(t)
	cfg.DumpPath = ""
	cfg.DumpURL = srv.URL + "/missing.tar.gz"
	cfg.Client = srv.Client()
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error for 404 download")
	}
}

// TestIndexTar_orphanArticles verifies that a JORFTEXT subtree with only
// article files (no version) is dropped from the index.
func TestIndexTar_orphanArticles(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	write := func(name, content string) {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	write("x/JORFTEXT000000000002/article/LEGI/ARTI/00/LEGIARTI000000000009.xml", "<ARTICLE/>")
	write("README.txt", "ignored")                  // non-xml ignored
	write("loose/LEGITEXT000000000003.xml", "<x/>") // no JORFTEXT segment → ignored
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	idx, err := indexTar(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.byCID) != 0 {
		t.Errorf("index has %d entries, want 0 (orphan articles dropped)", len(idx.byCID))
	}
}

// makeTar builds an in-memory gzip tarball from name→content pairs.
func makeTar(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "t.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestRun_structOnlyNoVersion: a JORFTEXT with only a struct file has its CID
// keyed from the struct, but buildAct must error (no TEXTE_VERSION).
func TestRun_structOnlyNoVersion(t *testing.T) {
	base := "x/JORFTEXT000000000005"
	tar := makeTar(t, map[string]string{
		base + "/texte/struct/LEGITEXT000000000005.xml": "<TEXTELR/>",
	})
	cfg := baseCfg(t)
	cfg.DumpPath = tar
	cfg.TextCIDs = []string{"LEGITEXT000000000005"}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected error: struct present but no version")
	}
}

func TestRun_malformedVersion(t *testing.T) {
	base := "x/JORFTEXT000000000006"
	tar := makeTar(t, map[string]string{
		base + "/texte/version/LEGITEXT000000000006.xml": "<TEXTE_VERSION><META",
	})
	cfg := baseCfg(t)
	cfg.DumpPath = tar
	cfg.TextCIDs = []string{"LEGITEXT000000000006"}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Error("expected parse error for malformed version XML")
	}
}

// TestDownload_retryThenSuccess exercises the 503-retry path in download.
func TestDownload_retryThenSuccess(t *testing.T) {
	body, err := os.ReadFile(fixtureTarball)
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg := baseCfg(t)
	cfg.DumpPath = ""
	cfg.DumpURL = srv.URL + "/d.tar.gz"
	cfg.Client = srv.Client()
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run after retry: %v", err)
	}
	if calls < 2 {
		t.Errorf("calls = %d, want a retry", calls)
	}
}

func TestDownload_contextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // always transient → forces backoff
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	cfg := baseCfg(t)
	cfg.DumpPath = ""
	cfg.DumpURL = srv.URL + "/d.tar.gz"
	cfg.Client = srv.Client()
	if _, err := Run(ctx, cfg); err == nil {
		t.Error("expected error when context cancelled during backoff")
	}
}
