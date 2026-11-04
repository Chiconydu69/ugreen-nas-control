package mqtt

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// TestKeysCoverLoadConfig: every v["..."] LoadConfig reads is in Keys.
func TestKeysCoverLoadConfig(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "config.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	ast.Inspect(f, func(x ast.Node) bool {
		if ix, ok := x.(*ast.IndexExpr); ok {
			if id, ok := ix.X.(*ast.Ident); ok && id.Name == "v" {
				if lit, ok := ix.Index.(*ast.BasicLit); ok {
					key, _ := strconv.Unquote(lit.Value)
					n++
					if known, _ := KnownKey(key); !known {
						t.Errorf("%s is read by LoadConfig but not in Keys", key)
					}
				}
			}
		}
		return true
	})
	if n != len(Keys) {
		t.Errorf("LoadConfig reads %d keys, Keys has %d", n, len(Keys))
	}
}
