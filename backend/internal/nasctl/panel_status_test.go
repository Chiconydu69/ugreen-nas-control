package nasctl

import "testing"

func TestPanelStatus(t *testing.T) {
	for product, want := range map[string]string{
		"DXP8800 Plus":   "partly confirmed by users, #165",
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

// The shutdown on a held power button is offered where the registry panel check is community or verified (no model today), not for a
// partial or expected check, an unsupported model or an unknown one.
func TestPanelConfirmed(t *testing.T) {
	for product, want := range map[string]bool{
		"DXP8800 Plus":   false, // partial: reset and beeper; the power pad shows only a delayed pulse (#165)
		"DXP4800 Plus":   false, // partial: the beeper only
		"DXP6800 Pro":    false,
		"DXP6800 Ultra":  false,
		"Something Else": false,
		"":               false,
	} {
		if got := PanelConfirmed(product); got != want {
			t.Errorf("PanelConfirmed(%q) = %v, want %v", product, got, want)
		}
	}
}
