package nasctl

import "testing"

func TestPanelStatus(t *testing.T) {
	for product, want := range map[string]string{
		"DXP8800 Plus":   "confirmed by users, #165",
		"DXP4800 Plus":   "partly confirmed by users, #266",
		"DXP4800 GT":     "vendor source, not yet confirmed",
		"DXP6800 Ultra":  "vendor source, not yet confirmed",
		"Something Else": "vendor source, not yet confirmed",
		"":               "vendor source, not yet confirmed",
	} {
		if got := PanelStatus(product); got != want {
			t.Errorf("PanelStatus(%q) = %q, want %q", product, got, want)
		}
	}
}
