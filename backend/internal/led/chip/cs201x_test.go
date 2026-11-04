package chip

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
	cs "github.com/ugreen-nas-control/ugreen-nas-control/internal/hw/cs201x"
)

// fakeCSChip is the Super-I/O window and the EC register file of a CS201x; it logs every EC write.
type fakeCSChip struct {
	mu     sync.Mutex
	idx    byte
	active byte
	base   uint16
	ec     map[uint32]byte
	addr   [4]byte
	writes []uint32 // register of every EC data write, in order
	cfg    int      // writes to the Super-I/O data port
}

func (f *fakeCSChip) reg() uint32 {
	return uint32(f.addr[0]) | uint32(f.addr[1])<<8 | uint32(f.addr[2])<<16 | uint32(f.addr[3])<<24
}

func (f *fakeCSChip) In(port uint16) (byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch port {
	case 0x2f:
		switch f.idx {
		case 0x20:
			return 0x20, nil
		case 0x21:
			return 0x11, nil
		case 0x30:
			return f.active, nil
		case 0x60:
			return byte(f.base >> 8), nil
		case 0x61:
			return byte(f.base), nil
		}
		return 0, nil
	case f.base + cs.EcData:
		return f.ec[f.reg()], nil
	}
	return 0xff, nil
}

func (f *fakeCSChip) Out(port uint16, v byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case port == 0x2e:
		f.idx = v
	case port == 0x2f:
		f.cfg++
		if f.idx == 0x30 {
			f.active = v
		}
	case port >= f.base+cs.EcAddr && port < f.base+cs.EcAddr+4:
		f.addr[port-f.base-cs.EcAddr] = v
	case port == f.base+cs.EcData:
		f.ec[f.reg()] = v
		f.writes = append(f.writes, f.reg())
	}
	return nil
}

func (f *fakeCSChip) Close() error { return nil }

func (f *fakeCSChip) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

// csRig installs a fake chip, temp files for the opt-in, the snapshot, the lock and the module list, and the layout.
func csRig(t *testing.T, optIn bool) *fakeCSChip {
	t.Helper()
	dir := t.TempDir()
	f := &fakeCSChip{active: 1, base: 0x200, ec: map[uint32]byte{
		regArgbFlow:       0,
		regArgbMode:       1, // power on, the others off (UGREEN's init, leds-sio-201x.c:420-444)
		regArgbBright:     255,
		regArgbBright + 1: 255, regArgbBright + 2: 255, regArgbBright + 3: 255,
		regArgbColor + 1: 255, regArgbColor + 2: 255, regArgbColor: 255, // power white
	}}
	oldPorts, oldPaths, oldLayout := openCSPorts, csPaths, getLayout()
	openCSPorts = func() (hw.Ports, error) { return f, nil }
	csPaths.config = filepath.Join(dir, "leds.cfg")
	csPaths.snapshot = filepath.Join(dir, "snapshot")
	csPaths.modules = filepath.Join(dir, "modules")
	restoreLock := cs.SetLockPath(filepath.Join(dir, "lock"))
	SetLayout(layoutCS201x)
	t.Cleanup(func() {
		openCSPorts, csPaths = oldPorts, oldPaths
		restoreLock()
		SetLayout(oldLayout)
	})
	if optIn {
		csWrite(t, csPaths.config, "night_enabled=\"0\"\ncs201x_leds=\"1\"\n")
	}
	return f
}

func csWrite(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func run(args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	rc := Run(args, &out, &errw)
	return rc, out.String(), errw.String()
}

func TestCS201xLayoutFromProduct(t *testing.T) {
	for product, want := range map[string]string{"DXP2800 GT": layoutCS201x, "DXP2800": layoutDXP2800, "DXP4800 GT": "", "DXP2800S": ""} {
		if got := detectLayout(product); got != want {
			t.Errorf("%q: %q, want %q", product, got, want)
		}
	}
}

func TestCS201xOffByDefault(t *testing.T) {
	f := csRig(t, false)
	for _, args := range [][]string{{"get", "power"}, {"set", "power", "color", "1", "2", "3"}, {"probe"}} {
		rc, _, stderr := run(args...)
		if rc == 0 || !strings.Contains(stderr, "cs201x_leds") {
			t.Errorf("%v: rc %d stderr %q", args, rc, stderr)
		}
	}
	if len(f.writes) != 0 || f.cfg != 0 {
		t.Fatalf("the chip was touched while off: %d EC writes, %d config writes", len(f.writes), f.cfg)
	}
	if _, err := os.Stat(csPaths.snapshot); err == nil {
		t.Fatal("snapshot written while off")
	}
}

func TestCS201xOptInLastLineWins(t *testing.T) {
	csRig(t, true)
	csWrite(t, csPaths.config, "cs201x_leds=\"1\"\ncs201x_leds=\"0\"\n")
	if cs201xOptIn() {
		t.Fatal("the last line is 0")
	}
	csWrite(t, csPaths.config, "cs201x_leds=\"1\"\n")
	if !cs201xOptIn() {
		t.Fatal("opted in")
	}
}

func TestCS201xKernelModuleBlocks(t *testing.T) {
	f := csRig(t, true)
	csWrite(t, csPaths.modules, "nfsd 1 0 - Live\nleds_sio_201x 16384 0 - Live 0x0\n")
	if rc, _, stderr := run("set", "power", "on"); rc == 0 || !strings.Contains(stderr, "leds_sio_201x") {
		t.Fatalf("rc %d stderr %q", rc, stderr)
	}
	if len(f.writes) != 0 {
		t.Fatal("wrote while a kernel driver owns the chip")
	}
}

func TestCS201xColorBlinkBreathRegisters(t *testing.T) {
	f := csRig(t, true)
	if rc, _, e := run("set", "disk1", "color", "10", "20", "30"); rc != 0 {
		t.Fatal(e)
	}
	// disk1 is channel 2: colour bytes B, R, G at 0x20005444 + 4*2 (ug_201x.c:377-386).
	if f.ec[regArgbColor+8] != 30 || f.ec[regArgbColor+9] != 10 || f.ec[regArgbColor+10] != 20 {
		t.Fatalf("colour bytes B/R/G: %d %d %d", f.ec[regArgbColor+8], f.ec[regArgbColor+9], f.ec[regArgbColor+10])
	}
	if f.ec[regArgbFlow] != 1 {
		t.Fatalf("flow control %d, want 1 before the first write", f.ec[regArgbFlow])
	}
	if rc, _, e := run("set", "disk1", "blink", "200", "800"); rc != 0 {
		t.Fatal(e)
	}
	// cycle 1000 ms = 20 units, on 200 ms = 4 units, mode 2.
	if f.ec[regArgbPeriod+2] != 20 || f.ec[regArgbDuty+2] != 4 || f.ec[regArgbMode+2] != 2 {
		t.Fatalf("blink: period %d duty %d mode %d", f.ec[regArgbPeriod+2], f.ec[regArgbDuty+2], f.ec[regArgbMode+2])
	}
	if rc, _, e := run("set", "disk1", "breath", "1000", "500"); rc != 0 {
		t.Fatal(e)
	}
	if f.ec[regArgbPeriod+2] != 30 || f.ec[regArgbDuty+2] != 20 || f.ec[regArgbMode+2] != 3 {
		t.Fatalf("breath: period %d duty %d mode %d", f.ec[regArgbPeriod+2], f.ec[regArgbDuty+2], f.ec[regArgbMode+2])
	}
	if rc, _, e := run("set", "disk1", "brightness", "99"); rc != 0 || f.ec[regArgbBright+2] != 99 {
		t.Fatalf("brightness %d %s", f.ec[regArgbBright+2], e)
	}
	run("set", "disk1", "off")
	if f.ec[regArgbMode+2] != 0 {
		t.Fatalf("off: mode %d", f.ec[regArgbMode+2])
	}
	run("set", "disk1", "on")
	if f.ec[regArgbMode+2] != 1 {
		t.Fatalf("on: mode %d", f.ec[regArgbMode+2])
	}
	// The mode is the last register of a write: a channel never runs a new mode with old timing.
	run("set", "disk2", "blink", "300", "300")
	if n := len(f.writes); f.writes[n-1] != regArgbMode+3 {
		t.Fatalf("last write %#x, want the mode register", f.writes[n-1])
	}
}

func TestCS201xBlinkClamps(t *testing.T) {
	var c csChannel
	// 100+100 ms (the minimum blink) is 4 units; a cycle beyond 255 units stays at 255.
	_ = applyCommand(&c, cmdBlink, periodParams(cmdBlink, 100, 100))
	if c.Period != 4 || c.Duty != 2 {
		t.Fatalf("minimum blink: %d %d", c.Period, c.Duty)
	}
	_ = applyCommand(&c, cmdBlink, periodParams(cmdBlink, 20000, 20000))
	if c.Period != 255 || c.Duty != 255 {
		t.Fatalf("long blink: %d %d", c.Period, c.Duty)
	}
}

func TestCS201xOnlyFourLEDs(t *testing.T) {
	f := csRig(t, true)
	if rc, _, stderr := run("set", "disk3", "on"); rc == 0 || !strings.Contains(stderr, "unknown LED") {
		t.Fatalf("rc %d %q", rc, stderr)
	}
	if rc, _, stderr := run("effect", "rainbow", "all"); rc == 0 || !strings.Contains(stderr, "DXP2800 GT") {
		t.Fatalf("effect rc %d %q", rc, stderr)
	}
	if len(f.writes) != 0 {
		t.Fatal("rejected commands wrote")
	}
}

func TestCS201xStatusAndProbe(t *testing.T) {
	f := csRig(t, true)
	f.ec[regArgbMode+1], f.ec[regArgbPeriod+1], f.ec[regArgbDuty+1] = 2, 20, 4
	f.ec[regArgbColor+4+1], f.ec[regArgbColor+4+2], f.ec[regArgbColor+4] = 1, 2, 3 // R, G, B of channel 1
	rc, out, e := run("--plain", "get", "netdev")
	if rc != 0 || strings.TrimSpace(out) != "netdev blink 255 1 2 3 200 800" {
		t.Fatalf("rc %d out %q err %q", rc, out, e)
	}
	rc, out, _ = run("probe")
	if rc != 0 || !strings.Contains(out, `"bus": "/dev/port"`) || !strings.Contains(out, "disk2") || strings.Contains(out, "disk3") {
		t.Fatalf("probe %d %q", rc, out)
	}
	if rc, out, _ := run("--plain", "get", "all"); rc != 0 || strings.Count(out, "\n") != 4 {
		t.Fatalf("get all: %q", out)
	}
	if len(f.writes) != 0 {
		t.Fatal("reads wrote to the EC")
	}
}

func TestCS201xHostInterfaceOffIsAnErrorForReads(t *testing.T) {
	f := csRig(t, true)
	f.active = 0
	if rc, _, _ := run("get", "power"); rc == 0 {
		t.Fatal("read with the host interface off must fail")
	}
	if f.active != 0 {
		t.Fatal("a read switched the host interface on")
	}
	if rc, _, e := run("set", "power", "on"); rc != 0 || f.active != 1 {
		t.Fatalf("write: rc %d active %d %s", rc, f.active, e)
	}
}

func TestCS201xDumpWritesNothing(t *testing.T) {
	f := csRig(t, false) // the dump is not gated by the opt-in
	f.ec[regArgbMode+3], f.ec[regArgbPeriod+3], f.ec[regArgbDuty+3] = 3, 30, 20
	rc, out, e := run("cs201x")
	if rc != 0 {
		t.Fatal(e)
	}
	if len(f.writes) != 0 || f.cfg != 1 { // only the logical device number goes to the data port
		t.Fatalf("dump wrote: %d EC writes, %d config writes", len(f.writes), f.cfg)
	}
	var d csDump
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}
	if d.ChipID != "0x2011" || d.HostEC != "active" || len(d.Channels) != 4 || d.OptIn || d.Snapshot {
		t.Fatalf("%+v", d)
	}
	p, g := d.Channels[0], d.Channels[3]
	if !p.Enabled || p.Mode != "on" || p.Color != "255 255 255" || p.Brightness != 255 {
		t.Fatalf("power %+v", p)
	}
	if g.Mode != "breath" || g.PeriodMs != 1500 || g.OnMs != 1000 {
		t.Fatalf("disk2 %+v", g)
	}
}

func TestCS201xDumpHostOff(t *testing.T) {
	f := csRig(t, false)
	f.active = 0
	rc, out, _ := run("cs201x")
	if rc != 0 || !strings.Contains(out, "not active") || len(f.writes) != 0 || f.active != 0 {
		t.Fatalf("rc %d out %q active %d", rc, out, f.active)
	}
}

func TestCS201xSnapshotOnceAndRestore(t *testing.T) {
	f := csRig(t, true)
	before := map[uint32]byte{}
	for k, v := range f.ec {
		before[k] = v
	}
	run("set", "power", "color", "255", "0", "0")
	run("set", "disk1", "blink", "500", "500")
	run("set", "power", "off")
	if _, err := os.Stat(csPaths.snapshot); err != nil {
		t.Fatal("no snapshot after the first write")
	}
	var s csSnapshot
	data, _ := os.ReadFile(csPaths.snapshot)
	if err := json.Unmarshal(data, &s); err != nil || s.Flow != 0 || s.Channels[0].Mode != 1 || s.Channels[0].R != 255 || s.Channels[0].G != 255 {
		t.Fatalf("snapshot %+v %v: it must hold the state before the first write", s, err)
	}
	rc, out, e := run("cs201x", "restore")
	if rc != 0 || !strings.Contains(out, `"restored": true`) {
		t.Fatalf("rc %d %q %q", rc, out, e)
	}
	for k, v := range before {
		if f.ec[k] != v {
			t.Errorf("register %#x = %d after restore, was %d", k, f.ec[k], v)
		}
	}
	if _, err := os.Stat(csPaths.snapshot); err == nil {
		t.Fatal("snapshot not removed")
	}
	// Nothing saved, nothing written.
	n := len(f.writes)
	if rc, out, _ := run("cs201x", "restore"); rc != 0 || !strings.Contains(out, `"restored": false`) || len(f.writes) != n {
		t.Fatalf("second restore: rc %d %q", rc, out)
	}
}

func TestCS201xExistingSnapshotIsKept(t *testing.T) {
	csRig(t, true)
	const saved = `{"flow":0,"channels":[{},{},{},{}]}`
	csWrite(t, csPaths.snapshot, saved)
	run("set", "power", "on")
	if data, _ := os.ReadFile(csPaths.snapshot); string(data) != saved {
		t.Fatalf("snapshot overwritten: %s", data)
	}
}

// The LED backend takes the lock the fan tool takes: a write waits while a fan access holds it.
func TestCS201xWaitsForTheFanLock(t *testing.T) {
	f := csRig(t, true)
	released := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = cs.WithLock(func() error { close(held); <-released; return nil })
	}()
	<-held
	done := make(chan int, 1)
	go func() { rc, _, _ := run("set", "power", "color", "1", "2", "3"); done <- rc }()
	select {
	case <-done:
		t.Fatal("the LED write did not wait for the lock")
	case <-time.After(200 * time.Millisecond):
	}
	if f.count() != 0 {
		t.Fatal("wrote under a lock held by another")
	}
	close(released)
	if rc := <-done; rc != 0 {
		t.Fatalf("rc %d", rc)
	}
}

func TestCS201xRestoreKeepsSnapshotWhenModuleOwnsLEDs(t *testing.T) {
	f := csRig(t, true)
	const saved = `{"flow":0,"channels":[{},{},{},{}]}`
	csWrite(t, csPaths.snapshot, saved)
	csWrite(t, csPaths.modules, "ug_201x 16384 0 - Live 0x0\n")
	rc, _, e := run("cs201x", "restore")
	if rc == 0 || !strings.Contains(e, "ug_201x") {
		t.Fatalf("rc %d %q", rc, e)
	}
	if f.count() != 0 {
		t.Fatal("wrote while a kernel module owns the LEDs")
	}
	if data, _ := os.ReadFile(csPaths.snapshot); string(data) != saved {
		t.Fatalf("snapshot lost: %q", data)
	}
}

func TestCS201xRestoreKeepsUnreadableSnapshot(t *testing.T) {
	for name, saved := range map[string]string{
		"garbage":        "not json",
		"wrong channels": `{"flow":0,"channels":[{}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := csRig(t, true)
			csWrite(t, csPaths.snapshot, saved)
			rc, _, e := run("cs201x", "restore")
			if rc == 0 || !strings.Contains(e, "unreadable") {
				t.Fatalf("rc %d %q", rc, e)
			}
			if f.count() != 0 {
				t.Fatal("wrote from an unreadable snapshot")
			}
			if data, _ := os.ReadFile(csPaths.snapshot); string(data) != saved {
				t.Fatalf("snapshot lost: %q", data)
			}
		})
	}
}
