//go:build embed_assets

package podsim

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"testing"
)

func TestEmbeddedWebAssets(t *testing.T) {
	t.Parallel()
	assets, ok := WebAssets()
	if !ok {
		t.Fatal("embedded assets are unavailable")
	}
	for _, name := range []string{"index.html", "game.html", "editor.html", "podsim.wasm", "podsim.wasm.gz", "wasm_exec.js"} {
		info, err := fs.Stat(assets, name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("invalid embedded asset %s", name)
		}
	}
	raw, err := fs.ReadFile(assets, "podsim.wasm")
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := assets.Open("podsim.wasm.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = compressed.Close() }()
	reader, err := gzip.NewReader(compressed)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, raw) {
		t.Fatal("compressed WASM does not match the embedded module")
	}
}
