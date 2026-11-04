package hw

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProductName(t *testing.T) {
	dir := t.TempDir()
	if got := ProductName(dir); got != "" {
		t.Fatalf("missing file: %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "product_name"), []byte("DXP4800 GT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProductName(dir); got != "DXP4800 GT" {
		t.Fatalf("got %q", got)
	}
}
