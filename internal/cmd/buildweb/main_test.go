package main

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCompressWASM(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	source := filepath.Join(directory, "podsim.wasm")
	destination := filepath.Join(directory, "dist", "podsim.wasm.gz")
	if err := os.Mkdir(filepath.Dir(destination), 0o750); err != nil {
		t.Fatal(err)
	}
	previous := []byte("previous module")
	if err := os.WriteFile(destination, previous, 0o600); err != nil {
		t.Fatal(err)
	}
	// A failed build keeps the previous file and leaves no temporary file.
	if err := compressWASM(filepath.Join(directory, "missing.wasm"), destination); err == nil {
		t.Fatal("compressWASM succeeded without a source")
	}
	if got, _ := os.ReadFile(destination); !bytes.Equal(got, previous) {
		t.Fatalf("destination %q after a failed build, want %q", got, previous)
	}
	raw := bytes.Repeat([]byte("\x00asm module"), 100)
	if err := os.WriteFile(source, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := compressWASM(source, destination); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(destination))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("dist has %d entries, want only podsim.wasm.gz", len(entries))
	}
	file, err := os.Open(destination) // #nosec G304 -- The path is a test file.
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(reader); err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("decoded %d bytes, error %v", len(got), err)
	}
}
