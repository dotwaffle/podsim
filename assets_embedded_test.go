//go:build embed_assets

package podsim

import (
	"bytes"
	"compress/gzip"
	"errors"
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
	for _, name := range []string{"index.html", "shell.js", "game.html", "tiles.js", "simulation-map.js", "loader.js", "editor.html", "editor-model.js", "editor-model.wasm.gz", "podsim.wasm.gz", "wasm_exec.js"} {
		info, err := fs.Stat(assets, name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("invalid embedded asset %s", name)
		}
	}
	for _, name := range []string{"podsim.wasm", "editor-model.wasm"} {
		if _, err := fs.Stat(assets, name); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s: %v, want only the compressed module", name, err)
		}
	}
	for _, name := range []string{"podsim.wasm.gz", "editor-model.wasm.gz"} {
		compressed, err := assets.Open(name)
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
		if !bytes.HasPrefix(decoded, []byte("\x00asm")) {
			t.Fatalf("%s does not hold a WASM module", name)
		}
	}
}
