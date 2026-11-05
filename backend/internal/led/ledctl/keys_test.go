package ledctl

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// flatten turns a key expression into its literal parts; every other part becomes "\x00".
func flatten(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if s, err := strconv.Unquote(x.Value); err == nil {
			return s
		}
	case *ast.BinaryExpr:
		return flatten(x.X) + flatten(x.Y)
	}
	return "\x00"
}

// TestKeyTableCoversTheParser reads the source of this package and checks that every settings key it asks for is in
// the table of KnownKey.
func TestKeyTableCoversTheParser(t *testing.T) {
	fset := token.NewFileSet()
	names, _ := filepath.Glob("*.go")
	var files []*ast.File
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	seen := 0
	check := func(e ast.Expr) {
		k := flatten(e)
		if strings.Trim(k, "\x00") == "" {
			return // a parameter such as get(key): its callers are checked
		}
		seen++
		for _, fill := range []string{"power", "netdev", "disk1", "10", "1"} {
			if known, _ := KnownKey(strings.ReplaceAll(k, "\x00", fill)); known {
				return
			}
		}
		t.Errorf("settings key %q is read but not in the key table (keys.go)", strings.ReplaceAll(k, "\x00", "<x>"))
	}
	for _, f := range files {
		{
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok && len(x.Args) > 0 {
						switch sel.Sel.Name {
						case "get", "colorCfg", "msCfg", "boolNot0", "bool1":
							if id, ok := sel.X.(*ast.Ident); ok && id.Name == "c" {
								check(x.Args[0])
							}
						}
					}
				case *ast.IndexExpr:
					if sel, ok := x.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "cfg" {
						check(x.Index)
					}
				}
				return true
			})
		}
	}
	if seen < 30 {
		t.Errorf("found only %d key reads; the scan no longer matches the code", seen)
	}
}

func TestKnownKey(t *testing.T) {
	for _, c := range []struct {
		key            string
		known, private bool
	}{
		{"mode", true, false}, {"disk8_color", true, false}, {"netdev2_brightness", true, false}, {"netdev_speed_2500", true, false},
		{"netdev_speed_7", false, false}, {"bay1_serial", true, true}, {"smart_ack_WD_X", true, true}, {"mqtt_pass", false, false}, {"disk100_color", false, false},
	} {
		if k, p := KnownKey(c.key); k != c.known || p != c.private {
			t.Errorf("%s: known %t private %t", c.key, k, p)
		}
	}
}
