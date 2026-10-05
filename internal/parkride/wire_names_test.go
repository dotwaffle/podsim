package parkride

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/dotwaffle/podsim/internal/wirename"
)

// TestCheckpointRulesMatchTags keeps each checkpoint member name in
// lowerCamel case. It checks the frozen member rules and the scanner paths
// of the checkpoint against the struct tags, so that a renamed member
// cannot leave a rule or a bound that names nothing.
func TestCheckpointRulesMatchTags(t *testing.T) {
	t.Parallel()
	members, _ := wirename.Walk(checkpointFile{})
	for _, member := range members {
		if !wirename.LowerCamel(member.Name) {
			t.Errorf("%s.%s has JSON name %q, want lowerCamel", member.Owner, member.Field, member.Name)
		}
	}
	paths := wirename.Paths(members)
	for path, r := range checkpointRules {
		if path != "" && !paths[trimElements(path)] {
			t.Errorf("rule path %q names no member", path)
		}
		for field := range r.fields {
			if !paths[path+"/"+field] {
				t.Errorf("rule member %q at %q names no member", field, path)
			}
		}
	}
	for _, path := range arrayLimitPaths(t) {
		for strings.HasSuffix(path, "/*") {
			path = trimElements(path)
		}
		if !paths[path] {
			t.Errorf("array limit path %q names no member", path)
		}
	}
	found, err := wirename.Mismatch([]string{"checkpoint_scan.go", "checkpoint_restore.go", "foundation.go", "checkpoint.go"}, wirename.Names(members))
	if err != nil {
		t.Fatal(err)
	}
	for _, literal := range found {
		t.Errorf("%s: %q matches a member name only without case", literal.Position, literal.Value)
	}
}

// trimElements removes a trailing array element segment from a rule path,
// because the walk lists members, not array elements.
func trimElements(path string) string {
	return strings.TrimSuffix(path, "/*")
}

// arrayLimitPaths returns the case paths of arrayLimit, so that a renamed
// or misspelled path cannot leave an array at the general element limit.
func arrayLimitPaths(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "checkpoint_scan.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Name.Name != "arrayLimit" {
			continue
		}
		ast.Inspect(function, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expr := range clause.List {
				literal, ok := expr.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("arrayLimit case %T is not a string literal", expr)
				}
				path, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			return true
		})
	}
	if len(paths) == 0 {
		t.Fatal("arrayLimit has no case paths")
	}
	return paths
}
