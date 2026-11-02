package fan

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

func TestDecodeITEAC(t *testing.T) {
	// (F2[5], F4[6:5]) -> value (F2[5] << 2 | F4[6:5]): 1 on, 3 off, 7 last, the rest (also the old BIOS's 0, 2, 4) unknown.
	want := map[byte]acMode{1: acOn, 3: acOff, 7: acLast}
	for f2bit := byte(0); f2bit < 2; f2bit++ {
		for f4 := byte(0); f4 < 4; f4++ {
			v := f2bit<<2 | f4
			w, ok := want[v]
			if !ok {
				w = acUnknown
			}
			// Other bits set and cleared must not matter.
			for _, noise := range [][2]byte{{0x00, 0x00}, {0xdf, 0x9f}} {
				if got := decodeITEAC(noise[0]|f2bit<<5, noise[1]|f4<<5); got != w {
					t.Errorf("v=%d noise %x: %q, want %q", v, noise, got, w)
				}
			}
		}
	}
}

// IT8613E register values per mode: on = F2[5]=0 F4[5]=1 F4[6]=0, off = 0 1 1, last = 1 1 1.
func iteACBytes(m acMode, f2other, f4other byte) (byte, byte) {
	f2, f4 := f2other&^0x20, f4other&^0x60|0x20
	switch m {
	case acOff:
		f4 |= 0x40
	case acLast:
		f2 |= 0x20
		f4 |= 0x40
	}
	return f2, f4
}

func newACFake(f2, f4 byte) (*fakeSIO, iteAC) {
	f := newFakeSIO()
	f.regs[[2]byte{sioLDNMonitor, iteRegACF2}], f.regs[[2]byte{sioLDNMonitor, iteRegACF4}] = f2, f4
	return f, iteAC{chip: &Chip{ports: f}}
}

func TestITEACSetEveryTransition(t *testing.T) {
	modes := []acMode{acOn, acOff, acLast}
	for _, other := range [][2]byte{{0xdf, 0x9f}, {0x00, 0x00}, {0x5a, 0x85}} {
		for _, from := range modes {
			for _, to := range modes {
				s2, s4 := iteACBytes(from, other[0], other[1])
				f, ac := newACFake(s2, s4)
				if err := ac.Set(to); err != nil {
					t.Fatalf("%s -> %s: %v", from, to, err)
				}
				w2, w4 := iteACBytes(to, other[0], other[1])
				if g2, g4 := f.regs[[2]byte{4, iteRegACF2}], f.regs[[2]byte{4, iteRegACF4}]; g2 != w2 || g4 != w4 {
					t.Errorf("%s -> %s (other %x): F2 %#x F4 %#x, want %#x %#x", from, to, other, g2, g4, w2, w4)
				}
				// Only LDN 4, only F2 and F4, only registers that changed, F2 before F4.
				for _, w := range f.wlog {
					if w.ldn != 4 || (w.reg != 0xf2 && w.reg != 0xf4) {
						t.Errorf("write outside the list: %+v", w)
					}
				}
				if w2 != s2 && w4 != s4 && (f.wlog[0].reg != 0xf2 || f.wlog[1].reg != 0xf4) {
					t.Errorf("%s -> %s: order %v", from, to, f.wlog)
				}
				if (w2 != s2) != slices.ContainsFunc(f.wlog, func(w sioWrite) bool { return w.reg == 0xf2 }) {
					t.Errorf("%s -> %s: F2 written only when it changes: %v", from, to, f.wlog)
				}
				if f.sioKey != 0 {
					t.Fatal("config mode not left")
				}
			}
		}
	}
}

func TestITEACLastToOnWritesF2ThenF4(t *testing.T) {
	s2, s4 := iteACBytes(acLast, 0xdf, 0x9f)
	f, ac := newACFake(s2, s4)
	if err := ac.Set(acOn); err != nil {
		t.Fatal(err)
	}
	want := []sioWrite{{4, 0xf2, 0xdf}, {4, 0xf4, 0xbf}}
	if !slices.Equal(f.wlog, want) {
		t.Fatalf("writes %v, want %v", f.wlog, want)
	}
}

func TestITEACReadbackMismatchFails(t *testing.T) {
	s2, s4 := iteACBytes(acOn, 0xdf, 0x9f)
	f, ac := newACFake(s2, s4)
	f.ignoreWrite = true
	if err := ac.Set(acOff); !errors.Is(err, errACVerify) {
		t.Fatalf("err %v", err)
	}
}

func TestITEACUnknownPatternGetReportsRawSetWrites(t *testing.T) {
	for _, b := range [][2]byte{{0x00, 0x00}, {0x00, 0x40}, {0x20, 0x00}, {0xdf, 0x9f}} { // old BIOS encodings and a plain 0
		f, ac := newACFake(b[0], b[1])
		st, err := ac.Get()
		if err != nil || st.Mode != acUnknown || st.Raw["f2"] != int(b[0]) || st.Raw["f4"] != int(b[1]) {
			t.Errorf("%x: get %+v %v", b, st, err)
		}
		if len(f.wlog) != 0 {
			t.Errorf("%x: get wrote %v", b, f.wlog)
		}
		for _, m := range []acMode{acOn, acOff, acLast} {
			_, ac := newACFake(b[0], b[1])
			if err := ac.Set(m); err != nil {
				t.Errorf("%x -> %s: %v", b, m, err)
			}
			w2, w4 := iteACBytes(m, b[0], b[1])
			if st, err := ac.Get(); err != nil || st.Mode != m || st.Raw["f2"] != int(w2) || st.Raw["f4"] != int(w4) {
				t.Errorf("%x -> %s: read back %+v %v, want f2 %02x f4 %02x", b, m, st, err, w2, w4)
			}
		}
	}
	if err := (iteAC{}).Set(acUnknown); !errors.Is(err, errACNotSettable) {
		t.Fatalf("unknown mode: %v", err)
	}
}

func TestITEACGate(t *testing.T) {
	f, ac := newACFake(0x00, 0x20)
	f.id = 0x8612
	if err := ac.Set(acOff); !errors.Is(err, errNotIT8613) || len(f.wlog) != 0 {
		t.Fatalf("err %v writes %v", err, f.wlog)
	}
	if _, err := ac.Get(); !errors.Is(err, errNotIT8613) {
		t.Fatalf("get: %v", err)
	}
}

// cmdEC records every port write and every bare command byte (anything but a register read or write).
type cmdEC struct {
	*fakeEC
	cmds []byte
	outs int
}

func (c *cmdEC) Out(port uint16, v byte) error {
	c.outs++
	if port == ecCmdPort && v != ecCmdRead && v != ecCmdWrite {
		c.cmds = append(c.cmds, v)
	}
	return c.fakeEC.Out(port, v)
}

func TestECACCommands(t *testing.T) {
	f := &cmdEC{fakeEC: newFakeEC()}
	ec, err := DetectEC(f)
	if err != nil {
		t.Fatal(err)
	}
	f.outs = 0
	ac := ecAC{ec: ec}
	if err := ac.Set(acOn); err != nil {
		t.Fatal(err)
	}
	if err := ac.Set(acOff); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.cmds, []byte{0x88, 0x89}) || f.outs != 2 {
		t.Fatalf("commands %x, %d port writes", f.cmds, f.outs)
	}
	f.cmds, f.outs = nil, 0
	if err := ac.Set(acLast); !errors.Is(err, errACNotSettable) || f.outs != 0 {
		t.Fatalf("last: %v, %d port writes", err, f.outs)
	}
	st, err := ac.Get()
	if err != nil || st.Readable || st.Mode != acUnknown || st.Source != "ec-cmd" || !slices.Equal(st.Settable, []acMode{acOn, acOff}) {
		t.Fatalf("get %+v %v", st, err)
	}
	if f.outs != 0 {
		t.Fatal("Get touched the controller")
	}
}

func TestECSendCommandAllowlist(t *testing.T) {
	gt := &cmdEC{fakeEC: newFakeEC()}
	ec, _ := DetectEC(gt)
	gt.outs = 0
	for _, cmd := range []byte{0x80, 0x81, 0x66, 0x00, 0xff} {
		if err := ec.SendCommand(cmd); !errors.Is(err, errNotAllowed) {
			t.Errorf("GT 0x%02x: %v", cmd, err)
		}
	}
	if gt.outs != 0 {
		t.Fatalf("%d port writes", gt.outs)
	}

	pro := &cmdEC{fakeEC: newFakeIdxEC()}
	ecPro, err := detectEC(pro, ecSpecIdxPro)
	if err != nil {
		t.Fatal(err)
	}
	plain := &cmdEC{fakeEC: newFakeIdxEC()}
	ecPlain, err := detectEC(plain, ecSpecIdx)
	if err != nil {
		t.Fatal(err)
	}
	pro.outs, plain.outs = 0, 0
	if err := ecPro.SendCommand(0x88); !errors.Is(err, errNotAllowed) {
		t.Errorf("Pro: %v", err)
	}
	if err := ecPlain.SendCommand(0x88); !errors.Is(err, errReadOnly) {
		t.Errorf("plain iDX: %v", err)
	}
	if pro.outs != 0 || plain.outs != 0 {
		t.Fatal("port writes on a model without the command")
	}
}

type memAccess struct {
	addr  uint64
	width int
	write bool
}

type fakeMem struct {
	b        map[uint64]byte
	touched  map[uint64]bool
	accesses []memAccess
	writes   int
	ignore   bool
	// flip is XORed into every read of the register: bits that change on their own.
	flip uint32
}

func newFakeMem() *fakeMem { return &fakeMem{b: map[uint64]byte{}, touched: map[uint64]bool{}} }

func (m *fakeMem) set32(addr uint64, v uint32) {
	for i := range 4 {
		m.b[addr+uint64(i)] = byte(v >> (8 * i))
	}
}

func (m *fakeMem) get32(addr uint64) (v uint32) {
	for i := 3; i >= 0; i-- {
		v = v<<8 | uint32(m.b[addr+uint64(i)])
	}
	return
}

func (m *fakeMem) Read(addr uint64, width int) (uint32, error) {
	if err := hw.CheckMemAccess(addr, width); err != nil {
		return 0, err
	}
	m.accesses = append(m.accesses, memAccess{addr, width, false})
	var v uint32
	for i := width - 1; i >= 0; i-- {
		m.touched[addr+uint64(i)] = true
		v = v<<8 | uint32(m.b[addr+uint64(i)])
	}
	return v ^ m.flip, nil
}

func (m *fakeMem) Write(addr uint64, width int, v uint32) error {
	if err := hw.CheckMemAccess(addr, width); err != nil {
		return err
	}
	m.accesses = append(m.accesses, memAccess{addr, width, true})
	m.writes++
	for i := range width {
		m.touched[addr+uint64(i)] = true
		if !m.ignore {
			m.b[addr+uint64(i)] = byte(v >> (8 * i))
		}
	}
	return nil
}

func (m *fakeMem) Close() error { return nil }

func TestIdxACBit0Only(t *testing.T) {
	m := newFakeMem()
	m.set32(idxACAddr, 0xa5a5a5a4)
	ac := newIdxAC(m, false)
	st, err := ac.Get()
	if err != nil || st.Mode != acOn || !st.Readable || st.Source != "mmio-idx" || !slices.Equal(st.Settable, []acMode{acOn, acOff}) {
		t.Fatalf("get %+v %v", st, err)
	}
	if err := ac.Set(acOff); err != nil {
		t.Fatal(err)
	}
	if got := m.get32(idxACAddr); got != 0xa5a5a5a5 {
		t.Fatalf("off: %#x", got)
	}
	if st, _ := ac.Get(); st.Mode != acOff {
		t.Fatalf("mode %q", st.Mode)
	}
	if err := ac.Set(acOn); err != nil {
		t.Fatal(err)
	}
	if got := m.get32(idxACAddr); got != 0xa5a5a5a4 {
		t.Fatalf("on: %#x", got)
	}
	w := m.writes
	if err := ac.Set(acLast); !errors.Is(err, errACNotSettable) || m.writes != w {
		t.Fatalf("last: %v", err)
	}
	for a := range m.touched {
		if a < idxACAddr || a > idxACAddr+3 {
			t.Fatalf("touched 0x%X", a)
		}
	}
	m.ignore = true
	if err := ac.Set(acOff); !errors.Is(err, errACVerify) {
		t.Fatalf("mismatch: %v", err)
	}
	for _, a := range m.accesses {
		if a.addr != idxACAddr || a.width != 4 {
			t.Fatalf("access %+v: must be one aligned dword", a)
		}
	}
}

// Readback is compared on bit 0 only: other bits of the dword changing on their own is not a failure.
func TestIdxACVerifyMasksOwnBits(t *testing.T) {
	m := newFakeMem()
	m.set32(idxACAddr, 0x10)
	m.flip = 0xfffffffe
	if err := newIdxAC(m, false).Set(acOff); err != nil {
		t.Fatalf("foreign bits changed: %v", err)
	}
}

// The plain iDX6011/6012 is read only: Get works, Set refuses and writes nothing.
func TestIdxPlainACReadOnly(t *testing.T) {
	m := newFakeMem()
	m.set32(idxACAddr, 1)
	ac := newIdxAC(m, true)
	st, err := ac.Get()
	if err != nil || st.Mode != acOff || !st.Readable || len(st.Settable) != 0 {
		t.Fatalf("get %+v %v", st, err)
	}
	for _, mode := range []acMode{acOn, acOff, acLast} {
		if err := ac.Set(mode); !errors.Is(err, errACReadOnly) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
	if m.writes != 0 {
		t.Fatal("wrote on a read-only model")
	}
}

func TestPowerACSetRefusedOnPlainIdx(t *testing.T) {
	for _, model := range []string{"iDX6011", "iDX6012"} {
		acRig(t, model)
		mem := newFakeMem()
		env := &acEnv{openPorts: func() (Ports, error) { return newFakeSIO(), nil }, openMem: func() (hw.Mem, error) { return mem, nil }}
		_, err := runPowerAC([]string{"set", "off"}, model, env, func() error { return nil }, time.Unix(1, 0))
		if !errors.Is(err, errACReadOnly) || mem.writes != 0 || readACRecord() != nil {
			t.Fatalf("%s: err %v writes %d", model, err, mem.writes)
		}
		env = &acEnv{openPorts: env.openPorts, openMem: env.openMem}
		out, err := runPowerAC(nil, model, env, noRival, time.Unix(1, 0))
		if err != nil || out["readable"] != true {
			t.Fatalf("%s read: %v %v", model, out, err)
		}
	}
}

func TestFCHACByteOnly(t *testing.T) {
	m := newFakeMem()
	m.b[fchACAddr] = 0xf4
	ac := newFCHAC(m)
	for _, tc := range []struct {
		to   acMode
		want byte
	}{{acOn, 0xf5}, {acLast, 0xf7}, {acOff, 0xf4}} {
		if err := ac.Set(tc.to); err != nil {
			t.Fatal(err)
		}
		if m.b[fchACAddr] != tc.want {
			t.Fatalf("%s: %#x, want %#x", tc.to, m.b[fchACAddr], tc.want)
		}
	}
	m.b[fchACAddr] = 0xf6 // 2: last, after an abnormal shutdown only
	st, err := ac.Get()
	if err != nil || st.Mode != acLast || st.Raw["field"] != 2 || st.Raw["value"] != 0xf6 || st.Source != "fch-mmio" {
		t.Fatalf("get %+v %v", st, err)
	}
	if len(m.touched) != 1 || !m.touched[fchACAddr] {
		t.Fatalf("addresses touched: %v", m.touched)
	}
	for _, a := range m.accesses {
		if a.addr != fchACAddr || a.width != 1 {
			t.Fatalf("access %+v: must be one byte", a)
		}
	}
	m.ignore = true
	if err := ac.Set(acOn); !errors.Is(err, errACVerify) {
		t.Fatalf("mismatch: %v", err)
	}
}

func TestPowerACFor(t *testing.T) {
	isolate(t, "x")
	cs := newFakeCS()
	mem := newFakeMem()
	env := func() *acEnv {
		return &acEnv{
			openPorts: func() (Ports, error) { return cs, nil },
			openMem:   func() (hw.Mem, error) { return mem, nil },
		}
	}
	fakes := map[string]Ports{"DXP4800 GT": newFakeEC(), "DXP2800 GT": cs}
	for model, want := range map[string]string{
		"DXP2800": "it8613", "DXP4800": "it8613", "DXP4800 Plus": "it8613", "DXP4800 Pro": "it8613",
		"DXP6800 Pro": "it8613", "DXP8800 Plus": "it8613",
		"DXP4800 GT": "ec-cmd", "iDX6011": "mmio-idx", "iDX6012": "mmio-idx", "iDX6011 Pro": "mmio-idx",
		"DXP2800 GT": "fch-mmio", "DX4600 Pro": "", "": "",
	} {
		e := env()
		if p, ok := fakes[model]; ok {
			e.openPorts = func() (Ports, error) { return p, nil }
		} else {
			e.openPorts = func() (Ports, error) { return newFakeSIO(), nil }
		}
		ac, err := powerACFor(model, e)
		if want == "" {
			if !errors.Is(err, errNoACControl) {
				t.Errorf("%q: %v", model, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", model, err)
			continue
		}
		st, err := ac.Get()
		if err != nil || st.Source != want {
			t.Errorf("%q: source %q err %v, want %q", model, st.Source, err, want)
		}
	}
}

func TestPowerACForRefusesForeignChips(t *testing.T) {
	isolate(t, "x")
	e := &acEnv{openPorts: func() (Ports, error) { return newFakeSIO(), nil }, openMem: func() (hw.Mem, error) { return newFakeMem(), nil }}
	if _, err := powerACFor("DXP2800 GT", e); err == nil { // an IT8613E answers where a CS201x is expected
		t.Fatal("no error")
	}
}

func acRig(t *testing.T, model string) (*acEnv, *fakeSIO) {
	t.Helper()
	isolate(t, model)
	oldLock, oldRec := lockPath, powerACRecordPath
	dir := t.TempDir()
	lockPath, powerACRecordPath = filepath.Join(dir, "lock"), filepath.Join(dir, "power_ac.json")
	t.Cleanup(func() { lockPath, powerACRecordPath = oldLock, oldRec })
	f := newFakeSIO()
	s2, s4 := iteACBytes(acOff, 0xdf, 0x9f)
	f.regs[[2]byte{4, iteRegACF2}], f.regs[[2]byte{4, iteRegACF4}] = s2, s4
	return &acEnv{openPorts: func() (Ports, error) { return f, nil }, openMem: func() (hw.Mem, error) { return nil, errors.New("no mem") }}, f
}

var noRival = func() error { return nil }

func TestPowerACCommand(t *testing.T) {
	env, f := acRig(t, "DXP4800")
	at := time.Unix(1_700_000_000, 0)

	out, err := runPowerAC(nil, "DXP4800", env, noRival, at)
	if err != nil {
		t.Fatal(err)
	}
	if out["mode"] != acOff || out["readable"] != true || out["source"] != "it8613" || out["last_set"] != nil || len(f.wlog) != 0 {
		t.Fatalf("read %+v writes %v", out, f.wlog)
	}

	env, f = acRig(t, "DXP4800")
	out, err = runPowerAC([]string{"set", "last"}, "DXP4800", env, noRival, at)
	if err != nil {
		t.Fatal(err)
	}
	if out["mode"] != acLast || out["verified"] != true || out["set"] != "last" {
		t.Fatalf("set %+v", out)
	}
	if rec, _ := out["last_set"].(*acRecord); rec == nil || rec.Mode != "last" || rec.Time != at.Unix() {
		t.Fatalf("record %+v", out["last_set"])
	}
	if len(f.wlog) == 0 {
		t.Fatal("nothing written")
	}
}

func TestPowerACCommandUsageAndRival(t *testing.T) {
	for _, args := range [][]string{{"on"}, {"set"}, {"set", "sometimes"}, {"set", "on", "x"}, {"get"}} {
		env, f := acRig(t, "DXP4800")
		if _, err := runPowerAC(args, "DXP4800", env, noRival, time.Now()); !errors.Is(err, errACUsage) {
			t.Errorf("%v: %v", args, err)
		}
		if f.access != 0 {
			t.Errorf("%v: hardware touched", args)
		}
	}

	env, f := acRig(t, "DXP4800")
	rival := func() error { return errors.New("FanCtrl Plus controls the fans too") }
	if _, err := runPowerAC([]string{"set", "on"}, "DXP4800", env, rival, time.Now()); err == nil || f.access != 0 {
		t.Fatalf("set with a rival: %v, %d accesses", err, f.access)
	}
	// A read is not a write: a rival does not stop it.
	if _, err := runPowerAC(nil, "DXP4800", env, rival, time.Now()); err != nil {
		t.Fatalf("read with a rival: %v", err)
	}
	if _, err := runPowerAC(nil, "DX4600 Pro", env, noRival, time.Now()); !errors.Is(err, errNoACControl) {
		t.Fatalf("unsupported model: %v", err)
	}
}

// Nothing but the explicit command reaches this setting: status reads no port of it, with fan control off not even the
// fan chip.
func TestStatusNeverTouchesPowerAC(t *testing.T) {
	_, f := acRig(t, "DXP4800")
	dir := t.TempDir()
	oldCfg := configPath
	configPath = filepath.Join(dir, "fan.cfg")
	t.Cleanup(func() { configPath = oldCfg })
	if err := os.WriteFile(configPath, []byte("fan_control=\"0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := statusAnswer(func() (Fans, error) {
		t.Fatal("status opened the chip with fan control off")
		return nil, nil
	})
	if f.access != 0 {
		t.Fatalf("%d port accesses", f.access)
	}
	for k := range out {
		if k == "power_ac" || k == "power_on_ac" {
			t.Fatalf("status has %q", k)
		}
	}
}

// From a value this tool cannot decode, `set` needs --force; the refusal names the raw bytes and writes nothing.
func TestPowerACSetFromUnknownNeedsForce(t *testing.T) {
	setup := func() (*acEnv, *fakeSIO) {
		env, f := acRig(t, "DXP4800")
		f.regs[[2]byte{4, iteRegACF2}], f.regs[[2]byte{4, iteRegACF4}] = 0x00, 0x40
		return env, f
	}
	env, f := setup()
	_, err := runPowerAC([]string{"set", "on"}, "DXP4800", env, noRival, time.Unix(1, 0))
	if !errors.Is(err, errACUnknown) || len(f.wlog) != 0 || readACRecord() != nil {
		t.Fatalf("err %v writes %v", err, f.wlog)
	}
	for _, want := range []string{"F2 0x00", "F4 0x40", "may differ", "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err, want)
		}
	}

	env, f = setup()
	out, err := runPowerAC([]string{"set", "on", "--force"}, "DXP4800", env, noRival, time.Unix(1, 0))
	if err != nil || out["mode"] != acOn || out["verified"] != true || len(f.wlog) == 0 {
		t.Fatalf("forced: %v %v writes %v", out, err, f.wlog)
	}

	// A known value needs no flag; --force alone or in the wrong place is a usage error.
	env, _ = acRig(t, "DXP4800")
	if _, err := runPowerAC([]string{"set", "on"}, "DXP4800", env, noRival, time.Unix(1, 0)); err != nil {
		t.Fatalf("known value: %v", err)
	}
	for _, args := range [][]string{{"--force"}, {"set", "--force", "on"}, {"set", "on", "--forced"}} {
		env, f = acRig(t, "DXP4800")
		if _, err := runPowerAC(args, "DXP4800", env, noRival, time.Now()); !errors.Is(err, errACUsage) || f.access != 0 {
			t.Errorf("%v: %v", args, err)
		}
	}
}
