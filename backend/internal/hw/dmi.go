package hw

import (
	"os"
	"path/filepath"
	"strings"
)

const DMIDir = "/sys/class/dmi/id"

// ProductName is the trimmed product_name in dir ("" when unreadable), the model the plugin detects hardware by.
func ProductName(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, "product_name"))
	return strings.TrimSpace(string(b))
}

// SystemVersion is the trimmed product_version in dir (dmidecode "System Information: Version"), "" when unreadable.
func SystemVersion(dir string) string {
	b, _ := os.ReadFile(filepath.Join(dir, "product_version"))
	return strings.TrimSpace(string(b))
}
