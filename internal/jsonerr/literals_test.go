package jsonerr

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoLibraryWordingInTests fails when a test file pins the json/v2
// modal verb, which changes from one process to the next.
func TestNoLibraryWordingInTests(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	// The strings are built in parts so that this file does not match.
	banned := []string{"unable to " + "unmarshal", "cannot " + "unmarshal"}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, word := range banned {
			if strings.Contains(string(data), word) {
				t.Errorf("%s contains %q", path, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
