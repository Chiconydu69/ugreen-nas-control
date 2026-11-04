// Package panel reads the front-panel buttons and drives the beeper of UGREEN NAS models. Facts come from UGREEN's
// GPL-2.0-only drivers (drivers/ugreen/ @c75c5ab, cited as file:line@c75c5ab); the code is our own. Every table
// entry is vendor source, not yet confirmed on hardware.
package panel

import "strings"

// matcher selects a model by its DMI product_name the way the vendor drivers do: strcmp (exact) or strncmp (prefix).
type matcher struct {
	exact  []string
	prefix []string
}

func (m matcher) ok(product string) bool {
	// DXP6800 Ultra and DXP8800 Ultra are newer than the vendor source; its prefixes would match them by accident.
	if strings.Contains(product, "Ultra") {
		return false
	}
	for _, e := range m.exact {
		if product == e {
			return true
		}
	}
	for _, p := range m.prefix {
		if strings.HasPrefix(product, p) {
			return true
		}
	}
	return false
}
