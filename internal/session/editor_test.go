package session

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
)

// TestEditorMirrorsCommandLimits checks that the scenario editor has the
// command limits of the server. SERVER_COMMAND_BYTES must be
// MaxCommandBytes, and SERVER_COMMAND_JSON_BYTES must be
// MaxInflatedCommandBytes. SERVER_COMMAND_JSON_BYTES adds a number of KiB to
// SERVER_PROJECT_BYTES, and a test in internal/project checks that
// SERVER_PROJECT_BYTES is project.MaxFileBytes.
func TestEditorMirrorsCommandLimits(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("../../web/editor.js")
	if err != nil {
		t.Fatal(err)
	}
	value := func(pattern string) int {
		t.Helper()
		match := regexp.MustCompile(pattern).FindSubmatch(source)
		if match == nil {
			t.Fatalf("web/editor.js has no value in the form %s", pattern)
		}
		number, err := strconv.Atoi(string(match[1]))
		if err != nil {
			t.Fatal(err)
		}
		return number
	}
	if value(`const MIB = (1024) \* 1024;`) != 1024 {
		t.Fatal("web/editor.js has a different MIB")
	}
	if got := value(`const SERVER_COMMAND_BYTES = (\d+) \* MIB;`) << 20; got != MaxCommandBytes {
		t.Errorf("editor SERVER_COMMAND_BYTES is %d, want MaxCommandBytes %d", got, MaxCommandBytes)
	}
	if got := project.MaxFileBytes + value(`const SERVER_COMMAND_JSON_BYTES = SERVER_PROJECT_BYTES \+ (\d+) \* 1024;`)<<10; got != MaxInflatedCommandBytes {
		t.Errorf("editor SERVER_COMMAND_JSON_BYTES is %d, want MaxInflatedCommandBytes %d", got, MaxInflatedCommandBytes)
	}
}

// TestBrowserMirrorsStreamLimits checks that the debug capture and the
// editor refuse a state reply with the depth and element limits of a
// stream document. MAX_STATE_DEPTH must be the depth limit and
// MAX_STATE_ELEMENTS the element limit of streamLimits.
func TestBrowserMirrorsStreamLimits(t *testing.T) {
	t.Parallel()
	limits := streamLimits(contractMarkers{})
	for _, name := range []string{"shell.js", "editor.js"} {
		source, err := os.ReadFile("../../web/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []struct {
			constant string
			value    int64
		}{{"MAX_STATE_DEPTH", int64(limits.depth)}, {"MAX_STATE_ELEMENTS", limits.elements}} {
			match := regexp.MustCompile(`const ` + want.constant + ` = (\d+);`).FindSubmatch(source)
			if match == nil {
				t.Fatalf("web/%s has no %s value", name, want.constant)
			}
			if got, err := strconv.ParseInt(string(match[1]), 10, 64); err != nil || got != want.value {
				t.Errorf("web/%s %s is %s, want %d", name, want.constant, match[1], want.value)
			}
		}
	}
}
