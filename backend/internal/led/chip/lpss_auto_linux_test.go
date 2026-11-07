//go:build linux

package chip

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// autoBox is a fake SUB2 board for findBus: leds.cfg, an I801 adapter (i801 "nak" answers ENXIO at 0x3a, "ack" has
// the device, "" has no I801 at all), the LPSS sysfs and the controller registers.
type autoBox struct {
	t     *testing.T
	f     *fakeDW
	asked int // times the I801 was asked for 0x3a
}

func newAutoBox(t *testing.T, cfg, i801 string, o sysOpts) *autoBox {
	t.Helper()
	b := &autoBox{t: t, f: newFakeDW()}
	b.f.resp = []byte{0xb2, 0xc5}

	cfgPath := filepath.Join(t.TempDir(), "leds.cfg")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	oldCS, oldSys, oldDev, oldAns, oldState, oldLock := csPaths.config, i2cSysBus, i2cDevDir, i801Answers, lpssState, lockFile
	t.Cleanup(func() {
		csPaths.config, i2cSysBus, i2cDevDir, i801Answers, lpssState, lockFile = oldCS, oldSys, oldDev, oldAns, oldState, oldLock
		lpssRefusedMem.Store(false)
	})
	csPaths.config = cfgPath
	lpssState = filepath.Join(t.TempDir(), "state")
	lockFile = filepath.Join(t.TempDir(), "lock")
	lpssRefusedMem.Store(false)

	i2cSysBus, i2cDevDir = t.TempDir(), t.TempDir()
	if i801 != "" {
		if err := os.MkdirAll(filepath.Join(i2cSysBus, "i2c-0"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(i2cSysBus, "i2c-0/name"), []byte("SMBus I801 adapter at f040\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(i2cDevDir, "i2c-0"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	i801Answers = func(string) bool { b.asked++; return i801 == "ack" }

	fakeSys(t, o)
	fakePM(t, true)
	fakeMem(t, b.f)
	return b
}

func (b *autoBox) untouched() bool { return b.f.roMaps == 0 && b.f.rwMaps == 0 }

func TestLPSSDefaultOn(t *testing.T) {
	for _, tc := range []struct{ name, cfg, i801 string }{
		{"no key, I801 NAK", "", "nak"},
		{"no key, no I801", "", ""},
		{"key 1 is the same", "lpss_leds=\"1\"\n", "nak"},
		{"other keys", "led_enabled=\"1\"\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newAutoBox(t, tc.cfg, tc.i801, sysOpts{})
			path, err := findBus()
			if err != nil || path != lpssPath {
				t.Fatalf("findBus = %q, %v; want %s", path, err, lpssPath)
			}
			bus, err := openBusAt("", controllerAddr, false)
			if err != nil {
				t.Fatal(err)
			}
			defer bus.close()
			if bus.path != lpssPath || bus.dw == nil {
				t.Errorf("bus %+v", bus)
			}
			if b.f.rwMaps != 1 {
				t.Errorf("rw maps %d, want 1 (the bring-up ran)", b.f.rwMaps)
			}
			if got := strings.TrimSpace(string(mustRead(lpssState))); got != lpssStateOK {
				t.Errorf("state %q, want %q", got, lpssStateOK)
			}
		})
	}
}

func TestLPSSOptOut(t *testing.T) {
	// With an I801: the SMBus path, LPSS never touched.
	b := newAutoBox(t, "lpss_leds=\"0\"\n", "nak", sysOpts{})
	if path, err := findBus(); err != nil || path != i2cDevDir+"/i2c-0" {
		t.Fatalf("findBus = %q, %v", path, err)
	}
	if !b.untouched() {
		t.Error("the LPSS controller was mapped")
	}
	// Without: the old error, not the LPSS path.
	b = newAutoBox(t, "lpss_leds=\"1\"\nlpss_leds=\"0\"\n", "", sysOpts{}) // the last line wins
	if path, err := findBus(); err == nil || !strings.Contains(err.Error(), "no SMBus I801") {
		t.Fatalf("findBus = %q, %v", path, err)
	}
	// An explicit --bus naming the LPSS path is refused too.
	if _, err := openBusAt(lpssPath, controllerAddr, false); err != errLPSSOff {
		t.Errorf("openBusAt(lpssPath) = %v, want errLPSSOff", err)
	}
	if !b.untouched() {
		t.Error("the LPSS controller was mapped")
	}
}

func TestLPSSNotUsedWhenI801Answers(t *testing.T) {
	b := newAutoBox(t, "", "ack", sysOpts{})
	if path, err := findBus(); err != nil || path != i2cDevDir+"/i2c-0" {
		t.Fatalf("findBus = %q, %v", path, err)
	}
	if b.asked != 1 {
		t.Errorf("I801 asked %d times", b.asked)
	}
	if !b.untouched() || sysLine(filepath.Join(lpssSys, "bus/pci/devices/0000:00:15.0/power/control")) != "auto" {
		t.Error("the LPSS controller or its power/control was touched")
	}
}

func TestLPSSNoHT32(t *testing.T) {
	newAutoBox(t, "", "nak", sysOpts{noMCU: true})
	if path, err := findBus(); err != nil || path != i2cDevDir+"/i2c-0" {
		t.Fatalf("with I801: findBus = %q, %v", path, err)
	}
	b := newAutoBox(t, "", "", sysOpts{noMCU: true})
	if path, err := findBus(); err == nil || path == lpssPath {
		t.Fatalf("without I801: findBus = %q, %v", path, err)
	}
	if !b.untouched() {
		t.Error("the LPSS controller was mapped")
	}
}

// A bound i2c_designware child means another driver owns the controller: refused, old behaviour, nothing written.
func TestLPSSBoundChildRefused(t *testing.T) {
	o := bound()
	o.children = map[string]string{"i2c_designware.0": "i2c_designware"}
	b := newAutoBox(t, "", "", o)
	if _, err := openBusAt("", controllerAddr, false); err == nil || !strings.Contains(err.Error(), "no SMBus I801") {
		t.Fatalf("openBusAt = %v, want the old no-adapter error", err)
	}
	if !b.untouched() {
		t.Error("the LPSS controller was mapped")
	}
	if got := sysLine(filepath.Join(lpssSys, "bus/pci/devices/0000:00:15.0/power/control")); got != "auto" {
		t.Errorf("power/control %q, want auto", got)
	}
	if r := lpssRefusal(); !strings.Contains(r, "child-driver") {
		t.Errorf("refusal %q lacks the rule", r)
	}
}

// Not an HT32 board is not a refusal worth caching, and a bound child on a box with an I801 falls back to it.
func TestLPSSNegativeCache(t *testing.T) {
	// Passes every read check, bring-up runs, chip id is wrong: refused once, never again this boot.
	o := bound()
	b := newAutoBox(t, "", "nak", o)
	b.f.resp = []byte{0x34, 0x12}
	pm := fakePM(t, true)

	if path, _ := findBus(); path != lpssPath {
		t.Fatalf("first findBus = %q", path)
	}
	_, err := openBusAt("", controllerAddr, false) // refused, then the old path (the stand-in file takes no ioctl)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(lpssRefusal(), "chip id 0x1234") {
		t.Errorf("refusal %q", lpssRefusal())
	}
	writes, maps := len(*pm), b.f.roMaps+b.f.rwMaps
	if writes == 0 || maps == 0 {
		t.Fatalf("the first run touched nothing (%d, %d)", writes, maps)
	}

	for range 3 {
		if path, err := findBus(); err != nil || path != i2cDevDir+"/i2c-0" {
			t.Fatalf("findBus after the refusal = %q, %v", path, err)
		}
		_, _ = openBusAt("", controllerAddr, false)
	}
	if len(*pm) != writes || b.f.roMaps+b.f.rwMaps != maps {
		t.Errorf("later commands touched the controller again: power writes %d->%d, maps %d->%d", writes, len(*pm), maps, b.f.roMaps+b.f.rwMaps)
	}

	// The memory copy alone holds when the state file cannot be written.
	lpssState = filepath.Join(t.TempDir(), "no/such/dir/state")
	if path, _ := findBus(); path != i2cDevDir+"/i2c-0" {
		t.Errorf("findBus without a state file = %q", path)
	}
}

// One transient failure after the controller answered in this boot does not switch the path off.
func TestLPSSNoCacheAfterSuccess(t *testing.T) {
	newAutoBox(t, "", "nak", sysOpts{})
	lpssNoteOK()
	if lpssNoteRefused(os.ErrDeadlineExceeded) {
		t.Error("a refusal was cached after an ok")
	}
	if path, _ := findBus(); path != lpssPath {
		t.Errorf("findBus = %q, want %s", path, lpssPath)
	}
}
