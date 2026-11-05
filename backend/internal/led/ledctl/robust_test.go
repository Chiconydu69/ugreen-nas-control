package ledctl

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
)

// startDaemon runs the status daemon on a fast tick; stop() cancels it like a SIGTERM and returns its exit code.
func startDaemon(t *testing.T, r *rig) (stop func() int) {
	t.Helper()
	r.c.Tick = 2 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	var once sync.Once
	go func() { done <- r.c.daemonRun(ctx) }()

	stopFn := func() int {
		cancel()
		select {
		case rc := <-done:
			return rc
		case <-time.After(5 * time.Second):
			t.Error("daemon did not stop")
			return -1
		}
	}
	t.Cleanup(func() { once.Do(func() { stopFn() }) })

	return func() (rc int) {
		once.Do(func() { rc = stopFn() })
		return rc
	}
}

func waitFor(t *testing.T, r *rig, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout: %s\ncalls: %v", what, r.chip.callList())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func countCalls(calls []string, prefix string) (n int) {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeRunner) logged(substr string) (n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, "logger ") && strings.Contains(c, substr) {
			n++
		}
	}
	return n
}

// The SUB2 board: the controller answers a detect but rejects every command. The daemon says so once, stops writing
// and asks again only at growing intervals; when the controller answers again it starts over.
func TestDaemonBacksOffWhenControllerRejects(t *testing.T) {
	r := newRig(t, "DXP2800")
	writeSystem(r)
	r.chip.setRC(chip.ExitRejected)
	r.c.RejectBackoff, r.c.RejectBackoffMax = 40*time.Millisecond, 80*time.Millisecond
	stop := startDaemon(t, r)

	waitFor(t, r, "rejection logged", func() bool { return r.run.logged("rejects every read and write") == 1 })
	time.Sleep(500 * time.Millisecond)

	calls := r.chip.callList()
	if sets := countCalls(calls, "set "); sets > 1 {
		t.Errorf("%d writes after the first rejection, want 1: %v", sets, calls)
	}
	// 500 ms at 40, 80, 80 ... ms: a handful of probes, not one per 2 ms tick.
	if probes := countCalls(calls, "probe"); probes < 2 || probes > 12 {
		t.Errorf("%d probes in 500 ms with a 40..80 ms back-off", probes)
	}
	if n := r.run.logged("rejects every read and write"); n != 1 {
		t.Errorf("message logged %d times, want once", n)
	}

	// The controller comes back: the LEDs are written again from scratch.
	r.chip.setRC(0)
	waitFor(t, r, "resumed", func() bool { return r.chip.get("disk1")[1] == "255" && r.run.logged("answers again") == 1 })

	if rc := stop(); rc != 0 {
		t.Errorf("rc = %d", rc)
	}
}

// ugreen-led-ctl status names the state in one field next to i2c_probe, and asks the chip tool only once.
func TestStatusReportsRejectedController(t *testing.T) {
	r := newRig(t, "DXP2800")
	r.chip.setRC(chip.ExitRejected)

	if rc := r.ctl("status"); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	out := r.out.String()
	for _, want := range []string{`"i2c_probe": "rejected"`, `"i2c_error": "` + chip.RejectedMessage + `"`, `"i2c_bus": null`} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %s:\n%s", want, out)
		}
	}
	if n := countCalls(r.chip.callList(), "probe"); n != 1 {
		t.Errorf("probe asked %d times, want 1", n)
	}

	// A command passes the chip tool's exit code on.
	if rc := r.ctl("on", "power"); rc != chip.ExitRejected {
		t.Errorf("on power: rc = %d, want %d", rc, chip.ExitRejected)
	}
}

func diskINI(spundown, temp string) string {
	return fmt.Sprintf("[disk1]\nname=\"disk1\"\ndevice=\"sda\"\nstatus=\"DISK_OK\"\nspundown=%q\ntemp=%q\nid=\"X_1\"\n[disk2]\nname=\"disk2\"\ndevice=\"sdb\"\nstatus=\"DISK_DSBL\"\n", spundown, temp)
}

// A disk that goes to standby, is read by SMART while down, wakes and sleeps again: the bay is dimmed while down,
// full while awake, never off, never stuck in the other look, and a SMART read alone does not write anything.
func TestStandbyTransitionsAroundSmartRead(t *testing.T) {
	r := newRig(t, "DXP2800")
	writeSystem(r)
	r.config("disk1_color=\"1 2 3\"\nstandby_mode=\"dim\"\nstandby_level=\"20\"\n")
	stop := startDaemon(t, r)

	look := func(mode, brightness string) func() bool {
		return func() bool { s := r.chip.get("disk1"); return s[0] == mode && s[1] == brightness && s[2] == "1" }
	}
	settle := func() { time.Sleep(200 * time.Millisecond) } // several 5 s rounds (50 ms each here)

	waitFor(t, r, "awake", look("on", "255"))

	r.write("disks.ini", diskINI("1", "*"))
	waitFor(t, r, "standby look", look("on", "51"))
	settle()
	before := countCalls(r.chip.callList(), "set ")

	// Unraid reads SMART of the sleeping disk: new smart file, new temperature, still spun down.
	r.write("smart/disk1", "ID# ATTRIBUTE_NAME FLAG VALUE WORST THRESH TYPE UPDATED WHEN_FAILED RAW_VALUE\n  5 Reallocated_Sector_Ct 0x0033 100 100 010 Pre-fail Always - 0\n")
	r.write("disks.ini", diskINI("1", "31"))
	settle()
	if after := countCalls(r.chip.callList(), "set "); after != before {
		t.Errorf("a SMART read of a sleeping disk wrote to the LED: %v", r.chip.callList()[before:])
	}

	// It wakes: full look at once, no stale standby.
	r.write("disks.ini", diskINI("0", "32"))
	waitFor(t, r, "awake again", look("on", "255"))
	settle()

	// And sleeps again: standby look, no stuck blink or full brightness.
	r.write("disks.ini", diskINI("1", "*"))
	waitFor(t, r, "standby again", look("on", "51"))
	settle()

	if rc := stop(); rc != 0 {
		t.Errorf("rc = %d", rc)
	}

	calls := r.chip.callList()
	for _, c := range calls {
		if strings.HasPrefix(c, "set disk1 off") {
			t.Errorf("disk1 went dark during the transitions: %v", calls)
			break
		}
	}
	// Every change of look is written exactly once: awake, standby, awake, standby.
	var brightness []string
	for _, c := range calls {
		if f := strings.Fields(c); len(f) == 4 && f[0] == "set" && f[1] == "disk1" && f[2] == "brightness" {
			brightness = append(brightness, f[3])
		}
	}
	if want := []string{"255", "51", "255", "51"}; !slices.Equal(brightness, want) {
		t.Errorf("brightness writes %v, want %v", brightness, want)
	}
}

// Traffic while the disk wakes (the SMART read) must not leave the bay blinking once it sleeps again.
func TestStandbyAfterWakeEndsActivityBlink(t *testing.T) {
	r := newRig(t, "DXP2800")
	writeSystem(r)
	r.config("standby_mode=\"dim\"\nstandby_level=\"20\"\n")
	stop := startDaemon(t, r)

	waitFor(t, r, "awake", func() bool { return r.chip.get("disk1")[1] == "255" })

	n := 1
	waitFor(t, r, "blinks with traffic", func() bool {
		n++
		r.write("sys/block/sda/stat", fmt.Sprintf("%d 0 0 0 %d 0 0 0\n", n, n))
		return r.chip.get("disk1")[0] == "blink"
	})

	r.write("disks.ini", diskINI("1", "*"))
	waitFor(t, r, "standby look, no blink", func() bool {
		s := r.chip.get("disk1")
		return s[1] == "51" && s[0] == "on"
	})
	time.Sleep(100 * time.Millisecond) // no traffic: nothing blinks it again
	if s := r.chip.get("disk1"); s[0] != "on" || s[1] != "51" {
		t.Errorf("disk1 = %v", s)
	}
	stop()
}

// Stopping the daemon never leaves the network LED in an activity blink: it ends steady, and the saved settings
// (daemon stop) then apply to it like to every other LED.
func TestDaemonStopEndsNetdevActivityBlink(t *testing.T) {
	r := newRig(t, "DXP2800")
	writeSystem(r)
	r.config("netdev_color=\"9 8 7\"\nnetdev_brightness=\"100\"\n")
	stop := startDaemon(t, r)

	waitFor(t, r, "network LED set", func() bool { s := r.chip.get("netdev"); return s[1] == "100" && s[2] == "9" })

	n := 1
	waitFor(t, r, "network blinks with traffic", func() bool {
		n++
		r.write("sys/net/eth0/statistics/rx_bytes", fmt.Sprintf("%d\n", n))
		return r.chip.get("netdev")[0] == "blink"
	})

	if rc := stop(); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if s := r.chip.get("netdev"); s[0] != "on" {
		t.Fatalf("netdev ends %v, want steady on", s)
	}

	// daemon stop: the saved manual look follows.
	r.silentApply(t)
	if s := r.chip.get("netdev"); s[0] != "on" || s[1] != "100" || s[2] != "9" {
		t.Errorf("netdev after the saved settings = %v", s)
	}
}

func (r *rig) silentApply(t *testing.T) {
	t.Helper()
	if rc := r.c.applyConfig(); rc != 0 {
		t.Fatalf("applyConfig rc = %d", rc)
	}
}

// With the network LED on manual the daemon never blinks it, so stopping leaves the saved effect alone.
func TestDaemonStopKeepsManualNetdev(t *testing.T) {
	r := newRig(t, "DXP2800")
	writeSystem(r)
	r.config("netdev_status=\"manual\"\nnetdev_color=\"9 8 7\"\nnetdev_effect=\"breath\"\nnetdev_on=\"1200\"\nnetdev_off=\"900\"\n")
	stop := startDaemon(t, r)

	waitFor(t, r, "manual look applied", func() bool { return r.chip.get("netdev")[0] == "breath" })
	r.write("sys/net/eth0/statistics/rx_bytes", "5000\n")
	time.Sleep(100 * time.Millisecond)
	stop()

	if s := r.chip.get("netdev"); s[0] != "breath" || s[2] != "9" {
		t.Errorf("manual netdev = %v, want the saved breath", s)
	}
}
