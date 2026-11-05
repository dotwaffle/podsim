package editormodel

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// draftTailCalls holds what one function of a source file does with a
// clearance index.
type draftTailCalls struct {
	// calls counts the calls of each plain function.
	calls map[string]int
	// draftKeys holds the keys of its serverDraft literals.
	draftKeys map[string]bool
	// serverIndex reports the statement server.clearance = clearance.index.
	serverIndex bool
}

func newDraftTailCalls(t *testing.T, path, name string) draftTailCalls {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	found := draftTailCalls{calls: make(map[string]int), draftKeys: make(map[string]bool)}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != name {
			continue
		}
		ast.Inspect(function, func(node ast.Node) bool {
			found.note(node)
			return true
		})
	}
	return found
}

func (d *draftTailCalls) note(node ast.Node) {
	switch node := node.(type) {
	case *ast.AssignStmt:
		if len(node.Lhs) == 1 && len(node.Rhs) == 1 && selectorText(node.Lhs[0]) == "server.clearance" && selectorText(node.Rhs[0]) == "clearance.index" {
			d.serverIndex = true
		}
	case *ast.CallExpr:
		if target, ok := node.Fun.(*ast.Ident); ok {
			d.calls[target.Name]++
		}
	case *ast.CompositeLit:
		if target, ok := node.Type.(*ast.Ident); ok && target.Name == "serverDraft" {
			for _, element := range node.Elts {
				if pair, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := pair.Key.(*ast.Ident); ok {
						d.draftKeys[key.Name] = true
					}
				}
			}
		}
	}
}

// selectorText returns the text of a selector of a plain name, or "".
func selectorText(expression ast.Expr) string {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	base, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return base.Name + "." + selector.Sel.Name
}

// TestOneDraftTailIndex pins that one editor check builds one clearance index,
// because the index walks each curved lane, and that it gives the index to the
// server validation and to the warnings. An edit builds one index for the
// edited network and one for the original, and the server validation reads the
// first.
func TestOneDraftTailIndex(t *testing.T) {
	t.Parallel()
	check := newDraftTailCalls(t, "checks.go", "checkNetwork")
	if check.calls["newDraftClearance"] != 1 || check.calls["validateBankDraft"] != 0 || check.calls["draftTailWarnings"] != 1 {
		t.Fatalf("checkNetwork calls = %v", check.calls)
	}
	if !check.draftKeys["clearance"] {
		t.Fatalf("the server draft of checkNetwork has only %v", check.draftKeys)
	}
	edit := newDraftTailCalls(t, "edit_banks.go", "validateBankChanges")
	if edit.calls["newDraftClearance"] != 2 || edit.calls["validateBankDraft"] != 0 {
		t.Fatalf("validateBankChanges calls = %v", edit.calls)
	}
	if !edit.serverIndex {
		t.Fatal("validateBankChanges does not give the edited index to the server draft")
	}
	if server := newDraftTailCalls(t, "edit_geometry.go", "serverChecks"); server.calls["newDraftClearance"] != 0 || server.calls["validateBankDraft"] != 0 {
		t.Fatalf("serverChecks calls = %v", server.calls)
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
