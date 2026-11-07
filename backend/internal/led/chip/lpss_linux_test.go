//go:build linux

package chip

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// fakeDW is a DesignWare controller behind an LPSS wrapper: plain registers, the enable bit mirrored to
// ENABLE_STATUS, and a transfer that answers reads from resp (or NAKs).
type fakeDW struct {
	regs   map[int]uint32
	enable bool
	rx     []byte
	resp   []byte
	nak    bool
	raw    uint32
	log    []string // "r<off>" and "w<off>"
	writes int
	rwMaps int
	roMaps int
}

func newFakeDW() *fakeDW {
	return &fakeDW{regs: map[int]uint32{
		dwCompTypeReg:  dwCompType,
		dwCompVersion:  dwSDAHoldVers,
		dwCompParam1:   15<<16 | 15<<8,
		lpssPrivResets: 0x7,
		dwSSHcnt:       0x190, dwSSLcnt: 0x1d6, // firmware leftovers
	}}
}

func (f *fakeDW) load(reg int) uint32 {
	f.log = append(f.log, "r"+itoa(reg))
	switch reg {
	case dwEnableStatus:
		if f.enable {
			return 1
		}
		return 0
	case dwRxFLR:
		return uint32(len(f.rx))
	case dwRawIntrStat:
		return f.raw
	case dwDataCmd:
		if len(f.rx) == 0 {
			return 0
		}
		b := f.rx[0]
		f.rx = f.rx[1:]
		return uint32(b)
	case dwClrIntr:
		f.raw = 0
	case dwClrStopDet:
		f.raw &^= 1 << 9
	}
	return f.regs[reg]
}

func (f *fakeDW) store(reg int, v uint32) {
	f.log = append(f.log, "w"+itoa(reg))
	f.writes++
	switch reg {
	case dwEnable:
		f.enable = v&1 != 0
	case dwDataCmd:
		if !f.enable {
			return
		}
		if f.nak {
			f.raw |= 1 << 6
			f.regs[dwTxAbrtSource] = 1
			return
		}
		if v&dwCmdRead != 0 && len(f.resp) > 0 {
			f.rx, f.resp = append(f.rx, f.resp[0]), f.resp[1:]
		}
		if v&dwCmdStop != 0 {
			f.raw |= 1 << 9
		}
		return
	}
	f.regs[reg] = v
}

func itoa(n int) string { return strconv.Itoa(n) }

// sysOpts changes one thing in the fake sysfs.
type sysOpts struct {
	driver       bool
	pmState      int
	noPM         bool
	command      int
	otherMatches bool
	noMCU        bool
	vendor       string
	class        string
}

func config(o sysOpts) []byte {
	cfg := make([]byte, 256)
	cfg[0x04], cfg[0x05] = byte(o.command), byte(o.command>>8)
	if !o.noPM {
		cfg[0x06] = 0x10 // capability list
		cfg[0x34] = 0x80
		cfg[0x80], cfg[0x81] = 0x01, 0x00
		cfg[0x84] = byte(o.pmState)
	}
	return cfg
}

func fakeSys(t *testing.T, o sysOpts) {
	t.Helper()
	if o.command == 0 {
		o.command = 0x0006
	}
	if o.vendor == "" {
		o.vendor = "0x8086"
	}
	if o.class == "" {
		o.class = "0x0c8000"
	}
	dir := t.TempDir()
	put := func(rel string, data []byte) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !o.noMCU {
		put("bus/acpi/devices/PNP0C0A:00/hid", []byte("PNP0C0A\n"))
		put("bus/acpi/devices/HT32F522:00/hid", []byte("HT32F522\n"))
		put("bus/acpi/devices/HT32F522:00/path", []byte("\\_SB_.PC00.I2C1.MCU0\n"))
		put("bus/acpi/devices/HT32F522:00/status", []byte("15\n"))
	}
	pci := func(bdf, device, fw string) {
		base := "bus/pci/devices/" + bdf + "/"
		put(base+"vendor", []byte(o.vendor+"\n"))
		put(base+"device", []byte(device+"\n"))
		put(base+"class", []byte(o.class+"\n"))
		put(base+"firmware_node/path", []byte(fw+"\n"))
		put(base+"resource", []byte("0x0000000040171000 0x0000000040171fff 0x0000000000040200\n0x0 0x0 0x0\n"))
		put(base+"config", config(o))
	}
	pci("0000:00:15.0", "0x54e8", "\\_SB_.PC00.I2C1")
	pci("0000:00:15.1", "0x54e9", "\\_SB_.PC00.I2C2")
	if o.otherMatches {
		pci("0000:00:19.0", "0x54c5", "\\_SB_.PC00.I2C1")
	}
	if o.driver {
		if err := os.Symlink("../../../../bus/pci/drivers/intel-lpss", filepath.Join(dir, "bus/pci/devices/0000:00:15.0/driver")); err != nil {
			t.Fatal(err)
		}
	}
	old := lpssSys
	lpssSys = dir
	t.Cleanup(func() { lpssSys = old })
}

func fakeMem(t *testing.T, f *fakeDW) {
	t.Helper()
	old := lpssMapMem
	lpssMapMem = func(phys int64, write bool) (dwRegs, func(), error) {
		if phys != 0x40171000 {
			t.Errorf("mapped 0x%x, want BAR0 0x40171000", phys)
		}
		if write {
			f.rwMaps++
		} else {
			f.roMaps++
		}
		return f, func() {}, nil
	}
	t.Cleanup(func() { lpssMapMem = old })
}

func probe(t *testing.T, o sysOpts, f *fakeDW) (*lpssReport, error) {
	t.Helper()
	fakeSys(t, o)
	fakeMem(t, f)
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, true)
	if d != nil {
		d.close()
	}
	return r, err
}

func TestLPSSTiming(t *testing.T) {
	// 133 MHz, SDA fall 171 ns, SCL fall 208 ns, SDA hold 42 ns: what the Linux driver computes.
	if got := lpssTiming(); got != (dwTiming{hcnt: 552, lcnt: 652, sdaHold: 6}) {
		t.Errorf("timing %+v", got)
	}
}

func TestLPSSChipIDMatch(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5} // word, low byte first
	r, err := probe(t, sysOpts{}, f)
	if err != nil {
		t.Fatal(err, r.lines)
	}
	if f.enable || f.regs[dwEnableStatus] != 0 {
		t.Error("controller left enabled")
	}
	if f.regs[dwSSHcnt] != 552 || f.regs[dwSSLcnt] != 652 || f.regs[dwSDAHold] != 0x10006 {
		t.Errorf("timing registers %d %d 0x%x", f.regs[dwSSHcnt], f.regs[dwSSLcnt], f.regs[dwSDAHold])
	}
	if f.regs[lpssPrivResets] != 0x7 || f.regs[dwTar] != controllerAddr {
		t.Errorf("resets 0x%x tar 0x%x", f.regs[lpssPrivResets], f.regs[dwTar])
	}
	if f.roMaps != 1 || f.rwMaps != 1 {
		t.Errorf("maps ro %d rw %d", f.roMaps, f.rwMaps)
	}
	text := strings.Join(r.lines, "\n")
	for _, want := range []string{"acpi.path:", `\_SB_.PC00.I2C1.MCU0`, "0000:00:15.0 [8086:54e8]", "pci.driver:        none", "D0", "0x40171000", "0x44570140", "0xc5b2 (expected 0xc5b2)"} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q:\n%s", want, text)
		}
	}
}

func TestLPSSChipIDMismatch(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0x34, 0x12}
	r, err := probe(t, sysOpts{}, f)
	if err == nil || !strings.Contains(err.Error(), "0x1234") {
		t.Fatalf("want a chip id refusal, got %v", err)
	}
	if f.enable || f.regs[dwEnableStatus] != 0 {
		t.Error("controller not disabled after a mismatch")
	}
	if f.writes == 0 {
		t.Error("the probe transfer should have written controller registers")
	}
	if last := r.lines[len(r.lines)-1]; !strings.HasPrefix(last, "result:") {
		t.Errorf("last line %q", last)
	}
}

func TestLPSSNoAnswer(t *testing.T) {
	f := newFakeDW()
	f.nak = true
	_, err := probe(t, sysOpts{}, f)
	if err == nil || !strings.Contains(err.Error(), "no acknowledge") {
		t.Fatalf("want a NAK refusal, got %v", err)
	}
	if f.enable {
		t.Error("controller not disabled after a NAK")
	}
}

func TestLPSSResetTakenOut(t *testing.T) {
	f := newFakeDW()
	f.regs[lpssPrivResets] = 0
	f.resp = []byte{0xb2, 0xc5}
	if _, err := probe(t, sysOpts{}, f); err != nil {
		t.Fatal(err)
	}
	if f.regs[lpssPrivResets] != 0x7 {
		t.Errorf("RESETS 0x%x, want 0x7 (intel-lpss.c:259-265)", f.regs[lpssPrivResets])
	}
}

// Every refusal before the bring-up: nothing is written and no writable mapping is made.
func TestLPSSRefusals(t *testing.T) {
	cases := []struct {
		name string
		o    sysOpts
		mod  func(*fakeDW)
		want string
	}{
		{"no MCU", sysOpts{noMCU: true}, nil, "HT32"},
		{"two functions on the path", sysOpts{otherMatches: true}, nil, "exactly one"},
		{"driver bound", sysOpts{driver: true}, nil, "intel-lpss is bound"},
		{"wrong vendor", sysOpts{vendor: "0x1022"}, nil, "not an Intel LPSS"},
		{"wrong class", sysOpts{class: "0x060400"}, nil, "not an Intel LPSS"},
		{"memory decoding off", sysOpts{command: 0x0004}, nil, "memory decoding"},
		{"D3hot", sysOpts{pmState: 3}, nil, "D3"},
		{"D1", sysOpts{pmState: 1}, nil, "D1"},
		{"no PM capability", sysOpts{noPM: true}, nil, "power state"},
		{"comp type mismatch", sysOpts{}, func(f *fakeDW) { f.regs[dwCompTypeReg] = 0xffffffff }, "not a DesignWare"},
		{"held in reset", sysOpts{}, func(f *fakeDW) { f.regs[dwCompTypeReg] = 0; f.regs[lpssPrivResets] = 0 }, "held in reset"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeDW()
			f.resp = []byte{0xb2, 0xc5}
			if c.mod != nil {
				c.mod(f)
			}
			r, err := probe(t, c.o, f)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal with %q, got %v", c.want, err)
			}
			if f.writes != 0 || f.rwMaps != 0 {
				t.Errorf("%d writes, %d writable maps before a refusal:\n%v", f.writes, f.rwMaps, f.log)
			}
			if last := r.lines[len(r.lines)-1]; !strings.HasPrefix(last, "result:") || !strings.Contains(last, "refused") {
				t.Errorf("last line %q", last)
			}
		})
	}
}

// No write before the last read only check: the first write follows the component type read.
func TestLPSSWriteOrder(t *testing.T) {
	f := newFakeDW()
	f.regs[lpssPrivResets] = 0
	f.resp = []byte{0xb2, 0xc5}
	if _, err := probe(t, sysOpts{}, f); err != nil {
		t.Fatal(err)
	}
	typeRead, firstWrite := -1, -1
	for i, e := range f.log {
		if e == "r"+itoa(dwCompTypeReg) && typeRead < 0 {
			typeRead = i
		}
		if e[0] == 'w' && firstWrite < 0 {
			firstWrite = i
		}
	}
	if typeRead < 0 || firstWrite < typeRead {
		t.Errorf("first write at %d, first component type read at %d", firstWrite, typeRead)
	}
	if f.log[firstWrite] != "w"+itoa(lpssPrivResets) {
		t.Errorf("first write %s, want RESETS", f.log[firstWrite])
	}
}

func TestLPSSAcpiToPCI(t *testing.T) {
	fakeSys(t, sysOpts{otherMatches: false})
	_, _, path, _, controller, ok := lpssMCU()
	if !ok || path != `\_SB_.PC00.I2C1.MCU0` || controller != `\_SB_.PC00.I2C1` {
		t.Errorf("mcu %q %q %v", path, controller, ok)
	}
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	r, err := probe(t, sysOpts{}, f) // 00:15.1 has .I2C2 and must not be picked
	if err != nil || !strings.Contains(strings.Join(r.lines, "\n"), "0000:00:15.0") {
		t.Errorf("err %v\n%v", err, r.lines)
	}
}

func TestLPSSProbeCommand(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	fakeSys(t, sysOpts{})
	fakeMem(t, f)
	lpssReports = filepath.Join(t.TempDir(), "probe")
	t.Cleanup(func() { lpssReports = lpssReportFile })
	// The lock file path is fixed; skip when it cannot be created.
	if lock, err := acquireLock(); err != nil {
		t.Skip("no lock file:", err)
	} else {
		_ = lock.Close()
	}

	var out, errOut strings.Builder
	if rc := Run([]string{"lpss-probe", "--write"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc %d: %s %s", rc, out.String(), errOut.String())
	}
	t.Log("\n" + out.String())
	saved, _ := os.ReadFile(lpssReports)
	if string(saved) != out.String() || !strings.Contains(out.String(), "result:            ok") {
		t.Errorf("saved report differs or no result:\n%s", out.String())
	}
}

// Only a NAK diverts findBus to the LPSS controller; a busy or timing out SMBus must keep the SMBus path.
func TestLPSSNoDevice(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{syscall.ENXIO, true},
		{syscall.EREMOTEIO, true},
		{fmt.Errorf("wrapped: %w", syscall.ENXIO), true},
		{syscall.EBUSY, false},
		{syscall.ETIMEDOUT, false},
		{syscall.EIO, false},
		{syscall.EPERM, false},
	}
	for _, c := range cases {
		if got := lpssNoDevice(c.err); got != c.want {
			t.Errorf("lpssNoDevice(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
