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
)

// DXP2800 "SUB2" boards (BIOS EM_DXP2800S_SUB2_*, miskcoo/ugreen_leds_controller#115, issue #214): the LED MCU
// (ACPI HT32F522, \_SB_.PC00.I2C1.MCU0) sits behind an Intel Alder Lake-N LPSS DesignWare I2C controller. Unraid's
// kernel has no i2c_designware module, so the controller is driven through /dev/mem, like the AMD one in
// dwmem_linux.go. Off by default (lpss_leds="1" in leds.cfg); `ugreen-led-i2c lpss-probe` is read only, `lpss-probe --write` needs no opt-in.
//
// Order: (1) read only: ACPI -> PCI function, no driver bound, memory decoding on, D0, BAR0, DesignWare component
// type, all through sysfs and a read-only mapping; (2) only when every check passed: take the controller out of
// reset if it is in reset, set the SCL counts, enable it, read word register 0x5A at 0x3a; (3) no LED frame
// unless that returns 0xC5B2, else IC_ENABLE=0 and report.
//
// Facts from the Linux 6.18 sources (drivers/mfd/intel-lpss.c, intel-lpss-pci.c, i2c/busses/i2c-designware-*.c):
//   - private registers at BAR0+0x200 (intel-lpss.c:43); RESETS at priv+0x04 (:50), function 0x3 and iDMA 0x4
//     (:51-52), written 0x7 to take the device out of reset (intel_lpss_deassert_reset, :259-265; init_dev writes 0
//     first, :267-274);
//   - 00:15.0/.1 (54e8, 54e9) and 00:19.0/.1 (54c5, 54c6) are bxt_i2c_info (intel-lpss-pci.c:416-420): 133 MHz
//     input clock (:173-176), SDA hold 42 ns, SDA falling 171 ns, SCL falling 208 ns (:162-166);
//   - the clock divider and its gate are registered only for non-I2C devices (intel-lpss.c:358), so nothing is
//     written to priv+0x00 (CLK_PARAMS) here;
//   - standard mode counts: HCNT = round(clk_khz*(4000+sda_fall)/1e6)-3, LCNT = round(clk_khz*(4700+scl_fall)/1e6)-1
//     (i2c-designware-master.c:66-83, -common.c:417-458); SDA hold round(clk_khz*ns/1e6) (-platdrv.c:281-283) with
//     RX hold 1 (-common.c:488-491).
const (
	lpssPath     = "/dev/mem#lpss" // bus path of this controller in ugreen-led-i2c
	lpssOptInKey = "lpss_leds"

	lpssPrivResets  = 0x204 // LPSS_PRIV_OFFSET 0x200 + LPSS_PRIV_RESETS 0x04
	lpssResetsFunc  = 0x3
	lpssResetsIDMA  = 0x4
	lpssClockKHz    = 133000
	lpssSDAFallNS   = 171
	lpssSCLFallNS   = 208
	lpssSDAHoldNS   = 42
	lpssClass       = "0x0c80"
	lpssChipReg     = 0x5a   // chip id register, read as a word (leds-mcu.c:41,59,716-717,761-791@c75c5ab)
	lpssChipID      = 0xc5b2 // expected value (same lines)
	lpssReportFile  = "/var/run/ugreen-nas-control.lpss-probe"
	lpssMaxPMWalk   = 48
	lpssPMCapID     = 0x01
	lpssPCICommand  = 0x04
	lpssPCIStatus   = 0x06
	lpssPCICapPtr   = 0x34
	lpssCommandMem  = 1 << 1
	lpssStatusCapOK = 1 << 4
)

// Replaced in tests.
var (
	lpssSys     = "/sys"
	lpssMapMem  = devMemMap
	lpssReports = lpssReportFile
)

var errLPSSOff = errors.New(`the DXP2800 SUB2 LED path is off: run "ugreen-led-i2c lpss-probe" and set lpss_leds="1" in leds.cfg to try it (untested on hardware)`)

// lpssTiming is the standard mode setup the Linux driver computes for the 133 MHz LPSS clock.
func lpssTiming() dwTiming {
	round := func(num int) uint32 { return uint32((num + 500000) / 1000000) } // DIV_ROUND_CLOSEST by 1e6
	return dwTiming{
		hcnt:    round(lpssClockKHz*(4000+lpssSDAFallNS)) - 3,
		lcnt:    round(lpssClockKHz*(4700+lpssSCLFallNS)) - 1,
		sdaHold: round(lpssClockKHz * lpssSDAHoldNS),
	}
}

// devMemMap maps one page of /dev/mem; unmap also closes the file. A read-only mapping is opened O_RDONLY.
func devMemMap(phys int64, write bool) (dwRegs, func(), error) {
	flags, mapper := os.O_RDONLY|os.O_SYNC, mapPhysRO
	if write {
		flags, mapper = os.O_RDWR|os.O_SYNC, mapPhys
	}
	f, err := os.OpenFile(dwPath, flags, 0)
	if err != nil {
		return nil, nil, err
	}
	b, err := mapper(f, phys)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return mmio(b), func() { _ = syscall.Munmap(b); _ = f.Close() }, nil
}

// lpssReport collects the probe output, one "name value" line per check.
type lpssReport struct{ lines []string }

func (r *lpssReport) add(name, format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf("%-18s %s", name+":", fmt.Sprintf(format, args...)))
}

// refuse records the refusal and returns it.
func (r *lpssReport) refuse(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	r.add("result", "refused: %v", err)
	return err
}

func sysLine(path string) string {
	data, _ := os.ReadFile(path)
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

// lpssMCU is the ACPI device HT32*: its path and the path of the controller above it.
func lpssMCU() (id, hid, path, status, controller string, ok bool) {
	dirs, _ := filepath.Glob(filepath.Join(lpssSys, "bus/acpi/devices/*"))
	for _, d := range dirs {
		hid = sysLine(filepath.Join(d, "hid"))
		if !strings.HasPrefix(strings.ToUpper(hid), "HT32") {
			continue
		}
		path = sysLine(filepath.Join(d, "path"))
		if i := strings.LastIndexByte(path, '.'); i > 0 {
			controller = path[:i]
		}
		return filepath.Base(d), hid, path, sysLine(filepath.Join(d, "status")), controller, true
	}
	return "", "", "", "", "", false
}

// lpssHasMCU is the cheap check findBus uses.
func lpssHasMCU() bool {
	_, _, _, _, controller, ok := lpssMCU()
	return ok && controller != ""
}

func le16(b []byte, off int) int { return int(b[off]) | int(b[off+1])<<8 }

// pciPowerState reads the PMCSR power state (0 = D0) from the config space; ok is false without a PM capability.
func pciPowerState(cfg []byte) (state int, ok bool) {
	if len(cfg) < 0x40 || le16(cfg, lpssPCIStatus)&lpssStatusCapOK == 0 {
		return 0, false
	}
	ptr := int(cfg[lpssPCICapPtr]) &^ 3
	for i := 0; i < lpssMaxPMWalk && ptr >= 0x40 && ptr+6 <= len(cfg); i++ {
		if cfg[ptr] == lpssPMCapID {
			return le16(cfg, ptr+4) & 3, true
		}
		ptr = int(cfg[ptr+1]) &^ 3
	}
	return 0, false
}

// openLPSS runs the checks and, with write, the bring-up described above and returns the controller with the chip
// id confirmed. Without write it stops after the read only checks and returns nil, nil. Every step is recorded in r.
// Nothing is written before the last read only check passed.
func openLPSS(r *lpssReport, addr uint8, write bool) (*dwI2C, error) {
	id, hid, path, status, controller, ok := lpssMCU()
	if !ok {
		return nil, r.refuse("no ACPI device with a HT32* hid (this is not a SUB2 board)")
	}
	r.add("acpi.device", "%s (hid %s, status %s)", id, hid, orNone(status))
	r.add("acpi.path", "%s", path)
	if controller == "" {
		return nil, r.refuse("ACPI path %q has no parent controller", path)
	}
	r.add("acpi.controller", "%s", controller)

	devs, _ := filepath.Glob(filepath.Join(lpssSys, "bus/pci/devices/*"))
	var match []string
	for _, d := range devs {
		if sysLine(filepath.Join(d, "firmware_node/path")) == controller {
			match = append(match, d)
		}
	}
	if len(match) != 1 {
		return nil, r.refuse("%d PCI functions have the ACPI path %s, need exactly one", len(match), controller)
	}
	dev := match[0]
	vendor, device, class := sysLine(dev+"/vendor"), sysLine(dev+"/device"), sysLine(dev+"/class")
	r.add("pci.device", "%s [%s:%s] class %s", filepath.Base(dev), strings.TrimPrefix(vendor, "0x"), strings.TrimPrefix(device, "0x"), class)
	if vendor != "0x8086" || !strings.HasPrefix(class, lpssClass) {
		return nil, r.refuse("%s is not an Intel LPSS I2C controller (vendor 8086, class %s)", filepath.Base(dev), lpssClass)
	}

	if target, err := os.Readlink(dev + "/driver"); err == nil {
		r.add("pci.driver", "%s", filepath.Base(target))
		return nil, r.refuse("the driver %s is bound to %s", filepath.Base(target), filepath.Base(dev))
	}
	r.add("pci.driver", "none")

	cfg, err := os.ReadFile(dev + "/config")
	if err != nil || len(cfg) < 0x40 {
		return nil, r.refuse("cannot read the PCI config space of %s", filepath.Base(dev))
	}
	command := le16(cfg, lpssPCICommand)
	r.add("pci.command", "0x%04x (memory decoding %s)", command, map[bool]string{true: "on", false: "off"}[command&lpssCommandMem != 0])
	if command&lpssCommandMem == 0 {
		return nil, r.refuse("memory decoding of %s is off", filepath.Base(dev))
	}
	state, ok := pciPowerState(cfg)
	if !ok {
		r.add("pci.power_state", "unknown (no power management capability)")
		return nil, r.refuse("cannot read the power state of %s", filepath.Base(dev))
	}
	r.add("pci.power_state", "D%d", state)
	if state != 0 {
		return nil, r.refuse("%s is in D%d, not D0 (the plugin does not change the power state)", filepath.Base(dev), state)
	}

	var start, end, flags int64
	first, _, _ := strings.Cut(string(mustRead(dev+"/resource")), "\n")
	if f := strings.Fields(first); len(f) == 3 {
		start, _ = strconv.ParseInt(strings.TrimPrefix(f[0], "0x"), 16, 64)
		end, _ = strconv.ParseInt(strings.TrimPrefix(f[1], "0x"), 16, 64)
		flags, _ = strconv.ParseInt(strings.TrimPrefix(f[2], "0x"), 16, 64)
	}
	r.add("pci.bar0", "0x%x-0x%x flags 0x%x", start, end, flags)
	if start == 0 || end-start+1 < 0x1000 || flags&1 != 0 {
		return nil, r.refuse("BAR0 of %s is not a memory region of at least 4 KiB", filepath.Base(dev))
	}

	ro, unmapRO, err := lpssMapMem(start, false)
	if err != nil {
		return nil, r.refuse("cannot map BAR0 read only: %v", err)
	}
	resets := ro.load(lpssPrivResets)
	comp, version := ro.load(dwCompTypeReg), ro.load(dwCompVersion)
	unmapRO()
	r.add("lpss.resets", "0x%08x", resets)
	r.add("dw.comp_version", "0x%08x", version)
	r.add("dw.comp_type", "0x%08x (expected 0x%08x)", comp, dwCompType)
	if comp != dwCompType {
		hint := ""
		if resets&lpssResetsFunc != lpssResetsFunc {
			hint = " (the controller is held in reset)"
		}
		return nil, r.refuse("not a DesignWare I2C controller%s", hint)
	}

	if !write {
		r.add("result", "ok (read only checks passed, nothing written)")
		r.add("next", "run with --write to set up the controller and read the LED chip id (writes controller registers, not the LED chip)")
		return nil, nil
	}

	// All read only checks passed: from here on registers are written.
	regs, unmap, err := lpssMapMem(start, true)
	if err != nil {
		return nil, r.refuse("cannot map BAR0: %v", err)
	}
	// PCI runtime PM could have changed the power state since the D0 check: read the component type again before
	// the first write and leave the controller alone when it no longer answers. Nothing is written to undo it.
	if t := regs.load(dwCompTypeReg); t != dwCompType {
		unmap()
		r.add("dw.comp_type", "0x%08x on the writable mapping (expected 0x%08x)", t, dwCompType)
		return nil, r.refuse("component type changed to 0x%08x before the first write", t)
	}
	d := &dwI2C{mem: regs, unmap: unmap, addr: addr, timing: lpssTiming()}
	// Teardown leaves RESETS as it is: intel_lpss_remove (intel-lpss.c:469-479.18) does not touch it either; only
	// suspend writes 0 (:499-515). A failed disable is reported, not retried.
	fail := func(format string, args ...any) (*dwI2C, error) {
		if err := d.shutdown(); err != nil {
			r.add("disable", "FAILED: %v (the controller may still be enabled)", err)
		}
		return nil, r.refuse(format, args...)
	}

	if resets&(lpssResetsFunc|lpssResetsIDMA) != lpssResetsFunc|lpssResetsIDMA {
		regs.store(lpssPrivResets, lpssResetsFunc|lpssResetsIDMA)
		r.add("write.resets", "0x%08x -> 0x%08x", resets, regs.load(lpssPrivResets))
		if t := regs.load(dwCompTypeReg); t != dwCompType {
			return fail("component type is 0x%08x after taking the controller out of reset", t)
		}
	} else {
		r.add("write.resets", "not needed")
	}

	if err := d.init(); err != nil {
		return fail("controller setup: %v", err)
	}
	r.add("write.timing", "SS_SCL_HCNT %d, SS_SCL_LCNT %d, SDA_HOLD 0x%x (133 MHz, standard mode)", d.timing.hcnt, d.timing.lcnt, d.rd(dwSDAHold))

	word := make([]byte, 2)
	if err := d.xfer([]dwMsg{{buf: []byte{lpssChipReg}}, {read: true, buf: word}}); err != nil {
		r.add("chip_id", "0x%02x register 0x%02x: %v", addr, lpssChipReg, err)
		return fail("no chip id from 0x%02x: %v", addr, err)
	}
	got := int(word[0]) | int(word[1])<<8
	r.add("chip_id", "0x%04x (expected 0x%04x) at 0x%02x register 0x%02x", got, lpssChipID, addr, lpssChipReg)
	if got != lpssChipID {
		return fail("chip id 0x%04x is not 0x%04x", got, lpssChipID)
	}
	return d, nil
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func mustRead(path string) []byte {
	data, _ := os.ReadFile(path)
	return data
}

// runLPSSProbe is "ugreen-led-i2c lpss-probe [--write]": the read only checks, printed one per line; with --write
// also the bring-up and the chip id read. It needs no opt-in; the report is kept for diag.
func (c *cli) runLPSSProbe(write bool) int {
	lock, err := acquireLock()
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = lock.Close() }()

	r := &lpssReport{}
	d, err := openLPSS(r, controllerAddr, write)
	if err == nil && d != nil {
		if derr := d.shutdown(); derr != nil {
			r.add("disable", "FAILED: %v (the controller may still be enabled)", derr)
			r.add("result", "failed: the controller could not be disabled")
			err = derr
		} else {
			r.add("result", "ok (the chip id matches; LED control needs lpss_leds=\"1\" in leds.cfg)")
		}
	}

	text := strings.Join(r.lines, "\n") + "\n"
	_, _ = fmt.Fprint(c.out, text)
	_ = os.WriteFile(lpssReports, []byte(text), 0o644) //nolint:gosec // plain text for diag
	if err != nil {
		return 1
	}
	return 0
}

// lpssI801Answers reports whether the SMBus at path has a device at the controller address. Only a NAK (ENXIO, or
// EREMOTEIO from i2c_smbus_xfer) says "no device"; any other failure (EBUSY, ETIMEDOUT, ...) counts as "yes", so the
// SMBus path is never given up on a doubt.
func lpssI801Answers(path string) bool {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return true
	}
	defer func() { _ = file.Close() }()
	if err := ioctl(file.Fd(), ioctlSlave, controllerAddr); err != nil {
		return true // busy (a kernel driver holds it) or not testable
	}
	var data [smbusBlockMax + 2]byte
	return !lpssNoDevice(smbusIoctl(file, smbusRead, 0, smbusByte, &data))
}

// lpssNoDevice is true for the errors a missing device produces: a NAK on the address.
func lpssNoDevice(err error) bool {
	return errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.EREMOTEIO)
}
