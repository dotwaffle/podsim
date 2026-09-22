// Command buildweb creates the browser files used by the Podsim server.
package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var staticFiles = []string{"index.html", "game.html", "editor.html", "editor.css", "editor.js"}

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
	if err := buildWASM(ctx); err != nil {
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
	return compressWASM(filepath.Join("dist", "podsim.wasm"))
}

func buildWASM(ctx context.Context) error {
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", filepath.Join("dist", "podsim.wasm"), "./cmd/podsim") // #nosec G204 -- The command and arguments are fixed build inputs.
	command.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build WASM: %w", err)
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

func compressWASM(source string) error {
	input, err := os.Open(source) // #nosec G304 -- The path is a fixed generated output.
	if err != nil {
		return fmt.Errorf("open WASM: %w", err)
	}
	defer func() { _ = input.Close() }()
	destination := source + ".gz"
	output, err := os.Create(destination) // #nosec G304 -- The path is a fixed generated output.
	if err != nil {
		return fmt.Errorf("create compressed WASM: %w", err)
	}
	compressed, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		_ = output.Close()
		return fmt.Errorf("create gzip writer: %w", err)
	}
	if _, err := io.Copy(compressed, input); err != nil {
		_ = compressed.Close()
		_ = output.Close()
		return fmt.Errorf("compress WASM: %w", err)
	}
	if err := compressed.Close(); err != nil {
		_ = output.Close()
		return fmt.Errorf("finish compressed WASM: %w", err)
	}
	if err := output.Close(); err != nil {
		return fmt.Errorf("close compressed WASM: %w", err)
	}
	return nil
}
