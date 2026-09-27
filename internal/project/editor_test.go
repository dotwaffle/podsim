package project

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// TestEditorMirrorsMaxFileBytes checks that the SERVER_PROJECT_BYTES value in
// the scenario editor is MaxFileBytes. The editor uses that value to set the
// largest project file that it imports.
func TestEditorMirrorsMaxFileBytes(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../../web/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`const SERVER_PROJECT_BYTES = (\d+) \* 1024 \* 1024;`).FindSubmatch(source)
	if match == nil {
		t.Fatal("web/editor.js has no SERVER_PROJECT_BYTES value in the form N * 1024 * 1024")
	}
	mebibytes, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	if got := mebibytes << 20; got != MaxFileBytes {
		t.Fatalf("editor SERVER_PROJECT_BYTES is %d, want project.MaxFileBytes %d", got, MaxFileBytes)
	}
}
