package fan

import "testing"

func TestCapsFor(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	for _, tc := range []struct {
		model string
		want  FanCaps
		ok    bool
	}{
		{"DXP4800 GT", FanCaps{KindFixed, 2, true, false, 0}, true},
		{"iDX6011 Pro", FanCaps{KindFixed, 4, true, true, 0}, true},
		{"iDX6012 Pro", FanCaps{}, false},
		{"DXP2800 GT", FanCaps{KindProbe, 0, false, false, csFloorPct}, true},
		{"DXP6800 Pro", FanCaps{KindFixed, 3, true, false, 0}, true},
		{"DXP4800 Plus", FanCaps{KindProbe, 0, true, false, 0}, true},
		{"iDX6011", FanCaps{KindReadOnly, 2, false, false, 0}, true},
		{"iDX6012", FanCaps{KindReadOnly, 2, false, false, 0}, true},
		{"DX4600 Pro", FanCaps{}, false},
	} {
		got, ok := CapsFor(tc.model)
		if ok != tc.ok || got != tc.want {
			t.Errorf("CapsFor(%q) = %+v, %v; want %+v, %v", tc.model, got, ok, tc.want, tc.ok)
		}
	}
}

func TestStepsFromSkipsStepsBelowTheFloor(t *testing.T) {
	if got := stepsFrom(0); len(got) != len(calibSteps) {
		t.Fatalf("no floor: %v", got)
	}
	if got := stepsFrom(csFloorPct); got[0] != csFloorPct || len(got) != len(calibSteps)-3 {
		t.Fatalf("floor %d: %v", csFloorPct, got)
	}
}
