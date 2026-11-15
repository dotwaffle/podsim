// Command texgen writes the tiling textures of the ride page as PNG files
// in web/textures. The textures are generated, so a change to a texture is
// a change to this command. Run it from the repository root:
//
//	go run ./internal/cmd/texgen
//
// TestTextureFiles fails when a file does not match the output.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	output := flag.String("o", filepath.Join("web", "textures"), "output directory of the PNG files")
	flag.Parse()
	if err := writeTextures(*output); err != nil {
		fmt.Fprintln(os.Stderr, "texgen:", err)
		os.Exit(1)
	}
}

// writeTextures writes each texture to directory.
func writeTextures(directory string) error {
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	for _, texture := range textures {
		data, err := encodePNG(texture.draw())
		if err == nil {
			err = os.WriteFile(filepath.Join(directory, texture.file), data, 0o644) // #nosec G306 -- The textures are public web assets.
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", texture.file, err)
		}
	}
	return nil
}
