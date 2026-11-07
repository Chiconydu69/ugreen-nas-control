package nasctl

import (
	"os"
	"strings"
	"testing"
)

// The diag of an IT8613E box asks the fan tool for the raw header 4 control registers (read only); other chips are not asked.
func TestFanDiagReadsHeader4ControlRegisters(t *testing.T) {
	for model, want := range map[string]bool{
		"DXP8800 Plus": true, "DXP6800 Pro": true, "DXP2800": true,
		"DXP2800 GT": false, "DXP4800 GT": false, "iDX6011 Pro": false, "DX4700+": false,
	} {
		f := newFixture(t)
		log := f.dir + "/calls"
		write(t, f.c.Env.FanTool, "#!/bin/sh\necho \"$@\" >>"+log+"\necho '{}'\n")
		if err := os.Chmod(f.c.Env.FanTool, 0o755); err != nil {
			t.Fatal(err)
		}
		d := f.c.fanDiag(model)
		calls, _ := os.ReadFile(log)
		if got := strings.Contains(string(calls), "probe-fans regs"); got != want {
			t.Errorf("%s: probe-fans regs asked = %v, want %v\n%s", model, got, want, calls)
		}
		var key bool
		for _, r := range d.Extra {
			key = key || r.Key == "ite-ctrl"
		}
		if key != want {
			t.Errorf("%s: ite-ctrl result present = %v, want %v", model, key, want)
		}
	}
}
