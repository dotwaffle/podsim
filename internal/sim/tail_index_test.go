package sim_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// callers returns the functions of the product files of a directory that
// call a function, with one entry for each call.
func callers(t *testing.T, directory, name string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path) // #nosec G304 -- A source file of the repository.
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(function, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if target, ok := call.Fun.(*ast.Ident); ok && target.Name == name {
						found = append(found, function.Name.Name)
					}
				}
				return true
			})
		}
	}
	slices.Sort(found)
	return found
}

// TestOneTailIndex pins the calls that build a tail index, because the index
// walks each curved lane. Prepare builds one index for each network. The
// fallback of routeBlocks serves a simulation with no prepared indexes, and
// each of the other entries serves a caller with no prepared network.
func TestOneTailIndex(t *testing.T) {
	t.Parallel()
	want := []string{"LaneTailOverruns", "NewLaneClearance", "ValidateBankGeometry", "prepareNetworkTails", "routeBlocks"}
	if got := callers(t, ".", "indexGeometryTails"); !slices.Equal(got, want) {
		t.Fatalf("indexGeometryTails callers = %v, want %v", got, want)
	}
}
