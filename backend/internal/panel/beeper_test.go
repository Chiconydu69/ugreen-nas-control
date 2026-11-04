package panel

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

func noLock(fn func() error) error { return fn() }

func testOpeners(ec hw.Ports) openers {
	return openers{
		ports:     func() (hw.Ports, error) { return ec, nil },
		lock:      noLock,
		acpiDir:   "/nonexistent",
		conflicts: func() []conflict.Conflict { return nil },
	}
}

func TestBeeperFor(t *testing.T) {
	for model, kind := range map[string]BeepKind{
		"DXP4800": PCSpeaker, "DXP2800S": PCSpeaker, "DXP4800 Plus": PCSpeaker, "DXP6800 Pro": PCSpeaker,
		"DXP8800 Plus": PCSpeaker, "FORT 6": PCSpeaker, "DX4700": PCSpeaker,
		"iDX6011": ECByte, "iDX6012": ECByte, "iDX6011 Pro": ECByte,
		"DH2600": PadReg, "DXP480T Plus": PadReg,
	} {
		s := BeeperFor(model)
		if s == nil || s.Kind != kind {
			t.Errorf("%q: %+v", model, s)
		}
	}
	for _, m := range []string{"DXP4800 GT", "DXP2800 GT", "DX4600", "DXP6800 Ultra", "iDX6011 Plus", "", "Standard PC"} {
		if s := BeeperFor(m); s != nil {
			t.Errorf("%q: unexpected entry %s", m, s.Name)
		}
	}
}

// The iDX6011 Pro's 0xA5 is its backlight: the table must never point the beeper there.
func TestECRegisters(t *testing.T) {
	if BeeperFor("iDX6011 Pro").ECReg != 0x9E || BeeperFor("iDX6011").ECReg != 0xA5 || BeeperFor("iDX6012").ECReg != 0xA5 {
		t.Fatal("EC beeper registers")
	}
	for _, s := range BeeperSpecs {
		if s.Kind == ECByte && s.ECReg != 0xA5 && s.ECReg != 0x9E {
			t.Errorf("%s: EC register 0x%X", s.Name, s.ECReg)
		}
		if !strings.Contains(s.Cite, "@c75c5ab") {
			t.Errorf("%s: cite", s.Name)
		}
	}
}

func TestSpeaker(t *testing.T) {
	p := newFakePorts()
	p.val[pitGate] = 0x10 // an unrelated bit stays
	s := &speaker{p: p}
	if err := s.On(0); err != nil {
		t.Fatal(err)
	}
	want := [][2]int{{0x43, 0xB6}, {0x42, 0x44}, {0x42, 0x09}, {0x61, 0x13}}
	if !slices.Equal(p.writes, want) {
		t.Errorf("default tone: %x", p.writes)
	}
	p.writes = nil
	if err := s.On(1000); err != nil { // 1193182/1000 = 1193 = 0x4A9
		t.Fatal(err)
	}
	if p.writes[1] != [2]int{0x42, 0xA9} || p.writes[2] != [2]int{0x42, 0x04} {
		t.Errorf("1 kHz: %x", p.writes)
	}
	p.writes = nil
	if err := s.Off(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.writes, [][2]int{{0x61, 0x10}}) {
		t.Errorf("off: %x", p.writes)
	}
	// The whole frequency range fits the 16-bit divisor.
	for _, hz := range []int{MinHz, MaxHz} {
		p.writes = nil
		_ = s.On(hz)
		if d := p.writes[1][1] | p.writes[2][1]<<8; d < 100 || d > 0xFFFF {
			t.Errorf("%d Hz: divisor %d", hz, d)
		}
	}
}

func TestECBeeper(t *testing.T) {
	for _, c := range []struct {
		model string
		reg   byte
		echo2 bool
	}{{"iDX6011", 0xA5, false}, {"iDX6011", 0xA5, true}, {"iDX6012", 0xA5, false}, {"iDX6011 Pro", 0x9E, false}, {"iDX6011 Pro", 0x9E, true}} {
		ec := &fakeEC{echo2: c.echo2}
		b, _, err := BeeperFor(c.model).open(testOpeners(ec))
		if err != nil {
			t.Fatal(err)
		}
		if err := b.On(0); err != nil {
			t.Fatalf("%s on: %v", c.model, err)
		}
		if err := b.Off(); err != nil {
			t.Fatalf("%s off: %v", c.model, err)
		}
		if len(ec.writes) != 2 || ec.writes[0] != [2]byte{c.reg, 1} || ec.writes[1] != [2]byte{c.reg, 0} {
			t.Errorf("%s: writes %v", c.model, ec.writes)
		}
	}
}

func TestECBeeperNeverWritesAnotherRegister(t *testing.T) {
	ec := &fakeEC{}
	b := &ecBeeper{p: ec, reg: BeeperFor("iDX6011 Pro").ECReg, wait: 50 * time.Millisecond, lock: noLock}
	_ = b.On(0)
	_ = b.Off()
	for _, w := range ec.writes {
		if w[0] != 0x9E || w[1] > 1 {
			t.Errorf("write %v", w)
		}
	}
}

func TestECBeeperErrors(t *testing.T) {
	ec := &fakeEC{deaf: true}
	b := &ecBeeper{p: ec, reg: 0xA5, wait: 50 * time.Millisecond, lock: noLock}
	if err := b.On(0); err == nil || !strings.Contains(err.Error(), "reads back") {
		t.Errorf("deaf EC: %v", err)
	}
	if len(ec.writes) != ecTries {
		t.Errorf("tries %d", len(ec.writes))
	}
	ec = &fakeEC{failRead: true}
	b = &ecBeeper{p: ec, reg: 0xA5, wait: 5 * time.Millisecond, lock: noLock}
	if err := b.On(0); !errors.Is(err, errECTimeout) {
		t.Errorf("silent EC: %v", err)
	}
}

func TestPadBeeper(t *testing.T) {
	m := newFakeMem()
	m.words[0xFD6E07E0] = 0x84000000
	b := &padBeeper{m: m, addr: 0xFD6E07E0}
	_ = b.On(0)
	if m.words[0xFD6E07E0] != 0x84000201 { // vendor comment: on
		t.Errorf("on: 0x%X", m.words[0xFD6E07E0])
	}
	_ = b.Off()
	if m.words[0xFD6E07E0] != 0x84000200 { // vendor comment: off
		t.Errorf("off: 0x%X", m.words[0xFD6E07E0])
	}
	m.words[0xFD6E08B0] = 0xFFFFFFFE
	b = &padBeeper{m: m, addr: 0xFD6E08B0}
	_ = b.On(0)
	if got := m.words[0xFD6E08B0]; got != 0xFFFFFAFF { // bits 10..8 = 010, bit 0 set, the rest kept
		t.Errorf("DH2600 on: 0x%X", got)
	}
	if len(m.writes) != 3 {
		t.Errorf("writes %v", m.writes)
	}
}

type countBeeper struct {
	ons, offs int
	onErr     error
}

func (c *countBeeper) On(int) error         { c.ons++; return c.onErr }
func (c *countBeeper) Off() error           { c.offs++; return nil }
func noWait(context.Context, time.Duration) {}

func TestSoundAlwaysSwitchesOff(t *testing.T) {
	c := &countBeeper{}
	if err := Sound(context.Background(), c, 0, 100, noWait); err != nil || c.ons != 1 || c.offs != 1 {
		t.Errorf("normal: %v %+v", err, c)
	}
	c = &countBeeper{onErr: errors.New("boom")}
	if err := Sound(context.Background(), c, 0, 100, noWait); err == nil || c.offs != 1 {
		t.Errorf("on failed: %v %+v", err, c)
	}
	// Interrupted: the wait returns at once and Off still runs.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c = &countBeeper{}
	Sound(ctx, c, 0, MaxMs, func(ctx context.Context, _ time.Duration) {
		select {
		case <-ctx.Done():
		case <-time.After(time.Minute):
			t.Error("did not return on cancel")
		}
	})
	if c.offs != 1 {
		t.Errorf("cancel: %+v", c)
	}
	// A panic in the wait still switches off.
	c = &countBeeper{}
	func() {
		defer func() { _ = recover() }()
		_ = Sound(context.Background(), c, 0, 100, func(context.Context, time.Duration) { panic("x") })
	}()
	if c.offs != 1 {
		t.Errorf("panic: %+v", c)
	}
}

// cliEnv is a fake machine of one model.
type cliEnv struct {
	Env
	ports    *fakePorts
	ec       *fakeEC
	mem      *fakeMem
	memOpens []bool
	waits    []time.Duration
	// lock bookkeeping: held now, number of acquisitions, and whether a wait ran while it was held.
	heldNow, locks int
	heldInWait     bool
	rivals         []conflict.Conflict
}

func (c *cliEnv) lock(fn func() error) error {
	c.locks++
	c.heldNow++
	defer func() { c.heldNow-- }()
	return fn()
}

func newCLI(t *testing.T, model string) *cliEnv {
	dir := t.TempDir()
	if err := writeFile(dir, model); err != nil {
		t.Fatal(err)
	}
	c := &cliEnv{ports: newFakePorts(), ec: &fakeEC{}, mem: newFakeMem()}
	c.ec.held = func() bool { return c.heldNow > 0 }
	c.Env = Env{
		DMIDir: dir,
		OpenPorts: func() (hw.Ports, error) {
			if strings.HasPrefix(model, "iDX") {
				return c.ec, nil
			}
			return c.ports, nil
		},
		Lock:      c.lock,
		ACPIDir:   t.TempDir(),
		Conflicts: func() []conflict.Conflict { return c.rivals },
		OpenMem:   func(w bool) (Mem, error) { c.memOpens = append(c.memOpens, w); return c.mem, nil },
		Wait: func(_ context.Context, d time.Duration) {
			c.waits = append(c.waits, d)
			c.heldInWait = c.heldInWait || c.heldNow > 0
		},
		Now: func() time.Time { return time.Time{} },
	}
	return c
}

func (c *cliEnv) run(args ...string) (int, string) {
	var out, errb bytes.Buffer
	rc := Main(context.Background(), c.Env, args, &out, &errb)
	return rc, out.String() + errb.String()
}

func TestCLIBeepRefusesUnknownModel(t *testing.T) {
	c := newCLI(t, "DXP4800 GT")
	rc, out := c.run("beep")
	if rc != 1 || !strings.Contains(out, "no beeper known") || len(c.ports.writes) != 0 || len(c.memOpens) != 0 {
		t.Errorf("rc %d %q", rc, out)
	}
}

func TestCLIBeepBounds(t *testing.T) {
	c := newCLI(t, "DXP4800")
	for _, args := range [][]string{{"--ms", "2001"}, {"--ms", "0"}, {"--ms", "19"}, {"--freq", "99"}, {"--freq", "8001"}, {"--bogus"}, {"x"}} {
		rc, _ := c.run(append([]string{"beep"}, args...)...)
		if rc != 2 {
			t.Errorf("%v: rc %d", args, rc)
		}
	}
	if len(c.ports.writes) != 0 {
		t.Errorf("writes %v", c.ports.writes)
	}
	rc, out := c.run("beep", "--ms", "2000", "--freq", "440")
	if rc != 0 || len(c.waits) != 1 || c.waits[0] != 2*time.Second {
		t.Errorf("rc %d %q %v", rc, out, c.waits)
	}
	// 440 Hz: 1193182/440 = 2712 = 0xA98; the gate bits end cleared.
	if last := c.ports.writes[len(c.ports.writes)-1]; last != [2]int{0x61, 0} || !c.ports.closed {
		t.Errorf("last write %x closed %v", last, c.ports.closed)
	}
}

func TestCLIBeepDefaultAndFreqRefusedOnFixedTone(t *testing.T) {
	c := newCLI(t, "iDX6011 Pro")
	if rc, out := c.run("beep", "--freq", "500"); rc != 2 || !strings.Contains(out, "fixed tone") || len(c.ec.writes) != 0 {
		t.Errorf("rc %d %q", rc, out)
	}
	if rc, out := c.run("beep"); rc != 0 || c.waits[0] != DefaultMs*time.Millisecond {
		t.Errorf("rc %d %q %v", rc, out, c.waits)
	}
	if !slices.Equal(c.ec.writes, [][2]byte{{0x9E, 1}, {0x9E, 0}}) {
		t.Errorf("EC writes %v", c.ec.writes)
	}
}

func TestCLIBeepPadAndInterrupt(t *testing.T) {
	c := newCLI(t, "DXP480T Plus")
	c.mem.words[0xFD6E07E0] = 0x84000000
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // SIGINT before the beep ends
	var out bytes.Buffer
	if rc := Main(ctx, c.Env, []string{"beep"}, &out, &out); rc != 0 {
		t.Fatalf("rc %d %s", rc, out.String())
	}
	if c.mem.words[0xFD6E07E0] != 0x84000200 || !c.mem.closed || !slices.Equal(c.memOpens, []bool{true}) {
		t.Errorf("word 0x%X closed %v opens %v", c.mem.words[0xFD6E07E0], c.mem.closed, c.memOpens)
	}
}

func TestCLIButtons(t *testing.T) {
	c := newCLI(t, "DXP4800")
	c.mem.words[0xFD6A0980] = 2
	c.mem.words[0xFD6D0B50] = 2
	rc, out := c.run("buttons")
	if rc != 0 || !strings.Contains(out, "power held, reset released") || !strings.Contains(out, "not yet confirmed on hardware") {
		t.Errorf("rc %d %q", rc, out)
	}
	// Read-only open, and nothing written.
	if !slices.Equal(c.memOpens, []bool{false}) || len(c.mem.writes) != 0 {
		t.Errorf("opens %v writes %v", c.memOpens, c.mem.writes)
	}
	c = newCLI(t, "Standard PC")
	if rc, out := c.run("buttons"); rc != 1 || !strings.Contains(out, "no button map") || len(c.memOpens) != 0 {
		t.Errorf("rc %d %q opens %v", rc, out, c.memOpens)
	}
}

func TestCLIButtonsWatchStops(t *testing.T) {
	c := newCLI(t, "DXP4800")
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	c.Tick = func() (<-chan time.Time, func()) { return ticks, func() {} }
	cancel()
	var out bytes.Buffer
	if rc := Main(ctx, c.Env, []string{"buttons", "--watch"}, &out, &out); rc != 0 || !strings.Contains(out.String(), "start:") {
		t.Errorf("rc %d %q", rc, out.String())
	}
}

func TestCLIUsage(t *testing.T) {
	c := newCLI(t, "DXP4800")
	if rc, _ := c.run(); rc != 2 {
		t.Error("no args")
	}
	if rc, _ := c.run("nope"); rc != 2 {
		t.Error("unknown")
	}
	if rc, out := c.run("help"); rc != 0 || !strings.Contains(out, "Usage") {
		t.Error("help")
	}
}

func TestECBeeperRunsUnderTheSharedLock(t *testing.T) {
	c := newCLI(t, "iDX6011 Pro")
	if rc, out := c.run("beep"); rc != 0 {
		t.Fatalf("rc %d %q", rc, out)
	}
	if c.ec.unlocked != 0 {
		t.Errorf("%d EC port accesses outside the lock", c.ec.unlocked)
	}
	// One lock for the chip id, one per write (on, off); never held while the beep sounds.
	if c.locks != 3 || c.heldInWait || c.heldNow != 0 {
		t.Errorf("locks %d heldInWait %v heldNow %d", c.locks, c.heldInWait, c.heldNow)
	}
}

func TestECBeeperRetryLocksEachExchange(t *testing.T) {
	ec := &fakeEC{deaf: true}
	n := 0
	b := &ecBeeper{p: ec, reg: 0xA5, wait: 50 * time.Millisecond, lock: func(fn func() error) error { n++; return fn() }}
	_ = b.On(0)
	if n != ecTries {
		t.Errorf("%d locks for %d tries", n, ecTries)
	}
}

func TestECBeeperGuards(t *testing.T) {
	// a rival fan writer
	c := newCLI(t, "iDX6011 Pro")
	c.rivals = []conflict.Conflict{{Kind: conflict.KindDaemon, Name: "Rival", Controls: []string{conflict.Fan}, Severity: conflict.SeverityBlock}}
	if rc, out := c.run("beep"); rc != 1 || !strings.Contains(out, "Rival") || len(c.ec.writes) != 0 || c.locks != 0 {
		t.Errorf("rival: rc %d %q", rc, out)
	}
	// a warn-only finding does not block
	c = newCLI(t, "iDX6011 Pro")
	c.rivals = []conflict.Conflict{{Controls: []string{conflict.Fan}, Severity: conflict.SeverityWarn}}
	if rc, out := c.run("beep"); rc != 0 {
		t.Errorf("warn: rc %d %q", rc, out)
	}
	// kernel EC driver bound
	c = newCLI(t, "iDX6011 Pro")
	dev := filepath.Join(c.ACPIDir, "PNP0C09:00")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent", filepath.Join(dev, "driver")); err != nil {
		t.Fatal(err)
	}
	if rc, out := c.run("beep"); rc != 1 || !strings.Contains(out, "ACPI embedded controller driver is bound") || len(c.ec.writes) != 0 {
		t.Errorf("bound: rc %d %q", rc, out)
	}
	// unbound device directory is fine
	if err := os.Remove(filepath.Join(dev, "driver")); err != nil {
		t.Fatal(err)
	}
	if rc, out := c.run("beep"); rc != 0 {
		t.Errorf("unbound: rc %d %q", rc, out)
	}
	// wrong chip: refused on the Pro, only noted on the plain iDX6011
	c = newCLI(t, "iDX6011 Pro")
	c.ec.chip = 0x8613
	if rc, out := c.run("beep"); rc != 1 || !strings.Contains(out, "0x8613") || len(c.ec.writes) != 0 {
		t.Errorf("chip: rc %d %q", rc, out)
	}
	c = newCLI(t, "iDX6011")
	c.ec.chip = 0x8613
	if rc, out := c.run("beep"); rc != 0 || len(c.ec.writes) != 2 {
		t.Errorf("soft chip: rc %d %q", rc, out)
	}
}

func TestOtherBeepersTakeNoLock(t *testing.T) {
	for _, m := range []string{"DXP4800", "DXP480T Plus"} {
		c := newCLI(t, m)
		if rc, out := c.run("beep"); rc != 0 || c.locks != 0 {
			t.Errorf("%s: rc %d %q locks %d", m, rc, out, c.locks)
		}
	}
}
