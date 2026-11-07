package nasctl

import (
	"os"
	"strings"
	"testing"
)

// i2cFixture: a bound SMBus (I801), an unbound Serial IO I2C controller at 00:15.0 with the HT32 device behind it,
// a bound Serial IO controller at 00:15.1, and an unrelated PCI device.
func i2cFixture(t *testing.T, withMCU bool) *Ctl {
	dir := t.TempDir()
	env := Env{
		SysPCI: dir + "/pci", SysACPI: dir + "/acpi", SysI2C: dir + "/i2c", LibModules: dir + "/lib", Proc: dir + "/proc",
		ProcModules: dir + "/proc/modules",
	}
	pci := func(bdf, class, vendor, device string) {
		write(t, env.SysPCI+"/"+bdf+"/class", class+"\n")
		write(t, env.SysPCI+"/"+bdf+"/vendor", vendor+"\n")
		write(t, env.SysPCI+"/"+bdf+"/device", device+"\n")
	}
	pci("0000:00:1f.4", "0x0c0500", "0x8086", "0x51a3")
	pci("0000:00:15.0", "0x0c8000", "0x8086", "0x51e8")
	pci("0000:00:15.1", "0x0c8000", "0x8086", "0x51e9")
	pci("0000:00:02.0", "0x030000", "0x8086", "0x4680")
	write(t, dir+"/drivers/i801_smbus/x", "")
	write(t, dir+"/drivers/intel-lpss/x", "")
	for bdf, drv := range map[string]string{"0000:00:1f.4": "i801_smbus", "0000:00:15.1": "intel-lpss"} {
		if err := os.Symlink(dir+"/drivers/"+drv, env.SysPCI+"/"+bdf+"/driver"); err != nil {
			t.Fatal(err)
		}
	}
	write(t, env.SysPCI+"/0000:00:1f.4/i2c-0/name", "SMBus I801 adapter at efa0\n")
	write(t, env.SysPCI+"/0000:00:15.1/i2c_designware.1/i2c-3/name", "Synopsys DesignWare I2C adapter\n")
	os.MkdirAll(env.SysI2C, 0o755)
	os.Symlink(env.SysPCI+"/0000:00:1f.4/i2c-0", env.SysI2C+"/i2c-0")
	os.Symlink(env.SysPCI+"/0000:00:15.1/i2c_designware.1/i2c-3", env.SysI2C+"/i2c-3")
	if withMCU {
		write(t, env.SysPCI+"/0000:00:15.0/i2c_designware.0/i2c-HT32F522:00/x", "")
		write(t, env.SysACPI+"/HT32F522:00/hid", "HT32F522\n")
		write(t, env.SysACPI+"/HT32F522:00/path", "\\_SB_.PC00.I2C0.MCU_\n")
		write(t, env.SysACPI+"/HT32F522:00/status", "15\n")
		os.Symlink(env.SysPCI+"/0000:00:15.0/i2c_designware.0/i2c-HT32F522:00", env.SysACPI+"/HT32F522:00/physical_node")
		write(t, env.SysACPI+"/PNP0C0A:00/hid", "PNP0C0A\n")
	}
	write(t, env.Proc+"/sys/kernel/osrelease", "6.12.24-Unraid\n")
	return &Ctl{Env: env}
}

func TestI2CControllers(t *testing.T) {
	d := i2cFixture(t, true).i2cDiag()
	if len(d.Controllers) != 3 {
		t.Fatalf("want 3 controllers (class 0c80 and 0c05 only), got %+v", d.Controllers)
	}
	by := map[string]I2CCtl{}
	for _, x := range d.Controllers {
		by[x.BDF] = x
	}
	if x := by["0000:00:15.0"]; x.Driver != "none" || x.ID != "8086:51e8" || len(x.Adapters) != 0 {
		t.Errorf("unbound controller: %+v", x)
	}
	if x := by["0000:00:15.1"]; x.Driver != "intel-lpss" || len(x.Adapters) != 1 || x.Adapters[0] != "/dev/i2c-3 (Synopsys DesignWare I2C adapter)" {
		t.Errorf("bound controller: %+v", x)
	}
	if x := by["0000:00:1f.4"]; x.Driver != "i801_smbus" || len(x.Adapters) != 1 || !strings.HasPrefix(x.Adapters[0], "/dev/i2c-0 (SMBus I801") {
		t.Errorf("smbus: %+v", x)
	}
}

func TestI2CMCU(t *testing.T) {
	d := i2cFixture(t, true).i2cDiag()
	if len(d.MCUs) != 1 {
		t.Fatalf("only the HT32 device counts: %+v", d.MCUs)
	}
	m := d.MCUs[0]
	if m.ID != "HT32F522:00" || m.HID != "HT32F522" || m.Status != "15" || m.Path != `\_SB_.PC00.I2C0.MCU_` || m.Parent != "0000:00:15.0" {
		t.Errorf("mcu: %+v", m)
	}
	if none := i2cFixture(t, false).i2cDiag(); len(none.MCUs) != 0 {
		t.Errorf("no HT32 device expected: %+v", none.MCUs)
	}
}

func TestI2CModules(t *testing.T) {
	c := i2cFixture(t, false)
	dir := c.Env.LibModules + "/6.12.24-Unraid"
	write(t, dir+"/modules.dep", "kernel/drivers/i2c/busses/i2c-designware-core.ko.xz:\nkernel/drivers/i2c/busses/i2c-designware-pci.ko.xz: kernel/drivers/i2c/busses/i2c-designware-core.ko.xz\n")
	write(t, dir+"/modules.builtin", "kernel/drivers/i2c/busses/i2c-designware-platform.ko\n")
	write(t, c.Env.ProcModules, "i2c_designware_core 1 1 - Live 0x0\ni2c_i801 1 0 - Live 0x0\n")
	got := map[string]I2CModule{}
	for _, m := range c.i2cDiag().Modules {
		got[m.Name] = m
	}
	if m := got["i2c_designware_core"]; !m.Exists || !m.Loaded || m.Kind != "module" {
		t.Errorf("core: %+v", m)
	}
	if m := got["i2c_designware_pci"]; !m.Exists || m.Loaded {
		t.Errorf("pci: %+v", m)
	}
	if m := got["i2c_designware_platform"]; !m.Exists || !m.Loaded || m.Kind != "built in" {
		t.Errorf("platform: %+v", m)
	}
	// no modules.dep: nothing exists, nothing loaded
	for _, m := range i2cFixture(t, false).i2cDiag().Modules {
		if m.Exists || m.Loaded {
			t.Errorf("absent module reported: %+v", m)
		}
	}
}

func TestI2CHintInChecks(t *testing.T) {
	failed := Report{Fan: FanReport{Found: true}, LED: LEDReport{Supported: true, Probe: "failed", Error: "no answer"}}
	hint := func(c *Ctl, r Report) string {
		out := strings.Join(diagChecks(Facts{Report: r, LED: LEDDiag{Enabled: true, Supported: true, I2C: c.i2cDiag()}}), "\n")
		if i := strings.Index(out, "LED: no LED controller"); i >= 0 {
			return out[i:]
		}
		return ""
	}
	got := hint(i2cFixture(t, true), failed)
	for _, w := range []string{"0x3a", "0000:00:15.0", "no driver bound", "probably sits behind it"} {
		if !strings.Contains(got, w) {
			t.Errorf("hint misses %q: %q", w, got)
		}
	}
	for _, bad := range []string{"modprobe", "load"} {
		if strings.Contains(got, bad) {
			t.Errorf("hint must not tell the user to %s: %q", bad, got)
		}
	}
	// everything bound, no HT32 device: no hint
	c := i2cFixture(t, false)
	os.Remove(c.Env.SysPCI + "/0000:00:15.0/class")
	if got := hint(c, failed); got != "" {
		t.Errorf("unexpected hint: %q", got)
	}
	// probe ok: no hint even with an unbound controller
	ok := failed
	ok.LED.Probe = "ok"
	if got := hint(i2cFixture(t, true), ok); got != "" {
		t.Errorf("hint with a working probe: %q", got)
	}
	// HT32 device behind a bound controller: still named
	b := i2cFixture(t, true)
	os.Symlink(b.Env.SysPCI+"/0000:00:15.1", b.Env.SysPCI+"/0000:00:15.0/driver")
	if got := hint(b, failed); !strings.Contains(got, "ACPI device HT32F522 sits behind the I2C controller 0000:00:15.0") {
		t.Errorf("mcu hint: %q", got)
	}
}

func TestI2CSection(t *testing.T) {
	body := ledSection(LEDDiag{Skipped: "x", I2C: i2cFixture(t, true).i2cDiag()}).Body
	t.Log("\n" + body)
	for _, w := range []string{"0000:00:15.0 8086:51e8 class 0x0c80 driver none", "HT32F522:00 hid HT32F522", "i2c_designware_core: absent"} {
		if !strings.Contains(body, w) {
			t.Errorf("section misses %q", w)
		}
	}
}
