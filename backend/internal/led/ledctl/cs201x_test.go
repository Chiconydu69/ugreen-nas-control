package ledctl

import (
	"strings"
	"testing"
)

func TestCS201xLEDsOfTheGT(t *testing.T) {
	r := newRig(t, "DXP2800 GT")
	if got := strings.Join(r.c.leds, " "); got != "power netdev disk1 disk2" {
		t.Fatalf("leds %q, UGREEN has power, network_stat, disk1, disk2 (leds-sio-201x.c:93-98)", got)
	}
}

// No SMBus driver is loaded or waited for: the LEDs sit in the embedded controller.
func TestCS201xNoSMBusPreflight(t *testing.T) {
	r := newRig(t, "DXP2800 GT")
	r.c.Env.ToolFromEnv = false
	if rc := r.ctl("get", "power"); rc != 0 {
		t.Fatalf("rc %d err %q", rc, r.err.String())
	}
	if r.run.called("modprobe") {
		t.Fatalf("modprobe ran: %v", r.run.calls)
	}
	r2 := newRig(t, "DXP4800 Plus")
	r2.c.Env.ToolFromEnv = false
	r2.ctl("get", "power")
	if !r2.run.called("modprobe") {
		t.Fatal("other models still load the SMBus driver")
	}
}

func TestCS201xDumpCommandPassesThrough(t *testing.T) {
	r := newRig(t, "DXP2800 GT")
	r.c.Env.ToolFromEnv = false
	r.ctl("cs201x")
	r.ctl("cs201x", "restore")
	calls := strings.Join(r.chip.callList(), "|")
	if calls != "cs201x|cs201x restore" || r.run.called("modprobe") {
		t.Fatalf("calls %q", calls)
	}
}

func TestCS201xRestoredOnStop(t *testing.T) {
	r := newRig(t, "DXP2800 GT")
	r.config(`mode="manual"` + "\n" + `brightness_power="100"` + "\n")
	if rc := r.ctl("daemon", "stop"); rc != 0 {
		t.Fatal(rc)
	}
	calls := r.chip.callList()
	if len(calls) != 1 || calls[0] != "cs201x restore" {
		t.Fatalf("calls %v: stop must restore the snapshot and not apply the saved settings", calls)
	}
}

func TestOtherModelsDoNotRestoreOnStop(t *testing.T) {
	r := newRig(t, "DXP4800 Plus")
	r.ctl("daemon", "stop")
	for _, c := range r.chip.callList() {
		if strings.HasPrefix(c, "cs201x") {
			t.Fatalf("call %q on a model without a CS201x", c)
		}
	}
}
