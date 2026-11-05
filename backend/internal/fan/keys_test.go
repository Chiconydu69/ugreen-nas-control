package fan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// TestKnownSettingCoversLoadConfig: every key LoadConfig reads (v["..."], number("...")) is a KnownSetting, so the
// diagnostics, which print only known keys, never hide a setting the daemon uses.
func TestKnownSettingCoversLoadConfig(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "config.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(x ast.Node) bool {
		var lit *ast.BasicLit
		switch e := x.(type) {
		case *ast.IndexExpr:
			if id, ok := e.X.(*ast.Ident); ok && id.Name == "v" {
				lit, _ = e.Index.(*ast.BasicLit)
			}
		case *ast.CallExpr:
			if id, ok := e.Fun.(*ast.Ident); ok && id.Name == "number" && len(e.Args) > 0 {
				lit, _ = e.Args[0].(*ast.BasicLit)
			}
		}
		if lit != nil {
			key, _ := strconv.Unquote(lit.Value)
			n++
			if !KnownSetting(key) {
				t.Errorf("%s is read by LoadConfig but KnownSetting says no", key)
			}
		}
		return true
	})
	if n < 10 {
		t.Errorf("found only %d key reads", n)
	}
}
