//go:build linux

package chip

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// The Linux intel-lpss MFD driver can own the PCI function while no I2C driver exists below it (Unraid ships
// intel-lpss-pci.ko but no i2c_designware). That one state is accepted; see docs/led.md.
//
// Facts from the Linux 6.18 sources:
//   - the PCI driver is named "intel-lpss" (intel-lpss-pci.c:654);
//   - intel_lpss_probe registers the children with mfd_add_devices: "idma64" (intel-lpss.c:441) and the cell
//     "i2c_designware" (:119, :448), which appear as <pci device>/<name>.<id> platform devices;
//   - intel-lpss-pci.c:83-84 ends the probe with pm_runtime_put + pm_runtime_allow, so power/control is "auto" and the
//     function runtime suspends (intel_lpss_suspend writes RESETS 0, intel-lpss.c:514; the runtime resume callback
//     intel_lpss_resume deasserts it again, :524, via RUNTIME_PM_OPS at :536);
//   - power/control "on" keeps the device from being power managed and wakes a suspended one, runtime_status shows
//     "active" (Documentation/ABI/testing/sysfs-devices-power:35-50 and :264-270; Documentation/power/runtime_pm.rst:633-640);
//   - reading the sysfs "config" file resumes a suspended PCI function (pci-sysfs.c:763 pci_read_config), so the read
//     only probe does not read it while the function is not active.
const (
	lpssBoundDriver = "intel-lpss"
	lpssPMOn        = "on"
	lpssPMAuto      = "auto"
)

// Replaced in tests.
var (
	lpssPMState  = "/var/run/ugreen-nas-control.lpss-power" // "<pci address> <saved power/control>" while it is "on"
	lpssPMWait   = time.Second
	lpssPMPoll   = 10 * time.Millisecond
	lpssSysWrite = writeSysfs
	mfdChildName = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[0-9]+$`)
)

func writeSysfs(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// lpssChildren lists the MFD child devices of the PCI function with the driver bound to each ("" when none).
func lpssChildren(dev string) (names, drivers []string) {
	entries, _ := os.ReadDir(dev)
	for _, e := range entries {
		if !e.IsDir() || !mfdChildName.MatchString(e.Name()) {
			continue
		}
		drv := ""
		if target, err := os.Readlink(filepath.Join(dev, e.Name(), "driver")); err == nil {
			drv = filepath.Base(target)
		}
		names, drivers = append(names, e.Name()), append(drivers, drv)
	}
	return names, drivers
}

// lpssAdapters lists the I2C adapters below the PCI function: i2c-N directories in it or in a child, and every
// adapter or /dev/i2c-N class device whose parent chain leads to it.
func lpssAdapters(dev string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, pattern := range []string{"i2c-*", "*/i2c-*"} {
		m, _ := filepath.Glob(filepath.Join(dev, pattern))
		for _, p := range m {
			add(filepath.Base(p))
		}
	}
	resolved, err := filepath.EvalSymlinks(dev)
	if err != nil {
		resolved = dev
	}
	for _, class := range []string{"class/i2c-adapter", "class/i2c-dev"} {
		m, _ := filepath.Glob(filepath.Join(lpssSys, class, "*"))
		for _, p := range m {
			if target, err := filepath.EvalSymlinks(p); err == nil && strings.HasPrefix(target, resolved+string(filepath.Separator)) {
				add(filepath.Base(p))
			}
		}
	}
	return out
}

// lpssCheckBound decides whether the one bound driver, intel-lpss, is the harmless state: no i2c_designware.N child
// has a driver and no I2C adapter exists below the function. The idma64.N child may be bound: it has its own register
// window at +0x800, size 0x800 (intel-lpss.c:46-47,102-114), separate from the I2C registers used here. It records the
// facts and returns the refusal naming the rule.
func lpssCheckBound(r *lpssReport, dev string) error {
	names, drivers := lpssChildren(dev)
	var parts []string
	for i, n := range names {
		drv := drivers[i]
		if drv == "" {
			drv = "none"
		}
		if strings.HasPrefix(n, "idma64.") {
			r.add("lpss.idma64", "%s driver %s (own window at +0x800, not touched)", n, drv)
			continue
		}
		parts = append(parts, n+" driver "+drv)
	}
	r.add("lpss.children", "%s", orNone(strings.Join(parts, ", ")))
	adapters := lpssAdapters(dev)
	r.add("lpss.adapters", "%s", orNone(strings.Join(adapters, ", ")))
	for i, n := range names {
		if strings.HasPrefix(n, "i2c_designware.") && drivers[i] != "" {
			return r.refuse("rule child-driver: %s of %s is bound to %s", n, filepath.Base(dev), drivers[i])
		}
	}
	if len(adapters) > 0 {
		return r.refuse("rule adapter: %s exists below %s, the kernel owns the controller", strings.Join(adapters, ", "), filepath.Base(dev))
	}
	r.add("lpss.exception", "%s is bound, no i2c_designware child driver and no I2C adapter below %s", lpssBoundDriver, filepath.Base(dev))
	return nil
}

// lpssPM holds the saved power/control value of the PCI function while the plugin keeps it "on".
type lpssPM struct {
	r       *lpssReport
	ctl     string
	saved   string
	changed bool
}

// enable saves power/control, writes "on" and waits up to lpssPMWait for runtime_status "active". The saved value
// is kept in lpssPMState until restore; a value left there by a crashed run wins over the current "on".
func (p *lpssPM) enable(dev, current string) error {
	bdf := filepath.Base(dev)
	p.ctl = filepath.Join(dev, "power/control")
	saved := current
	if data, err := os.ReadFile(lpssPMState); err == nil {
		if f := strings.Fields(string(data)); len(f) == 2 && f[0] == bdf && (f[1] == lpssPMAuto || f[1] == lpssPMOn) {
			saved = f[1]
			p.r.add("pm.recovered", "power/control %s was saved by an earlier run", saved)
		}
	}
	if saved != lpssPMAuto && saved != lpssPMOn {
		return fmt.Errorf("power/control of %s reads %q, expected auto or on", bdf, saved)
	}
	if err := os.WriteFile(lpssPMState, []byte(bdf+" "+saved+"\n"), 0o644); err != nil { //nolint:gosec // fixed path under /var/run
		return fmt.Errorf("cannot save power/control: %w", err)
	}
	p.saved, p.changed = saved, true
	if err := lpssSysWrite(p.ctl, lpssPMOn); err != nil {
		return fmt.Errorf("cannot set power/control of %s to on: %w", bdf, err)
	}
	p.r.add("write.power", "%s -> on (saved %s)", current, saved)
	status, start := "", time.Now()
	for {
		if status = sysLine(filepath.Join(dev, "power/runtime_status")); status == "active" {
			p.r.add("pm.runtime_status", "active after %d ms", time.Since(start).Milliseconds())
			return nil
		}
		if time.Since(start) >= lpssPMWait {
			return fmt.Errorf("%s is not active %s after power/control on (runtime_status %q)", bdf, lpssPMWait, status)
		}
		time.Sleep(lpssPMPoll)
	}
}

// restore writes the saved value back; safe to call more than once. A failed write keeps the state file, so the next
// run restores it.
func (p *lpssPM) restore() {
	if !p.changed {
		return
	}
	p.changed = false
	if err := lpssSysWrite(p.ctl, p.saved); err != nil {
		p.r.add("power.restore", "FAILED: %v (power/control stays on; the next run restores %s)", err, p.saved)
		return
	}
	_ = os.Remove(lpssPMState)
	p.r.add("write.power", "restored to %s", p.saved)
}
