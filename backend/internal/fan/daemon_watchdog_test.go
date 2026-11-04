package fan

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

// runECDaemon runs the daemon on a fake EC with a clock that moves 10 s per reading, until it has fed or skipped for a
// while, then stops it. model is what daemonModel reports.
func runECDaemon(t *testing.T, f *fakeEC, cfg, model string, rivals []conflict.Conflict, pause bool, s Sensors) {
	t.Helper()
	dir := t.TempDir()
	isolate(t, model) // the petter checks the DMI model
	set := func(p *string, name string) {
		old := *p
		*p = filepath.Join(dir, name)
		t.Cleanup(func() { *p = old })
	}
	set(&configPath, "fan.cfg")
	set(&statePath, "state.json")
	defer func(old string) { historyPath = old }(historyPath)
	historyPath = ""
	set(&biosPath, "bios")
	set(&lockPath, "lock")
	set(&calibPath, "fan_rpm.json")
	set(&calibPausePath, "pause")
	set(&watchdogStatePath, "wdt.json")
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if pause {
		if err := os.WriteFile(calibPausePath, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	chip, err := detectFans(f, "DXP4800 GT")
	if err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	oldTick, oldRead, oldNow, oldDet, oldModel := tick, readSensors, now, detectConflicts, daemonModel
	tick = 5 * time.Millisecond
	readSensors = func() Sensors { return s }
	now = func() time.Time { return mon3am.Add(time.Duration(clock.Add(1)) * 10 * time.Second) }
	detectConflicts = func() []conflict.Conflict { return rivals }
	daemonModel = func() string { return model }
	t.Cleanup(func() {
		tick, readSensors, now, detectConflicts, daemonModel = oldTick, oldRead, oldNow, oldDet, oldModel
	})

	done := make(chan error, 1)
	go func() { done <- runDaemon(chip) }()
	for clock.Load() < 40 { // the daemon reads the clock at least once per tick
		time.Sleep(5 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func wdtWrites(f *fakeEC) (pets, enables int) {
	for _, r := range f.writes {
		switch r {
		case ecRegWdtHi:
			pets++
		case ecRegWdtEnable:
			enables++
		}
	}
	return
}

func armedEC() *fakeEC {
	f := newFakeEC()
	f.regs[ecRegWdtEnable] = 1
	return f
}

// The petter feeds at its interval (timeout 300 s -> every 60 s) and not more often, 6 ticks of 10 s apart per feed.
func TestPetWatchdogRateLimited(t *testing.T) {
	isolate(t, "DXP4800 GT")
	f := armedEC()
	chip, err := detectFans(f, "DXP4800 GT")
	if err != nil {
		t.Fatal(err)
	}
	p := &Petter{}
	t0 := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	for i := range 13 { // 0 s to 120 s in 10 s steps
		petWatchdog(p, chip, t0.Add(time.Duration(i)*10*time.Second), t.Logf)
	}
	if pets, _ := wdtWrites(f); pets != 3 { // at 0, 60, 120 s
		t.Fatalf("%d feeds, want 3", pets)
	}
}

func TestDaemonPetsArmedWatchdogOnEveryPath(t *testing.T) {
	hot := sens(95, 40)
	for _, tc := range []struct {
		name   string
		cfg    string
		rivals []conflict.Conflict
		pause  bool
		sens   Sensors
	}{
		{"normal", v2Base, nil, false, sens(45, 40)},
		{"yield to a rival", v2Base, rival(), false, sens(45, 40)},
		{"bios mode", "mode=\"bios\"\n", nil, false, sens(45, 40)},
		{"calibration pause", v2Base, nil, true, sens(45, 40)},
		{"emergency", v2Base, nil, false, hot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := armedEC()
			runECDaemon(t, f, tc.cfg, "DXP4800 GT", tc.rivals, tc.pause, tc.sens)
			pets, _ := wdtWrites(f)
			// 40+ readings of 10 s: at least one feed per 60 s of the fake clock, and none more often than that.
			if pets < 2 || pets > 12 {
				t.Fatalf("%d feeds", pets)
			}
			if tc.rivals == nil && f.regs[ecRegWdtEnable] != 0 { // a rival keeps the fans on stop, so the BIOS state stays
				t.Fatalf("watchdog still armed after stop")
			}
		})
	}
}

func TestDaemonLeavesDisarmedWatchdogAlone(t *testing.T) {
	f := newFakeEC() // 0x98 = 0
	runECDaemon(t, f, v2Base, "DXP4800 GT", nil, false, sens(45, 40))
	for _, r := range f.writes {
		if r == ecRegWdtHi || r == ecRegWdtLo {
			t.Fatalf("timeout register written although the watchdog is not armed (writes %x)", f.writes)
		}
	}
}

// Nothing writes 0x98 = 1: the fake panics on it (ec_test.go), and the register never ends up non-zero from a write.
func TestDaemonNeverArmsWatchdog(t *testing.T) {
	f := newFakeEC()
	runECDaemon(t, f, v2Base, "DXP4800 GT", nil, false, sens(45, 40))
	if f.regs[ecRegWdtEnable] != 0 {
		t.Fatal("0x98 set")
	}
}
