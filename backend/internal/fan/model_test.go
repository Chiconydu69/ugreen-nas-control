package fan

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelSupported(t *testing.T) {
	for model, want := range map[string]bool{
		"DXP6800 Pro":   true,
		"DXP6800 Plus":  true,
		"DXP4800 Plus":  true,
		"DXP4800 GT":    true,
		"DXP8800 Plus":  true,
		"DXP8800 Pro":   true,
		"DXP6800":       true,
		"DXP6800 Ultra": false,
		"DX4600 Pro":    false,
		"DXP2800":       true,
		"DXP2800 GT":    true,
		"iDX6011 Pro":   true,
		"iDX6011":       false, // read only: shown, not driven
		"":              false,
	} {
		if got := ModelSupported(model); got != want {
			t.Errorf("%q: got %v, want %v", model, got, want)
		}
	}
}

func TestRequireSupportedModel(t *testing.T) {
	old, oldMap := dmiDir, fanMapPath
	defer func() { dmiDir, fanMapPath = old, oldMap }()
	dmiDir, fanMapPath = t.TempDir(), filepath.Join(t.TempDir(), "fan_map.json")

	if err := requireSupportedModel(); err == nil {
		t.Fatal("no DMI data: expected an error")
	}

	name := filepath.Join(dmiDir, "product_name")
	for model, ok := range map[string]bool{"DXP6800 Pro\n": true, "DXP4800 GT\n": true, "DXP4800 Pro\n": false, "iDX6011 Pro\n": true, "iDX6011\n": true, "DXP2800 GT\n": false} {
		if err := os.WriteFile(name, []byte(model), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := requireSupportedModel(); (err == nil) != ok {
			t.Errorf("%q: err %v, want ok=%v", model, err, ok)
		}
	}
}

// The DXP2800 GT has a Chipsea CS201x: the fan control is gated by `cs201x confirm`, and none of the IT8613E probe
// paths may run there (they write Super-I/O registers).
func TestDXP2800GTGate(t *testing.T) {
	isolate(t, "DXP2800 GT")
	if err := requireSupportedModel(); !errors.Is(err, errCSNotConfirmed) {
		t.Fatalf("without a confirmation: %v", err)
	}
	confirmedMap(t, "IT8613", "fan1")
	if err := requireSupportedModel(); err == nil {
		t.Error("an IT8613 map opened the gate")
	}
	confirmedMap(t, csChip, "fan1")
	if err := requireSupportedModel(); err != nil {
		t.Errorf("confirmed: %v", err)
	}
	c, err := detectFans(newFakeCS(), "DXP2800 GT")
	if err != nil || c.Name() != csChip {
		t.Fatalf("detectFans: %v %v", c, err)
	}
	if caps, ok := CapsFor("DXP2800 GT"); !ok || caps.Kind != KindProbe || caps.Channels != 1 {
		t.Errorf("caps %+v %v", caps, ok)
	}
	if st := probeStatus(); !st.FanControl || st.Map == nil {
		t.Errorf("status %+v", st)
	}
}

func TestDXP2800GTRefusesIT8613Probe(t *testing.T) {
	isolate(t, "DXP2800 GT")
	if err := runProbe(); err == nil || !strings.Contains(err.Error(), "CS201x") {
		t.Errorf("runProbe: %v", err)
	}
	if _, err := confirmProbe(map[string]string{"pwm2": "cpu"}); err == nil || !strings.Contains(err.Error(), "CS201x") {
		t.Errorf("confirmProbe: %v", err)
	}
	if _, err := readTachs(); err == nil || !strings.Contains(err.Error(), "CS201x") {
		t.Errorf("readTachs: %v", err)
	}
	if _, err := restoreProbe(); err == nil || !strings.Contains(err.Error(), "CS201x") {
		t.Errorf("restoreProbe: %v", err)
	}
	if st := probeStatus(); st.FanControl || st.Map != nil || st.Proposal != nil {
		t.Errorf("status %+v", st)
	}
}

// The plain iDX shows its fans and never controls them.
func TestPlainIdxReadOnlyKind(t *testing.T) {
	isolate(t, "iDX6012")
	if err := requireSupportedModel(); err != nil {
		t.Fatalf("reads must work: %v", err)
	}
	f := newFakeEC()
	c, err := detectFans(f, "iDX6012")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetPWM(c.Outputs()[0], 50); !errors.Is(err, errReadOnly) {
		t.Errorf("SetPWM: %v", err)
	}
	old := daemonModel
	daemonModel = func() string { return "iDX6012" }
	defer func() { daemonModel = old }()
	if err := runDaemon(c); err == nil || !strings.Contains(err.Error(), "read only") {
		t.Errorf("daemon: %v", err)
	}
	if len(f.writes) != 0 {
		t.Errorf("writes: %v", f.writes)
	}
}
