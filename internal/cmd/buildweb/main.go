// Command buildweb creates the browser files used by the Podsim server.
package main

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var staticFiles = []string{"index.html", "game.html", "loader.js", "editor.html", "editor.css", "editor.js"}

func main() {
	if err := run(context.Background()); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "build browser application: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if err := os.MkdirAll("dist", 0o750); err != nil {
		return fmt.Errorf("create dist: %w", err)
	}
	if err := buildWASM(ctx, filepath.Join("dist", "podsim.wasm.gz")); err != nil {
		return err
	}
	output, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output() // #nosec G204 -- The command and arguments are fixed build inputs.
	if err != nil {
		return fmt.Errorf("find GOROOT: %w", err)
	}
	goRoot := strings.TrimSpace(string(output))
	if err := copyFile(filepath.Join(goRoot, "lib", "wasm", "wasm_exec.js"), filepath.Join("dist", "wasm_exec.js")); err != nil {
		return err
	}
	for _, name := range staticFiles {
		if err := copyFile(filepath.Join("web", name), filepath.Join("dist", name)); err != nil {
			return err
		}
	}
	return nil
}

// buildWASM builds the WASM module in a temporary directory and writes only
// the compressed module to destination. The server decompresses the module
// for clients that do not accept gzip. This keeps one copy of the module in
// dist and in the embedded server.
func buildWASM(ctx context.Context, destination string) error {
	scratch, err := os.MkdirTemp("", "podsim-wasm-")
	if err != nil {
		return fmt.Errorf("create WASM build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	module := filepath.Join(scratch, "podsim.wasm")
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", module, "./cmd/podsim") // #nosec G204 -- The command and arguments are fixed build inputs.
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build WASM: %w", err)
	}
	if err := compressWASM(module, destination); err != nil {
		return err
	}
	// Remove the raw module of an older build.
	if err := os.Remove(filepath.Join(filepath.Dir(destination), "podsim.wasm")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove old WASM: %w", err)
	}
	return nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source) // #nosec G304 -- All paths are fixed build inputs.
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.Create(destination) // #nosec G304 -- All paths are fixed generated outputs.
	if err != nil {
		return fmt.Errorf("create %s: %w", destination, err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return fmt.Errorf("copy %s: %w", source, err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close %s: %w", destination, err)
	}
	return nil
}

// compressWASM writes source to destination with maximum gzip compression.
// The gzip header has no name and no modification time, so the same module
// always gives the same bytes.
// It writes a temporary file beside destination and renames it over
// destination only after the write succeeds. A running server thus never
// reads a partial file, and a failed build keeps the previous module.
func compressWASM(source, destination string) (err error) {
	input, err := os.Open(source) // #nosec G304 -- The path is a fixed generated output.
	if err != nil {
		return fmt.Errorf("open WASM: %w", err)
	}
	defer func() { _ = input.Close() }()
	output, err := os.CreateTemp(filepath.Dir(destination), ".podsim.wasm.gz-*")
	if err != nil {
		return fmt.Errorf("create compressed WASM: %w", err)
	}
	defer func() {
		if err != nil {
			_ = output.Close()
			_ = os.Remove(output.Name())
		}
	}()
	if err := writeGzip(output, input); err != nil {
		return err
	}
	if err := output.Chmod(0o644); err != nil {
		return fmt.Errorf("set compressed WASM mode: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close compressed WASM: %w", err)
	}
	if err := os.Rename(output.Name(), destination); err != nil {
		return fmt.Errorf("publish compressed WASM: %w", err)
	}
	return nil
}

// writeGzip compresses input into output with maximum gzip compression.
func writeGzip(output io.Writer, input io.Reader) error {
	compressed, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := io.Copy(compressed, input); err != nil {
		_ = compressed.Close()
		return fmt.Errorf("compress WASM: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return fmt.Errorf("finish compressed WASM: %w", err)
	}
	return nil
}
