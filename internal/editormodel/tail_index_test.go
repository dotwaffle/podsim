package editormodel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestOneDraftTailIndex pins that one editor check builds one clearance index,
// because the index walks each curved lane, and that it gives the index to the
// server validation and to the warnings.
func TestOneDraftTailIndex(t *testing.T) {
	t.Parallel()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "checks.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	calls := make(map[string]int)
	draftKeys := make(map[string]bool)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "checkNetwork" {
			continue
		}
		ast.Inspect(function, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.CallExpr:
				if target, ok := node.Fun.(*ast.Ident); ok {
					calls[target.Name]++
				}
			case *ast.CompositeLit:
				if target, ok := node.Type.(*ast.Ident); ok && target.Name == "serverDraft" {
					for _, element := range node.Elts {
						if pair, ok := element.(*ast.KeyValueExpr); ok {
							if key, ok := pair.Key.(*ast.Ident); ok {
								draftKeys[key.Name] = true
							}
						}
					}
				}
			}
			return true
		})
	}
	if calls["newDraftClearance"] != 1 || calls["validateBankDraft"] != 0 || calls["draftTailWarnings"] != 1 {
		t.Fatalf("checkNetwork calls = %v", calls)
	}
	if !draftKeys["clearance"] {
		t.Fatalf("the server draft of checkNetwork has only %v", draftKeys)
	}
	// The warnings read the index that they get.
	source, err := os.ReadFile("large_geometry.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(source), "project.TailWarnings(") {
		t.Fatal("draftTailWarnings builds a new tail index")
	}
}
