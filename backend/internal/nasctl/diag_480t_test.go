package nasctl

import (
	"strings"
	"testing"
	"time"
)

// The DXP480T Plus report of issue 271: no LED MCU at 0x3a, the power LED chip at 0x26, no fan control, five NVMe SSDs
// without a root-port map.
const scan480T = `{"/dev/i2c-0": {"answer": ["0x26", "0x44", "0x48", "0x4a"], "busy": ["0x50", "0x52"]},
"/dev/i2c-1": {"name": "i915 gmbus dpa", "skipped": "display bus"}}`

func report480T() Report {
	return Report{
		Machine: Machine{Product: "DXP480T Plus"},
		LED:     LEDReport{Supported: true, Probe: "failed", Bus: "/dev/i2c-0", Address: "0x3a"},
		NVMe:    []string{"m2", "m2", "m2", "m2", "m2"},
		Network: []NetReport{{Name: "eth0", MaxMbit: 10000}},
	}
}

func reg480T(t *testing.T) *RegModel {
	t.Helper()
	reg := productReg("DXP480T Plus")
	if reg == nil {
		t.Fatal("DXP480T Plus is not in the registry")
	}
	return reg
}

func check480T(ev Evidence, area, id string) EvCheck {
	for _, c := range ev.Areas[area].Checks {
		if c.ID == id {
			return c
		}
	}
	return EvCheck{}
}

func TestDXP480TLEDControllerIsThePowerChip(t *testing.T) {
	reg := reg480T(t)
	for _, c := range []struct {
		name string
		scan string
		ok   *bool
	}{{"chip answers", scan480T, okp(true)}, {"chip silent", `{"/dev/i2c-0": {"answer": ["0x44"]}}`, okp(false)}, {"no scan", "", nil}} {
		ev := BuildEvidence(EvInputs{Reg: reg, Now: time.Unix(0, 0), Report: report480T(), LED: LEDDiag{Scan: c.scan}})
		got := check480T(ev, "leds", "led_controller")
		if (got.OK == nil) != (c.ok == nil) || (c.ok != nil && *got.OK != *c.ok) {
			t.Errorf("%s: ok %v, want %v (%+v)", c.name, got.OK, c.ok, got)
		}
	}
}

func TestDXP480TChecksTellNoLEDMCUAndNoFan(t *testing.T) {
	reg := reg480T(t)
	led := LEDDiag{Enabled: true, Supported: true, Scan: scan480T, I2C: I2CDiag{Controllers: []I2CCtl{{BDF: "0000:00:1f.5", Driver: "none"}}}}
	r := report480T()
	r.Fan = FanReport{Error: "no fan control"}
	got := strings.Join(diagChecks(Facts{Report: r, LED: led, Reg: reg, FanDaemonDown: true,
		FanSt: &fanStatus{ChipError: "no fan control", FanControl: okp(true)}}), "\n")
	for _, no := range []string{"0x3a on the SMBus", "no I2C device answers", "probe failed", "fan chip", "fan daemon not running", "probably sits behind"} {
		if strings.Contains(got, no) {
			t.Errorf("%q in checks:\n%s", no, got)
		}
	}
	for _, want := range []string{"power LED only", "power LED chip answers at 0x26", "no fan control on this model"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in checks:\n%s", want, got)
		}
	}
	led.Scan = `{"/dev/i2c-0": {"answer": ["0x44"]}}`
	if got := strings.Join(diagChecks(Facts{Report: r, LED: led, Reg: reg}), "\n"); !strings.Contains(got, "does not answer at 0x26") {
		t.Errorf("silent chip not reported:\n%s", got)
	}
}

func TestDXP480TSummariesAreConsistent(t *testing.T) {
	reg := reg480T(t)
	r := report480T()
	if got := reportSummary(r); !strings.Contains(got, "fan chip: no fan control on this model") || !strings.Contains(got, "power LED only") || strings.Contains(got, "0 leds") {
		t.Errorf("report summary:\n%s", got)
	}
	n, _ := modelLEDCount(reg, r.Machine.Product)
	got := feedbackSummary(nil, r, I2CDiag{}, n, reg)
	if want := "No fan control on this model. Power LED only."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDXP480TSlotOrderIsUnknownNotAMismatch(t *testing.T) {
	reg := reg480T(t)
	ev := BuildEvidence(EvInputs{Reg: reg, Now: time.Unix(0, 0), Report: report480T()})
	c := check480T(ev, "case", "nvme_slots")
	if c.OK != nil || !strings.Contains(c.Note, "slot order unknown") {
		t.Errorf("nvme_slots: %+v", c)
	}
	sec, checks := modelSection(reg, ev)
	if strings.Contains(strings.Join(checks, "\n"), "MISMATCH: nvme_slots") || !strings.Contains(sec.Body, "[n/a] (slot order unknown") {
		t.Errorf("checks %v\n%s", checks, sec.Body)
	}
	// a mapped slot that is wrong stays a mismatch
	rep := report480T()
	rep.NVMe = []string{"m2-1", "m2-1"}
	if c := check480T(BuildEvidence(EvInputs{Reg: reg, Now: time.Unix(0, 0), Report: rep}), "case", "nvme_slots"); c.OK == nil || *c.OK {
		t.Errorf("duplicate slot: %+v", c)
	}
}
