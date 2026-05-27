//go:build ignore

// Command gen rebuilds legi_delta_sample.tar.gz from the legi package's
// real-shaped XML fixtures, arranged in the real DILA tarball layout (a
// JORFTEXT… subtree with texte/version, texte/struct and article/LEGI/ARTI
// files). Run from this directory:
//
//	go run gen.go
package main

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
)

func main() {
	fx := filepath.Join("..", "..", "legi", "testdata")
	out := "legi_delta_sample.tar.gz"

	base := "legi/global/code_et_TNC_en_vigueur/code_en_vigueur/LEGI/TEXT/00/00/06/07/07/JORFTEXT000000000001"
	files := []struct{ dst, src string }{
		{base + "/texte/version/LEGITEXT000006070721.xml", "texte_version.sample.xml"},
		{base + "/texte/struct/LEGITEXT000006070721.xml", "texte_struct.sample.xml"},
		{base + "/article/LEGI/ARTI/00/00/06/41/92/LEGIARTI000006419280.xml", "article_1.sample.xml"},
		{base + "/article/LEGI/ARTI/00/00/06/41/92/LEGIARTI000006419281.xml", "article_2.sample.xml"},
	}

	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, fl := range files {
		b, err := os.ReadFile(filepath.Join(fx, fl.src))
		if err != nil {
			panic(err)
		}
		if err := tw.WriteHeader(&tar.Header{Name: fl.dst, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			panic(err)
		}
		if _, err := tw.Write(b); err != nil {
			panic(err)
		}
	}
	if err := tw.Close(); err != nil {
		panic(err)
	}
	if err := gz.Close(); err != nil {
		panic(err)
	}
}
