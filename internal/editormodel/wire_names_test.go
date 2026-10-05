package editormodel

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/project"
	"github.com/dotwaffle/podsim/internal/wirename"
)

// TestDraftLiteralsMatchProjectTags finds project member names in the
// editor model that match a member name only without case. The editor
// reads drafts as maps, so each name must have the exact case of the tag.
func TestDraftLiteralsMatchProjectTags(t *testing.T) {
	t.Parallel()
	members, _ := wirename.Walk(project.Config{})
	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	// Tests spell names with other cases on purpose.
	files := slices.DeleteFunc(all, func(name string) bool { return strings.HasSuffix(name, "_test.go") })
	found, err := wirename.Mismatch(files, wirename.Names(members))
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range found {
		t.Errorf("%s: %q matches a member name only without case", literal.Position, literal.Value)
	}
}
