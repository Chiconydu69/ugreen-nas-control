package fan

import (
	"os"
	"testing"
	"time"
)

func writeProposal(t *testing.T, model string, outs ...ProbeOutput) {
	t.Helper()
	if err := writeJSONAtomic(probeProposalPath, ProbeProposal{Version: 1, Model: model, Chip: "IT8613", Time: time.Now().Unix(), Outputs: outs}); err != nil {
		t.Fatal(err)
	}
}

func mapped(pwm, tach string) ProbeOutput { return ProbeOutput{PWM: pwm, Status: "mapped", Tach: tach} }

func TestFanKind(t *testing.T) {
	for model, want := range map[string]string{
		"DXP6800 Pro": KindFixed, "DXP4800 GT": KindFixed, "iDX6011 Pro": KindFixed,
		"DXP2800": KindProbe, "DXP4800": KindProbe, "DXP4800 Plus": KindProbe, "DXP4800 Pro": KindProbe, "DXP8800 Plus": KindProbe,
		"DXP2800 GT": KindProbe, "iDX6011": KindReadOnly, "iDX6012": KindReadOnly, "iDX6012 Pro": "", "DX4600 Pro": "", "": "",
		"DXP6800": KindProbe, "DXP6800 Plus": KindProbe, "DXP8800": KindProbe, "DXP8800 Pro": KindProbe,
		"DXP6800 Ultra": "", "DXP6800S": "", "DXP8800 Ultra": "", "DXP2800S": "", "DXP4800S": "", "DH2600": "", "DXP480T Plus": "",
	} {
		if got := FanKind(model); got != want {
			t.Errorf("%q: %q, want %q", model, got, want)
		}
	}
}

func TestConfirmWritesMapAndEnablesControl(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	if err := requireSupportedModel(); err == nil {
		t.Fatal("fan control must stay off without a map")
	}

	writeProposal(t, "DXP4800 Plus", mapped("pwm2", "fan3"), mapped("pwm3", "fan2"), ProbeOutput{PWM: "pwm1", Status: "no_fan"})
	m, err := confirmProbe(map[string]string{"pwm2": "cpu", "pwm3": "case"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Outputs) != 2 || m.Outputs[0] != (FanMapOutput{ID: "fan2", Role: "cpu", PWM: "pwm2", Tach: "fan3"}) ||
		m.Outputs[1] != (FanMapOutput{ID: "fan3", Role: "case", PWM: "pwm3", Tach: "fan2"}) {
		t.Fatalf("map = %+v", m.Outputs)
	}
	if _, err := os.Stat(probeProposalPath); err == nil {
		t.Error("proposal kept after confirming")
	}
	if err := requireSupportedModel(); err != nil {
		t.Fatalf("after confirming: %v", err)
	}
	st := probeStatus()
	if st.Map == nil || !st.FanControl || st.Kind != KindProbe {
		t.Fatalf("status = %+v", st)
	}

	// The map is the wiring the daemon gets: output 2's duty register, tach 3's counters.
	f := newFakeITE()
	chip, err := detectFans(f, "DXP4800 Plus")
	if err != nil {
		t.Fatal(err)
	}
	cpu := chip.Outputs()[0]
	if cpu.ID != "fan2" || cpu.Duty != 0x6b || cpu.Ctrl != 0x16 || cpu.TachLow != 0x0f || cpu.TachHigh != 0x1a {
		t.Fatalf("cpu channel = %+v", cpu)
	}
	// Floor: 0 % is written as 25 %.
	if err := chip.SetPWM(cpu, 0); err != nil {
		t.Fatal(err)
	}
	if f.hwm[0x6b] != percentToDuty(probeFloorPct, 255) {
		t.Fatalf("duty %d, floor %d", f.hwm[0x6b], percentToDuty(probeFloorPct, 255))
	}

	if err := forgetMap(); err != nil {
		t.Fatal(err)
	}
	if err := requireSupportedModel(); err == nil {
		t.Fatal("fan control must be off again after forgetting the map")
	}
}

func TestConfirmRefusals(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	if _, err := confirmProbe(map[string]string{"pwm2": "cpu"}); err == nil {
		t.Fatal("no probe result: expected an error")
	}
	writeProposal(t, "DXP4800 Plus", mapped("pwm2", "fan2"), mapped("pwm3", "fan3"), ProbeOutput{PWM: "pwm1", Status: "no_fan"},
		ProbeOutput{PWM: "pwm4", Status: "ambiguous"}, mapped("pwm5", "fan5"))
	for name, choice := range map[string]map[string]string{
		"empty":       {},
		"no fan":      {"pwm1": "cpu"},
		"ambiguous":   {"pwm4": "case"},
		"not probed":  {"pwm9": "cpu"},
		"bad role":    {"pwm2": "gpu"},
		"three cpus":  {"pwm2": "cpu", "pwm3": "cpu", "pwm5": "cpu"},
		"unknown pwm": {"pwm5": "cpu"},
	} {
		if _, err := confirmProbe(choice); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if _, err := os.Stat(fanMapPath); err == nil {
			t.Fatalf("%s: a map was written", name)
		}
	}

	// A result for another board is not used.
	writeProposal(t, "DXP8800 Plus", mapped("pwm2", "fan2"))
	if _, err := confirmProbe(map[string]string{"pwm2": "cpu"}); err == nil {
		t.Error("proposal of another model accepted")
	}
	// Neither is a stale one.
	if err := writeJSONAtomic(probeProposalPath, ProbeProposal{Version: 1, Model: "DXP4800 Plus", Time: time.Now().Add(-2 * proposalFresh).Unix(), Outputs: []ProbeOutput{mapped("pwm2", "fan2")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := confirmProbe(map[string]string{"pwm2": "cpu"}); err == nil {
		t.Error("stale proposal accepted")
	}
}

func TestMapForAnotherModelIsIgnored(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	writeProposal(t, "DXP4800 Plus", mapped("pwm2", "fan2"))
	if _, err := confirmProbe(map[string]string{"pwm2": "cpu"}); err != nil {
		t.Fatal(err)
	}
	// The flash is moved to a DXP8800 Plus: the map must not carry over.
	if err := os.WriteFile(dmiDir+"/product_name", []byte("DXP8800 Plus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := requireSupportedModel(); err == nil {
		t.Fatal("map of another model accepted")
	}
	if st := probeStatus(); st.Map != nil || st.MapError == "" || st.FanControl {
		t.Fatalf("status = %+v", st)
	}
}

func TestMapValidation(t *testing.T) {
	good := FanMap{Version: 1, Model: "DXP2800", Chip: "IT8613", Outputs: []FanMapOutput{{ID: "fan1", Role: "cpu", PWM: "pwm1", Tach: "fan3"}}}
	if err := good.validate("DXP2800"); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*FanMap){
		"version":       func(m *FanMap) { m.Version = 2 },
		"chip":          func(m *FanMap) { m.Chip = "IT5571" },
		"empty":         func(m *FanMap) { m.Outputs = nil },
		"unknown pwm":   func(m *FanMap) { m.Outputs[0].PWM = "pwm5" },
		"unknown tach":  func(m *FanMap) { m.Outputs[0].Tach = "fan6" },
		"role mismatch": func(m *FanMap) { m.Outputs[0].Role = "case" },
		"unknown id":    func(m *FanMap) { m.Outputs[0].ID = "fan9" },
		"duplicate tach": func(m *FanMap) {
			m.Outputs = append(m.Outputs, FanMapOutput{ID: "fan2", Role: "cpu", PWM: "pwm2", Tach: "fan3"})
		},
		"duplicate pwm": func(m *FanMap) {
			m.Outputs = append(m.Outputs, FanMapOutput{ID: "fan2", Role: "cpu", PWM: "pwm1", Tach: "fan2"})
		},
	} {
		m := good
		m.Outputs = append([]FanMapOutput(nil), good.Outputs...)
		edit(&m)
		if err := m.validate("DXP2800"); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAllocateIDs(t *testing.T) {
	got, err := allocateIDs(map[string]string{"pwm3": "cpu", "pwm4": "cpu"})
	if err != nil {
		t.Fatal(err)
	}
	// fan3 and fan4 are case fans in the daemon's table, so two cpu fans take fan1 and fan2.
	if got[0].ID != "fan1" || got[1].ID != "fan2" {
		t.Fatalf("got %+v", got)
	}
	if _, err := allocateIDs(map[string]string{"pwm1": "case", "pwm2": "case", "pwm3": "case"}); err == nil {
		t.Fatal("three case fans: expected an error")
	}
}

func TestDetectFansWithoutMapOnlyOffersCandidates(t *testing.T) {
	isolate(t, "DXP2800")
	f := newFakeITE()
	chip, err := detectFans(f, "DXP2800")
	if err != nil {
		t.Fatal(err)
	}
	ids := outputIDs(chip.Outputs())
	if len(ids) != 4 || ids[0] != "pwm1" {
		t.Fatalf("outputs = %v", ids)
	}
	if err := requireSupportedModel(); err == nil {
		t.Fatal("the daemon path must refuse without a map")
	}
}

func TestIteRigOnTheChip(t *testing.T) {
	f := newFakeITE()
	chip, err := Detect(f)
	if err != nil {
		t.Fatal(err)
	}
	chip.floor = percentToDuty(probeFloorPct, 255)
	rig := iteRig{chip}

	f.hwm[0x0d], f.hwm[0x18] = 0xec, 0x01 // tach 1
	f.hwm[0x82], f.hwm[0x4c] = 0xd7, 0x03 // tach 5
	for tach, want := range map[string]int{"fan1": 1371, "fan5": 686, "fan2": 1371} {
		if got, err := rig.RPM(tach); err != nil || got != want {
			t.Errorf("%s = %d (%v), want %d", tach, got, err, want)
		}
	}
	if _, err := rig.RPM("fan6"); err == nil {
		t.Error("fan6 accepted")
	}
	if err := rig.SetPct("pwm5", 50); err == nil {
		t.Error("pwm5 accepted: its registers are not confirmed")
	}

	snap, err := rig.Snapshot()
	if err != nil || len(snap) != 4 {
		t.Fatalf("snapshot = %v (%v)", snap, err)
	}
	f.hwm[0x16] = 0x80 // pwm2 in the chip's automatic mode
	if err := rig.SetPct("pwm2", 0); err != nil {
		t.Fatal(err)
	}
	if f.hwm[0x6b] != chip.floor || f.hwm[0x16] != 0 {
		t.Fatalf("duty %#x ctrl %#x, floor %#x", f.hwm[0x6b], f.hwm[0x16], chip.floor)
	}
	if err := rig.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if f.hwm[0x6b] != 0x50 || f.hwm[0x16] != 0 {
		t.Fatalf("after restore: duty %#x ctrl %#x", f.hwm[0x6b], f.hwm[0x16])
	}
}

func TestProbeModelsKeepTheDXP6800Table(t *testing.T) {
	f := newFakeITE()
	chip, err := detectFans(f, "DXP6800 Pro")
	if err != nil {
		t.Fatal(err)
	}
	if c := chip.(*Chip); c.floor != 0 || len(c.Channels) != 3 {
		t.Fatalf("chip = %+v", c)
	}
}
