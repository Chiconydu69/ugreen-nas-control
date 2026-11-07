package nasctl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// I2CCtl is one PCI device that can host an I2C or SMBus adapter: Intel Serial IO I2C (class 0x0c80) or SMBus (0x0c05).
type I2CCtl struct {
	BDF, ID, Class, Driver string
	Adapters               []string // "/dev/i2c-5 (Synopsys DesignWare I2C adapter)"
}

// ACPIMCU is an ACPI device the LED MCU announces itself as (HID HT32...), with the PCI device it sits behind.
type ACPIMCU struct {
	ID, HID, Path, Status, Parent string
}

// I2CModule is a DesignWare module: whether the running kernel has it and whether it is loaded.
type I2CModule struct {
	Name           string
	Exists, Loaded bool
	Kind           string // "module", "built in" or ""
}

// I2CDiag is the read-only map of where the LED MCU can sit; nothing here opens a bus or brings a controller up.
type I2CDiag struct {
	Controllers []I2CCtl
	MCUs        []ACPIMCU
	Modules     []I2CModule
	Release     string
	Probe       []string // report of the last ugreen-led-i2c lpss-probe, empty when it was not run
	State       string   // state of the default LPSS LED path in this boot ("ok", "refused: <reason>"), empty before the first use
}

var (
	bdfRe       = regexp.MustCompile(`[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]`)
	i2cModNames = []string{"i2c_designware_platform", "i2c_designware_pci", "i2c_designware_core"}
)

func readAll(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

func sysfsLine(path string) string { return strings.TrimSpace(readFirstLine(path)) }

// i2cControllers lists the class 0x0c80/0x0c05 PCI devices with their driver and the adapters that hang under them.
func (c *Ctl) i2cControllers() []I2CCtl {
	dirs, _ := filepath.Glob(filepath.Join(c.Env.SysPCI, "*"))
	adapters, _ := filepath.Glob(filepath.Join(c.Env.SysI2C, "i2c-*"))
	var out []I2CCtl
	for _, d := range dirs {
		class := sysfsLine(filepath.Join(d, "class"))
		if !strings.HasPrefix(class, "0x0c80") && !strings.HasPrefix(class, "0x0c05") {
			continue
		}
		bdf := filepath.Base(d)
		x := I2CCtl{BDF: bdf, Class: class[:6], Driver: "none",
			ID: strings.TrimPrefix(sysfsLine(filepath.Join(d, "vendor")), "0x") + ":" + strings.TrimPrefix(sysfsLine(filepath.Join(d, "device")), "0x")}
		if l, err := os.Readlink(filepath.Join(d, "driver")); err == nil {
			x.Driver = filepath.Base(l)
		}
		for _, a := range adapters {
			if resolved, err := filepath.EvalSymlinks(a); err == nil && strings.Contains(resolved+"/", "/"+bdf+"/") {
				x.Adapters = append(x.Adapters, "/dev/"+filepath.Base(a)+" ("+orDash(sysfsLine(filepath.Join(a, "name")))+")")
			}
		}
		sort.Slice(x.Adapters, func(i, j int) bool { return natural(x.Adapters[i]) < natural(x.Adapters[j]) })
		out = append(out, x)
	}
	return out
}

// i2cMCUs lists the ACPI devices with an HT32 HID. The parent is the last PCI address on the path of the
// physical node (or the firmware node link the other way round).
func (c *Ctl) i2cMCUs() []ACPIMCU {
	dirs, _ := filepath.Glob(filepath.Join(c.Env.SysACPI, "*"))
	var out []ACPIMCU
	for _, d := range dirs {
		hid := sysfsLine(filepath.Join(d, "hid"))
		if !strings.HasPrefix(strings.ToUpper(hid), "HT32") {
			continue
		}
		m := ACPIMCU{ID: filepath.Base(d), HID: hid, Path: orDash(sysfsLine(filepath.Join(d, "path"))), Status: orDash(sysfsLine(filepath.Join(d, "status")))}
		if resolved, err := filepath.EvalSymlinks(filepath.Join(d, "physical_node")); err == nil {
			if all := bdfRe.FindAllString(resolved, -1); len(all) > 0 {
				m.Parent = all[len(all)-1]
			}
		}
		if m.Parent == "" {
			m.Parent = c.pciByACPIPath(m.Path)
		}
		out = append(out, m)
	}
	return out
}

// pciByACPIPath is the PCI function whose firmware node is the parent of the ACPI path (\_SB_.PC00.I2C1.MCU0 ->
// \_SB_.PC00.I2C1), or "". The boards without a driver have no physical_node to follow.
func (c *Ctl) pciByACPIPath(path string) string {
	i := strings.LastIndexByte(path, '.')
	if i <= 0 {
		return ""
	}
	dirs, _ := filepath.Glob(filepath.Join(c.Env.SysPCI, "*"))
	for _, d := range dirs {
		if sysfsLine(filepath.Join(d, "firmware_node", "path")) == path[:i] {
			return filepath.Base(d)
		}
	}
	return ""
}

// i2cModules says per DesignWare module whether /lib/modules/<release> has it (modules.dep) or the kernel has it
// built in (modules.builtin), and whether it is loaded (/proc/modules or /sys/module).
func (c *Ctl) i2cModules(release string) []I2CModule {
	dir := filepath.Join(c.Env.LibModules, release)
	norm := func(s string) string { return strings.ReplaceAll(s, "-", "_") }
	have := func(file string) map[string]bool {
		set := map[string]bool{}
		data, _ := os.ReadFile(filepath.Join(dir, file))
		for _, line := range strings.Split(string(data), "\n") {
			p, _, _ := strings.Cut(line, ":")
			base := filepath.Base(strings.TrimSpace(p))
			base, _, _ = strings.Cut(base, ".ko")
			if base != "" {
				set[norm(base)] = true
			}
		}
		return set
	}
	dep, builtin := have("modules.dep"), have("modules.builtin")
	loaded := moduleNames(c.Env.ProcModules)
	var out []I2CModule
	for _, n := range i2cModNames {
		m := I2CModule{Name: n}
		switch {
		case dep[n]:
			m.Exists, m.Kind = true, "module"
		case builtin[n]:
			m.Exists, m.Kind = true, "built in"
		}
		m.Loaded = builtin[n]
		for _, l := range loaded {
			if norm(l) == n {
				m.Loaded = true
			}
		}
		out = append(out, m)
	}
	return out
}

func (c *Ctl) i2cDiag() I2CDiag {
	rel := sysfsLine(filepath.Join(c.Env.Proc, "sys/kernel/osrelease"))
	d := I2CDiag{Release: rel, Controllers: c.i2cControllers(), MCUs: c.i2cMCUs()}
	d.State = strings.TrimSpace(readAll(c.Env.LPSSState))
	if probe := strings.TrimSpace(readAll(c.Env.LPSSProbe)); probe != "" {
		d.Probe = strings.Split(probe, "\n")
	}
	if rel != "" {
		d.Modules = c.i2cModules(rel)
	}
	return d
}

// unbound is the first I2C or SMBus controller without a driver, or "".
func (d I2CDiag) unbound() string {
	for _, x := range d.Controllers {
		if x.Driver == "none" {
			return x.BDF
		}
	}
	return ""
}

// ledHint is the one factual line for a failed 0x3a probe: the MCU is probably behind a controller without a driver.
func (d I2CDiag) ledHint() string {
	const tail = "the LED controller probably sits behind it (DXP2800 SUB2 boards: the plugin tries that Intel LPSS controller by itself; ugreen-led-i2c lpss-probe shows each check, lpss_leds=\"0\" in leds.cfg turns it off)"
	if b := d.unbound(); b != "" {
		return "no LED controller at 0x3a on the SMBus; the I2C controller " + b + " has no driver bound, " + tail
	}
	if len(d.MCUs) > 0 {
		m := d.MCUs[0]
		if m.Parent == "" {
			return "no LED controller at 0x3a on the SMBus; the ACPI device " + m.HID + " exists, " + tail
		}
		return "no LED controller at 0x3a on the SMBus; the ACPI device " + m.HID + " sits behind the I2C controller " + m.Parent + ", " + tail
	}
	return ""
}

func (d I2CDiag) lines() []string {
	l := []string{"i2c controllers (PCI class 0c80 Serial IO, 0c05 SMBus; read only):"}
	for _, x := range d.Controllers {
		l = append(l, fmt.Sprintf("  %s %s class %s driver %s adapters %s", x.BDF, x.ID, x.Class, x.Driver, orDash(strings.Join(x.Adapters, ", "))))
	}
	if len(d.Controllers) == 0 {
		l = append(l, "  -")
	}
	l = append(l, "acpi devices HT32* (LED MCU):")
	for _, m := range d.MCUs {
		l = append(l, fmt.Sprintf("  %s hid %s path %s status %s controller %s", m.ID, m.HID, m.Path, m.Status, orDash(m.Parent)))
	}
	if len(d.MCUs) == 0 {
		l = append(l, "  -")
	}
	l = append(l, "designware modules for kernel "+orDash(d.Release)+":")
	for _, m := range d.Modules {
		state := "absent"
		if m.Exists {
			state = m.Kind
		}
		l = append(l, fmt.Sprintf("  %s: %s, loaded %t", m.Name, state, m.Loaded))
	}
	if len(d.Modules) == 0 {
		l = append(l, "  -")
	}
	if d.State != "" {
		l = append(l, "lpss led path (this boot): "+d.State)
	}
	if len(d.Probe) > 0 {
		l = append(l, "lpss-probe (last run):")
		for _, p := range d.Probe {
			l = append(l, "  "+p)
		}
	}
	return l
}
