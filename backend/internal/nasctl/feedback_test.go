package nasctl

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func feedbackFixture(t *testing.T) Feedback {
	ev, _ := evidenceFixture(t)
	rep := Report{
		Fan: FanReport{Found: true, Chip: "IT5571"},
		LED: LEDReport{Supported: true, Probe: "ok", Bus: "/dev/i2c-4", Address: "0x3a", LEDCount: 6},
	}
	ev.Model.ID, ev.Model.Name = "dxp4800-gt", "DXP4800 GT"
	return BuildFeedback(Anon{Hostnames: []string{"tower"}}, ev, nil, rep, I2CDiag{}, "6.12.24-Unraid", "## ugreen-nas-ctl diag\n\nChecks: <host>\n", 12345*time.Millisecond)
}

func TestFeedbackJSONShape(t *testing.T) {
	got := string(feedbackFixture(t).JSON())
	const golden = "testdata/feedback.golden"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("feedback/1 changed (UPDATE_GOLDEN=1 rewrites it):\n%s", got)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(got), &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema", "model", "versions", "summary", "checks", "diag_markdown", "took_ms"} {
		if _, ok := back[k]; !ok {
			t.Errorf("missing %q", k)
		}
	}
}

func okp(b bool) *bool { return &b }

func TestFeedbackSummary(t *testing.T) {
	led := LEDReport{Probe: "ok", Address: "0x3a", LEDCount: 6}
	fan := FanReport{Found: true, Chip: "it8613"}
	cases := []struct {
		name   string
		checks []FBCheck
		r      Report
		i2c    I2CDiag
		want   string
	}{
		{"mismatch", []FBCheck{{ID: "bays", OK: okp(false)}, {ID: "fan_chip", OK: okp(true)}, {ID: "led_support"}}, Report{Fan: fan, LED: led}, I2CDiag{},
			"MISMATCH: bays. ok: fan_chip. not compared: led_support. Fan chip it8613. LED probe ok at 0x3a, 6 LEDs."},
		{"all ok", []FBCheck{{ID: "bays", OK: okp(true)}, {ID: "lan_ports", OK: okp(true)}}, Report{Fan: fan, LED: led}, I2CDiag{},
			"ok: bays, lan_ports. Fan chip it8613. LED probe ok at 0x3a, 6 LEDs."},
		{"led probe failed", []FBCheck{{ID: "led_controller", OK: okp(false)}}, Report{Fan: FanReport{}, LED: LEDReport{Probe: "failed"}}, I2CDiag{},
			"MISMATCH: led_controller. Fan chip not found. LED probe failed."},
		{"unbound controller", nil, Report{Fan: fan, LED: LEDReport{Probe: "failed"}},
			I2CDiag{Controllers: []I2CCtl{{BDF: "0000:00:15.0", Driver: "none"}, {BDF: "0000:00:1f.4", Driver: "i801_smbus"}}},
			"Fan chip it8613. LED probe failed. I2C controllers without driver: 0000:00:15.0."},
		{"spi flash controller is no I2C controller", nil, Report{Fan: fan, LED: LEDReport{Probe: "ok", Address: "0x3a"}},
			I2CDiag{Controllers: []I2CCtl{{BDF: "0000:00:1f.5", ID: "8086:51a4", Driver: "none", Other: true}}},
			"Fan chip it8613. LED probe ok at 0x3a, 0 LEDs."},
	}
	for _, c := range cases {
		if got := feedbackSummary(c.checks, c.r, c.i2c, -1); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	var many []FBCheck
	for range 100 {
		many = append(many, FBCheck{ID: "some_check_id", OK: okp(false)})
	}
	if got := feedbackSummary(many, Report{}, I2CDiag{}, -1); len(got) > summaryMax {
		t.Errorf("summary has %d bytes", len(got))
	}
}

func TestFeedbackHasNoHostName(t *testing.T) {
	ev, _ := evidenceFixture(t)
	ev.Model.Name = "DXP4800 GT on tower"
	f := BuildFeedback(Anon{Hostnames: []string{"tower"}}, ev, nil, Report{}, I2CDiag{}, "6.12.24-Unraid", "", 0)
	if strings.Contains(string(f.JSON()), "tower") {
		t.Errorf("host name in %s", f.JSON())
	}
}

func TestDiagProgressGoesToStderrInTextMode(t *testing.T) {
	var out, errw bytes.Buffer
	c := New(Env{}, &out, &errw)
	c.diagNote(false, "collecting, this takes up to a minute (I2C scan)...")
	c.diagNote(false, "done in %d s", 12)
	if out.Len() != 0 {
		t.Errorf("stdout: %q", out.String())
	}
	if got := errw.String(); got != "diag: collecting, this takes up to a minute (I2C scan)...\ndiag: done in 12 s\n" {
		t.Errorf("stderr: %q", got)
	}
	errw.Reset()
	c.diagNote(true, "collecting")
	if out.Len() != 0 || errw.Len() != 0 {
		t.Errorf("--json printed %q %q", out.String(), errw.String())
	}
}

func TestFeedbackSummaryFitsByConstruction(t *testing.T) {
	var many []FBCheck
	for range 100 {
		many = append(many, FBCheck{ID: "some_check_id", OK: okp(true)})
	}
	many = append(many, FBCheck{ID: "bays", OK: okp(false)})
	ctl := make([]I2CCtl, 40)
	for i := range ctl {
		ctl[i] = I2CCtl{BDF: "0000:00:1f.5", Driver: "none"}
	}
	r := Report{Fan: FanReport{Found: true, Chip: "IT8613"}, LED: LEDReport{Probe: "ok", Address: "0x3a"}}
	got := feedbackSummary(many, r, I2CDiag{Controllers: ctl}, 4)
	if len(got) > summaryMax {
		t.Errorf("summary has %d bytes", len(got))
	}
	for _, want := range []string{"MISMATCH: bays.", "Fan chip IT8613.", "LED probe ok at 0x3a, 4 LEDs.", ", …."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if !utf8.ValidString(got) || strings.HasSuffix(got, "contro") || !strings.HasSuffix(got, ".") {
		t.Errorf("cut mid-clause: %q", got)
	}
}

func TestModelLEDCount(t *testing.T) {
	n := func(v int) *int { return &v }
	for _, c := range []struct {
		name    string
		reg     *RegModel
		product string
		want    int
		known   bool
	}{
		{"DXP2800 has power, netdev, disk1, disk2", &RegModel{LED: "bays", Bays: n(2)}, "DXP2800", 4, true},
		{"DXP8800 Plus", &RegModel{LED: "bays", Bays: n(8)}, "DXP8800 Plus", 10, true},
		{"iDX6011 Pro has a second LAN LED", &RegModel{LED: "bays", Bays: n(6)}, "iDX6011 Pro", 9, true},
		{"power LED only", &RegModel{LED: "power", Bays: n(0)}, "DXP480T Plus", 1, true},
		{"no LED controller", &RegModel{LED: "none", Bays: n(2)}, "DH2600", 0, true},
		{"unknown model", nil, "Foo", 0, false},
	} {
		got, known := modelLEDCount(c.reg, c.product)
		if got != c.want || known != c.known {
			t.Errorf("%s: got %d, %t", c.name, got, known)
		}
	}
}

func TestFeedbackSummaryCountsTheModelsLEDs(t *testing.T) {
	ev, _ := evidenceFixture(t)
	bays := 2
	rep := Report{Fan: FanReport{Found: true, Chip: "IT8613"}, LED: LEDReport{Probe: "ok", Address: "0x3a", LEDCount: 6}}
	f := BuildFeedback(Anon{}, ev, &RegModel{LED: "bays", Bays: &bays}, rep, I2CDiag{}, "k", "", 0)
	if !strings.Contains(f.Summary, "0x3a, 4 LEDs.") {
		t.Errorf("summary %q", f.Summary)
	}
}
