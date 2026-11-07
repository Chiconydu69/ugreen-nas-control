//go:build linux

package chip

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeDW is a DesignWare controller behind an LPSS wrapper: plain registers, the enable bit mirrored to
// ENABLE_STATUS, and a transfer that answers reads from resp (or NAKs).
type fakeDW struct {
	regs   map[int]uint32
	enable bool
	rx     []byte
	resp   []byte
	nak    bool
	stuck  bool   // ENABLE_STATUS never drops to 0
	flipRW bool   // the component type reads wrong once the writable mapping exists
	hang   bool   // commands are swallowed: no data, no stop, no abort
	abrt   uint32 // IC_TX_ABRT_SOURCE of an abort other than a NAK
	roErr  error  // the read only mapping fails
	raw    uint32
	log    []string // "r<off>" and "w<off>"
	writes int
	rwMaps int
	roMaps int
	unmaps int
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
		if f.enable || f.stuck {
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
		if f.hang {
			return
		}
		if f.abrt != 0 {
			f.raw |= 1 << 6
			f.regs[dwTxAbrtSource] = f.abrt
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
	driver       string            // driver name bound to the function
	children     map[string]string // MFD children (nil: i2c_designware.0) and their drivers
	adapter      string            // "below": i2c-3 in a child; "class": a class device deeper below the function
	pmControl    string            // default auto
	pmStatus     string            // default active
	pmState      int
	noPM         bool
	command      int
	otherMatches bool
	noMCU        bool
	vendor       string
	class        string
	resource     string // BAR0 line of sysfs resource
	fwNode       string // firmware node path of the MCU controller function
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
	if o.resource == "" {
		o.resource = "0x0000000040171000 0x0000000040171fff 0x0000000000040200"
	}
	if o.fwNode == "" {
		o.fwNode = `\_SB_.PC00.I2C1`
	}
	pci := func(bdf, device, fw string) {
		base := "bus/pci/devices/" + bdf + "/"
		put(base+"vendor", []byte(o.vendor+"\n"))
		put(base+"device", []byte(device+"\n"))
		put(base+"class", []byte(o.class+"\n"))
		put(base+"firmware_node/path", []byte(fw+"\n"))
		put(base+"resource", []byte(o.resource+"\n0x0 0x0 0x0\n"))
		put(base+"config", config(o))
	}
	pci("0000:00:15.0", "0x54e8", o.fwNode)
	pci("0000:00:15.1", "0x54e9", "\\_SB_.PC00.I2C2")
	if o.otherMatches {
		pci("0000:00:19.0", "0x54c5", "\\_SB_.PC00.I2C1")
	}
	dev := "bus/pci/devices/0000:00:15.0/"
	put(dev+"power/control", []byte(orDefault(o.pmControl, "auto")+"\n"))
	put(dev+"power/runtime_status", []byte(orDefault(o.pmStatus, "active")+"\n"))
	link := func(rel, target string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755)
		if err := os.Symlink(target, filepath.Join(dir, rel)); err != nil {
			t.Fatal(err)
		}
	}
	if o.driver != "" {
		link(dev+"driver", "../../../../bus/pci/drivers/"+o.driver)
		children := o.children
		if children == nil {
			children = map[string]string{"i2c_designware.0": ""}
		}
		for name, drv := range children {
			put(dev+name+"/uevent", nil)
			if drv != "" {
				link(dev+name+"/driver", "../../../../../bus/platform/drivers/"+drv)
			}
		}
	}
	switch o.adapter {
	case "below":
		put(dev+"i2c_designware.0/i2c-3/name", nil)
	case "class":
		put(dev+"i2c_designware.0/x/y/i2c-9/name", nil)
		link("class/i2c-dev/i2c-9", "../../"+dev+"i2c_designware.0/x/y/i2c-9")
	}
	oldW, oldS, oldWait, oldPoll := lpssPMState, lpssSysWrite, lpssPMWait, lpssPMPoll
	lpssPMState, lpssPMWait, lpssPMPoll = filepath.Join(t.TempDir(), "power"), 60*time.Millisecond, time.Millisecond
	t.Cleanup(func() { lpssPMState, lpssSysWrite, lpssPMWait, lpssPMPoll = oldW, oldS, oldWait, oldPoll })
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
		if !write && f.roErr != nil {
			return nil, nil, f.roErr
		}
		if write {
			f.rwMaps++
			if f.flipRW {
				f.regs[dwCompTypeReg] = 0xffffffff
			}
		} else {
			f.roMaps++
		}
		return f, func() { f.unmaps++ }, nil
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
		{"no function on the path", sysOpts{fwNode: `\_SB_.PC00.I2C9`}, nil, "0 PCI functions"},
		{"BAR0 is I/O space", sysOpts{resource: "0x0000000040171000 0x0000000040171fff 0x0000000000040101"}, nil, "BAR0"},
		{"BAR0 under 4 KiB", sysOpts{resource: "0x0000000040171000 0x00000000401717ff 0x0000000000040200"}, nil, "at least 4 KiB"},
		{"BAR0 empty", sysOpts{resource: "0x0 0x0 0x0"}, nil, "at least 4 KiB"},
		{"read only map fails", sysOpts{}, func(f *fakeDW) { f.roErr = errors.New("operation not permitted") }, "cannot map BAR0 read only"},
		{"driver bound", sysOpts{driver: "i2c_designware_pci"}, nil, "other-driver"},
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
			if f.unmaps != f.roMaps {
				t.Errorf("%d read only maps, %d unmaps", f.roMaps, f.unmaps)
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

// A controller that does not disable is reported in the probe output, not ignored.
func TestLPSSDisableFailureReported(t *testing.T) {
	f := newFakeDW()
	f.stuck = true
	r, err := probe(t, sysOpts{}, f)
	if err == nil {
		t.Fatal("want a refusal")
	}
	text := strings.Join(r.lines, "\n")
	if !strings.Contains(text, "disable:") || !strings.Contains(text, "may still be enabled") {
		t.Errorf("report does not say the controller could not be disabled:\n%s", text)
	}
}

// A chip id read that fails halfway still leaves the controller disabled and unmapped.
func TestLPSSChipIDReadFailures(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*fakeDW)
		want string
	}{
		{"timeout", func(f *fakeDW) { f.hang = true }, "timed out"},
		{"abort that is not a NAK", func(f *fakeDW) { f.abrt = 1 << 12 }, "aborted (0x1000)"},
		{"abort with a NAK bit among others", func(f *fakeDW) { f.abrt = 1 << 3 }, "no acknowledge"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeDW()
			c.mod(f)
			_, err := probe(t, sysOpts{}, f)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
			if f.enable || f.regs[dwEnable]&1 != 0 {
				t.Errorf("IC_ENABLE is 0x%x after the failure", f.regs[dwEnable])
			}
			if f.rwMaps != 1 || f.roMaps != 1 || f.unmaps != 2 {
				t.Errorf("maps ro %d rw %d, unmaps %d, want 1, 1, 2", f.roMaps, f.rwMaps, f.unmaps)
			}
		})
	}
}

// The default lpss-probe stops after the read only checks: no register write, no writable mapping.
func TestLPSSProbeCommandReadOnly(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	fakeSys(t, sysOpts{})
	fakeMem(t, f)
	lpssReports = filepath.Join(t.TempDir(), "probe")
	t.Cleanup(func() { lpssReports = lpssReportFile })
	if lock, err := acquireLock(); err != nil {
		t.Skip("no lock file:", err)
	} else {
		_ = lock.Close()
	}

	var out, errOut strings.Builder
	if rc := Run([]string{"lpss-probe"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc %d: %s %s", rc, out.String(), errOut.String())
	}
	t.Log("\n" + out.String())
	if f.writes != 0 || f.rwMaps != 0 || f.roMaps != 1 || f.unmaps != 1 {
		t.Errorf("writes %d, writable maps %d, read only maps %d, unmaps %d:\n%v", f.writes, f.rwMaps, f.roMaps, f.unmaps, f.log)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "next:") || !strings.Contains(last, "--write") {
		t.Errorf("last line %q", last)
	}
	if strings.Contains(out.String(), "chip_id") {
		t.Error("the default probe must not read the chip id")
	}
	if saved, _ := os.ReadFile(lpssReports); string(saved) != out.String() {
		t.Error("saved report differs")
	}
	if rc := Run([]string{"lpss-probe", "--bogus"}, &out, &errOut); rc != 2 {
		t.Errorf("unknown argument: rc %d, want 2", rc)
	}
}

// Runtime PM between the D0 check and the first write: the component type is read again on the writable mapping.
func TestLPSSRecheckBeforeFirstWrite(t *testing.T) {
	f := newFakeDW()
	f.flipRW = true
	f.resp = []byte{0xb2, 0xc5}
	r, err := probe(t, sysOpts{}, f)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("want a refusal, got %v", err)
	}
	if f.writes != 0 || f.rwMaps != 1 || f.unmaps != 2 {
		t.Errorf("%d writes, %d writable maps, %d unmaps, want 0, 1, 2:\n%v", f.writes, f.rwMaps, f.unmaps, f.log)
	}
	if last := r.lines[len(r.lines)-1]; !strings.Contains(last, "refused") {
		t.Errorf("last line %q", last)
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// fakePM replaces the sysfs write: every write is logged as "<file>=<value>" and a write of "on" to power/control
// wakes the function when wake is set.
func fakePM(t *testing.T, wake bool) *[]string {
	t.Helper()
	var log []string
	lpssSysWrite = func(path, value string) error {
		log = append(log, filepath.Base(path)+"="+value)
		if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
			return err
		}
		if wake && filepath.Base(path) == "control" && value == "on" {
			return os.WriteFile(filepath.Join(filepath.Dir(path), "runtime_status"), []byte("active\n"), 0o644)
		}
		return nil
	}
	return &log
}

// rl is a report line as lpssReport.add formats it.
func rl(name, value string) string { return fmt.Sprintf("%-18s %s", name+":", value) }

func bound() sysOpts { return sysOpts{driver: "intel-lpss"} }

func probeMode(t *testing.T, o sysOpts, f *fakeDW, write bool) (*lpssReport, error) {
	t.Helper()
	fakeSys(t, o)
	fakeMem(t, f)
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, write)
	if d != nil {
		d.close()
	}
	return r, err
}

func pmControlFile(t *testing.T) string {
	t.Helper()
	return sysLine(filepath.Join(lpssSys, "bus/pci/devices/0000:00:15.0/power/control"))
}

func TestLPSSBoundAccepted(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	o := bound()
	o.pmStatus = "suspended"
	fakeSys(t, o)
	pm := fakePM(t, true)
	fakeMem(t, f)
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, true)
	if err != nil {
		t.Fatal(err, r.lines)
	}
	if got := pmControlFile(t); got != "on" {
		t.Errorf("power/control %q while the controller is open, want on", got)
	}
	d.close()
	t.Log("\n" + strings.Join(r.lines, "\n"))
	if want := []string{"control=on", "control=auto"}; strings.Join(*pm, ",") != strings.Join(want, ",") {
		t.Errorf("power/control writes %v, want %v", *pm, want)
	}
	if got := pmControlFile(t); got != "auto" {
		t.Errorf("power/control %q after close, want auto", got)
	}
	if _, err := os.Stat(lpssPMState); err == nil {
		t.Error("state file left behind")
	}
	if f.enable || f.regs[dwEnable]&1 != 0 {
		t.Error("controller left enabled")
	}
	text := strings.Join(r.lines, "\n")
	for _, want := range []string{rl("pci.driver", "intel-lpss"), rl("lpss.children", "i2c_designware.0 driver none"), rl("lpss.adapters", "-"), rl("pm.control", "auto"), rl("pm.runtime_status", "suspended"), rl("write.power", "auto -> on (saved auto)"), rl("pm.runtime_status", "active after"), rl("write.power", "restored to auto"), "intel-lpss.c:259-265"} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestLPSSBoundRefusals(t *testing.T) {
	cases := []struct {
		name string
		o    sysOpts
		want string
	}{
		{"i2c child with a driver", sysOpts{driver: "intel-lpss", children: map[string]string{"i2c_designware.0": "i2c_designware", "idma64.0": ""}}, "rule child-driver"},
		{"adapter below", sysOpts{driver: "intel-lpss", adapter: "below"}, "rule adapter"},
		{"adapter by class", sysOpts{driver: "intel-lpss", adapter: "class"}, "rule adapter: i2c-9"},
		{"other driver", sysOpts{driver: "i2c-designware-pci"}, "rule other-driver"},
	}
	for _, write := range []bool{false, true} {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s write=%v", c.name, write), func(t *testing.T) {
				f := newFakeDW()
				f.resp = []byte{0xb2, 0xc5}
				fakeSys(t, c.o)
				pm := fakePM(t, true)
				fakeMem(t, f)
				r := &lpssReport{}
				d, err := openLPSS(r, controllerAddr, write)
				if d != nil || err == nil || !strings.Contains(err.Error(), c.want) {
					t.Fatalf("want a refusal with %q, got %v", c.want, err)
				}
				if f.writes != 0 || f.rwMaps != 0 || f.roMaps != 0 || len(*pm) != 0 {
					t.Errorf("%d writes, %d writable maps, %d read only maps, sysfs writes %v", f.writes, f.rwMaps, f.roMaps, *pm)
				}
				if last := r.lines[len(r.lines)-1]; !strings.Contains(last, "refused") {
					t.Errorf("last line %q", last)
				}
			})
		}
	}
}

// Without --write nothing is written: no power/control, no register, no writable mapping.
// A bound idma64 child has its own register window: accepted, and reported.
func TestLPSSBoundIDMA64Accepted(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	o := bound()
	o.children = map[string]string{"i2c_designware.0": "", "idma64.0": "idma64"}
	fakeSys(t, o)
	fakePM(t, true)
	fakeMem(t, f)
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, true)
	if err != nil {
		t.Fatal(err, r.lines)
	}
	d.close()
	text := strings.Join(r.lines, "\n")
	if want := rl("lpss.idma64", "idma64.0 driver idma64 (own window at +0x800, not touched)"); !strings.Contains(text, want) {
		t.Errorf("report lacks %q:\n%s", want, text)
	}
}

func TestLPSSBoundReadOnly(t *testing.T) {
	f := newFakeDW()
	fakeSys(t, bound())
	pm := fakePM(t, true)
	fakeMem(t, f)
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, false)
	if d != nil || err != nil {
		t.Fatal(d, err, r.lines)
	}
	t.Log("\n" + strings.Join(r.lines, "\n"))
	if len(*pm) != 0 || f.writes != 0 || f.rwMaps != 0 || f.roMaps != 1 {
		t.Errorf("sysfs writes %v, register writes %d, rw maps %d, ro maps %d", *pm, f.writes, f.rwMaps, f.roMaps)
	}
	text := strings.Join(r.lines, "\n")
	for _, want := range []string{rl("pm.control", "auto"), rl("pm.runtime_status", "active"), "lpss.resets:", rl("result", "ok (read only checks passed, nothing written)")} {
		if !strings.Contains(text, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

// A suspended function is not woken by the read only probe: no config read, no register read, no write.
func TestLPSSBoundSuspendedReadOnly(t *testing.T) {
	f := newFakeDW()
	o := bound()
	o.pmStatus = "suspended"
	fakeSys(t, o)
	pm := fakePM(t, true)
	fakeMem(t, f)
	// An unreadable config file would be refused if it were read.
	_ = os.Remove(filepath.Join(lpssSys, "bus/pci/devices/0000:00:15.0/config"))
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, false)
	if d != nil || err != nil {
		t.Fatal(d, err, r.lines)
	}
	t.Log("\n" + strings.Join(r.lines, "\n"))
	if len(*pm) != 0 || len(f.log) != 0 || f.roMaps != 0 || f.rwMaps != 0 {
		t.Errorf("sysfs writes %v, register log %v, maps %d/%d", *pm, f.log, f.roMaps, f.rwMaps)
	}
	text := strings.Join(r.lines, "\n")
	if !strings.Contains(text, "runtime_status is suspended") || !strings.Contains(text, "does not wake") {
		t.Errorf("report does not say why it stopped:\n%s", text)
	}
	if strings.Contains(text, "pci.command") || strings.Contains(text, "lpss.resets") {
		t.Error("config space or registers were read")
	}
}

func TestLPSSBoundWriteFailureRestores(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*fakeDW)
		want string
	}{
		{"chip id mismatch", func(f *fakeDW) { f.resp = []byte{0x34, 0x12} }, "0x1234"},
		{"nak", func(f *fakeDW) { f.nak = true }, "no acknowledge"},
		{"not designware", func(f *fakeDW) { f.regs[dwCompTypeReg] = 0xffffffff }, "not a DesignWare"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeDW()
			c.mod(f)
			o := bound()
			o.pmStatus = "suspended"
			fakeSys(t, o)
			pm := fakePM(t, true)
			fakeMem(t, f)
			_, err := openLPSS(&lpssReport{}, controllerAddr, true)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q, got %v", c.want, err)
			}
			if strings.Join(*pm, ",") != "control=on,control=auto" || pmControlFile(t) != "auto" {
				t.Errorf("power/control writes %v, final %q", *pm, pmControlFile(t))
			}
			if f.enable {
				t.Error("controller left enabled")
			}
		})
	}
}

// The function does not wake within the wait: refused, nothing written to registers, power/control restored.
func TestLPSSBoundWakeTimeout(t *testing.T) {
	f := newFakeDW()
	o := bound()
	o.pmStatus = "suspended"
	fakeSys(t, o)
	pm := fakePM(t, false)
	fakeMem(t, f)
	r := &lpssReport{}
	_, err := openLPSS(r, controllerAddr, true)
	if err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("want a timeout refusal, got %v", err)
	}
	if strings.Join(*pm, ",") != "control=on,control=auto" || pmControlFile(t) != "auto" {
		t.Errorf("power/control writes %v, final %q", *pm, pmControlFile(t))
	}
	if f.writes != 0 || f.roMaps != 0 || f.rwMaps != 0 {
		t.Errorf("register access after a timeout: %v", f.log)
	}
}

// A crashed run left "on" and its state file: the saved value wins over the current "on".
func TestLPSSBoundRecoversSavedValue(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	o := bound()
	o.pmControl = "on"
	fakeSys(t, o)
	if err := os.WriteFile(lpssPMState, []byte("0000:00:15.0 auto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakePM(t, true)
	fakeMem(t, f)
	d, err := openLPSS(&lpssReport{}, controllerAddr, true)
	if err != nil {
		t.Fatal(err)
	}
	d.close()
	if got := pmControlFile(t); got != "auto" {
		t.Errorf("power/control %q, want auto", got)
	}
}

// A restore that fails is reported and keeps the state file for the next run.
func TestLPSSBoundRestoreFailureReported(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	fakeSys(t, bound())
	fakePM(t, true)
	orig := lpssSysWrite
	lpssSysWrite = func(path, value string) error {
		if value == "auto" {
			return errors.New("busy")
		}
		return orig(path, value)
	}
	fakeMem(t, f)
	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, true)
	if err != nil {
		t.Fatal(err)
	}
	d.close()
	if text := strings.Join(r.lines, "\n"); !strings.Contains(text, "power.restore:") || !strings.Contains(text, "FAILED") {
		t.Errorf("failed restore not reported:\n%s", text)
	}
	if _, err := os.Stat(lpssPMState); err != nil {
		t.Error("state file removed although the restore failed")
	}
}

// RESETS is only written when it shows the controller in reset.
func TestLPSSBoundResetsNotWrittenWhenDeasserted(t *testing.T) {
	f := newFakeDW() // RESETS 0x7
	f.resp = []byte{0xb2, 0xc5}
	fakeSys(t, bound())
	fakePM(t, true)
	fakeMem(t, f)
	d, err := openLPSS(&lpssReport{}, controllerAddr, true)
	if err != nil {
		t.Fatal(err)
	}
	d.close()
	for _, e := range f.log {
		if e == "w"+itoa(lpssPrivResets) {
			t.Fatalf("RESETS written although deasserted:\n%v", f.log)
		}
	}
}

// No register is written before the last check: power/control comes first, the first register write follows the
// component type reads.
func TestLPSSBoundWriteOrder(t *testing.T) {
	f := newFakeDW()
	f.resp = []byte{0xb2, 0xc5}
	f.regs[dwCompTypeReg] = 0xffffffff // checks fail
	fakeSys(t, bound())
	fakePM(t, true)
	fakeMem(t, f)
	if _, err := openLPSS(&lpssReport{}, controllerAddr, true); err == nil {
		t.Fatal("want a refusal")
	}
	if f.writes != 0 || f.rwMaps != 0 {
		t.Errorf("%d register writes, %d writable maps before the checks passed", f.writes, f.rwMaps)
	}
}
