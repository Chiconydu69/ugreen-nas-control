package nasctl

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func feedbackFixture(t *testing.T) Feedback {
	ev, _ := evidenceFixture(t)
	rep := Report{
		Fan: FanReport{Found: true, Chip: "IT5571"},
		LED: LEDReport{Supported: true, Probe: "ok", Bus: "/dev/i2c-4", Address: "0x3a", LEDCount: 6},
	}
	ev.Model.ID, ev.Model.Name = "dxp4800-gt", "DXP4800 GT"
	return BuildFeedback(Anon{Hostnames: []string{"tower"}}, ev, rep, I2CDiag{}, "6.12.24-Unraid", "## ugreen-nas-ctl diag\n\nChecks: <host>\n", 12345*time.Millisecond)
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
	}
	for _, c := range cases {
		if got := feedbackSummary(c.checks, c.r, c.i2c); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	var many []FBCheck
	for range 100 {
		many = append(many, FBCheck{ID: "some_check_id", OK: okp(false)})
	}
	if got := feedbackSummary(many, Report{}, I2CDiag{}); len(got) > summaryMax+3 {
		t.Errorf("summary has %d bytes", len(got))
	}
}

func TestFeedbackHasNoHostName(t *testing.T) {
	ev, _ := evidenceFixture(t)
	ev.Model.Name = "DXP4800 GT on tower"
	f := BuildFeedback(Anon{Hostnames: []string{"tower"}}, ev, Report{}, I2CDiag{}, "6.12.24-Unraid", "", 0)
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
