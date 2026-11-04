package ledctl

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
)

var (
	busI2CRe = regexp.MustCompile(`"bus": "/dev/i2c-([0-9]*)"`)
	busMemRe = regexp.MustCompile(`"bus": "(/dev/mem|/dev/port)"`)
)

// statusJSON is what the web UI expects: brightness 0 while off, effect none|blink|breath.
// f is "<name> <mode> <brightness> <R> <G> <B> <on_ms> <off_ms>" split into eight fields.
func (c *Ctl) statusJSON(f []string) string {
	name, mode, brightness := f[0], f[1], f[2]
	color := f[3] + " " + f[4] + " " + f[5]
	on, off := f[6], f[7]
	effect := "none"

	switch mode {
	case "off":
		brightness = "0"
	case "blink", "breath":
		effect = mode
	}

	var sb strings.Builder
	sb.WriteString("{\n")
	fmt.Fprintf(&sb, "  \"name\": \"%s\",\n", name)
	fmt.Fprintf(&sb, "  \"brightness\": \"%s\",\n", brightness)
	sb.WriteString("  \"max_brightness\": \"255\",\n")
	fmt.Fprintf(&sb, "  \"color\": \"%s\",\n", color)
	fmt.Fprintf(&sb, "  \"effect\": \"%s\",\n", effect)
	fmt.Fprintf(&sb, "  \"on_ms\": \"%s\",\n", on)
	fmt.Fprintf(&sb, "  \"off_ms\": \"%s\"", off)

	// Phase: ms epoch of the last blink/breath command with these timings, and now, same clock.
	if mode == "blink" || mode == "breath" {
		if line, ok := readLine(c.Env.PhasePrefix + name); ok {
			p := splitRead(line, 3)
			if p[1] == on && p[2] == off {
				fmt.Fprintf(&sb, ",\n  \"sent_ms\": %s,\n  \"now_ms\": %d", p[0], c.Now().UnixMilli())
			}
		}
	}
	sb.WriteString("\n}\n")
	return sb.String()
}

func (c *Ctl) getStatus(led string) int {
	if !c.requireLED(led) {
		return 1
	}
	line, rc := c.ledLine(led)
	if rc != 0 {
		return 1
	}
	c.out("%s", c.statusJSON(splitFields(line, 8)))
	return 0
}

func splitFields(line string, n int) []string {
	f := strings.Fields(line)
	for len(f) < n {
		f = append(f, "")
	}
	return f[:n]
}

func (c *Ctl) allLEDs() {
	out, _ := c.toolOut(false, "--plain", "get", "all")
	var sb strings.Builder
	sb.WriteString("{\n")
	first := true
	lines, _ := splitTerminated(out)
	for _, line := range lines {
		f := splitRead(line, 8)
		if !c.validLED(f[0]) {
			continue
		}
		if !first {
			sb.WriteString(",\n")
		}
		fmt.Fprintf(&sb, "\"%s\": ", f[0])
		sb.WriteString(c.statusJSON(f))
		first = false
	}
	sb.WriteString("}\n")
	c.out("%s", sb.String())
}

// splitTerminated returns the newline-terminated lines of a string (what `while read` sees).
func splitTerminated(s string) ([]string, bool) {
	var out []string
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			return out, true
		}
		out = append(out, s[:i])
		s = s[i+1:]
	}
}

func (c *Ctl) listJSON() string {
	var parts []string
	if _, rc := c.toolOut(false, "--plain", "get", "power"); rc == 0 {
		for _, led := range c.leds {
			parts = append(parts, `"`+led+`"`)
		}
	}
	return "[" + strings.Join(parts, ", ") + "]\n"
}

// ledRivals names the running tools that write the same LEDs (the rival detector), "" when there is none.
func (c *Ctl) ledRivals() string {
	if c.Detect == nil {
		return ""
	}
	var names []string
	for _, r := range conflict.Blocking(c.Detect(), conflict.LED) {
		names = append(names, r.Name)
	}
	return strings.Join(names, ", ")
}

// foreignMonitorRunning: ich777's ugreenleds-driver monitor writes the same LEDs.
func (c *Ctl) foreignMonitorRunning() bool {
	_, code := c.Run.Output("pgrep", "-f", "^/bin/bash /usr/bin/ugreen-leds")
	return code == 0
}

func (c *Ctl) isGT() bool { return globMatch("DXP4800*GT*", c.product) }

// isCS201x: the DXP2800 GT has no I2C LED controller; its LEDs are four ARGB channels in the CS201x embedded
// controller, behind an opt-in (cs201x_leds="1" in leds.cfg, docs/led.md).
func (c *Ctl) isCS201x() bool { return globMatch("DXP2800 GT*", c.product) }

// cs201xRestore hands the channels back as found before our first write; a no-op on every other model.
func (c *Ctl) cs201xRestore() {
	if c.isCS201x() {
		c.quiet(func() { c.tool("cs201x", "restore") })
	}
}

func (c *Ctl) i2cNameFiles() []string {
	var out []string
	for _, n := range listDir(c.Env.SysI2C) {
		if strings.HasPrefix(n, "i2c-") {
			out = append(out, c.Env.SysI2C+"/"+n+"/name")
		}
	}
	return out
}

func fileHasLinePrefix(path string, prefixes ...string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var hits []string
	for _, line := range strings.Split(string(data), "\n") {
		for _, p := range prefixes {
			if strings.HasPrefix(line, p) {
				hits = append(hits, line)
				break
			}
		}
	}
	return hits
}

func (c *Ctl) hasSMBus() bool {
	if c.isGT() {
		if data, err := os.ReadFile(c.Env.ProcIomem); err == nil && strings.Contains(string(data), " : AMDI0010:") {
			return true
		}
		for _, f := range c.i2cNameFiles() {
			if len(fileHasLinePrefix(f, "Synopsys DesignWare I2C adapter")) > 0 {
				return true
			}
		}
		return false
	}
	for _, f := range c.i2cNameFiles() {
		if len(fileHasLinePrefix(f, "SMBus I801 adapter")) > 0 {
			return true
		}
	}
	return false
}

// ensureSMBus loads the SMBus driver: the controller sits on the Intel SMBus. Unraid usually
// loads its driver (i2c_i801) at boot, but not always (GitHub issue #30), so load it here and
// give udev a moment to register the adapter. i2c-dev makes it /dev/i2c-N.
func (c *Ctl) ensureSMBus() bool {
	c.Run.Output("modprobe", "-q", "i2c-dev")

	// The GT needs no driver: the helper drives its DesignWare controller through /dev/mem.
	if c.isGT() {
		return c.hasSMBus()
	}

	// Already loaded without an adapter: nothing to wait for (dmesg tells why).
	if data, err := os.ReadFile(c.Env.ProcModules); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "i2c_i801 ") {
				return c.hasSMBus()
			}
		}
	}
	if _, code := c.Run.Output("modprobe", "-q", "i2c_i801"); code != 0 {
		return false
	}

	for range 30 {
		if c.hasSMBus() {
			return true
		}
		c.Sleep(100 * time.Millisecond)
	}
	return false
}

func (c *Ctl) smbusCommand() int {
	if c.ensureSMBus() {
		var hits []string
		for _, f := range c.i2cNameFiles() {
			for _, l := range fileHasLinePrefix(f, "SMBus I801", "Synopsys DesignWare") {
				hits = append(hits, f+":"+l)
			}
		}
		if len(hits) == 0 {
			hits = []string{"AMD DesignWare I2C (AMDI0010) via /dev/mem"}
		}
		c.out("SMBus adapter: %s\n", strings.Join(hits, "\n"))
		return 0
	}
	c.errf("ERROR: No SMBus I801 / DesignWare adapter after loading its driver. Kernel messages:\n")
	out, _ := c.Run.Output("dmesg")
	re := regexp.MustCompile(`(?i)i801|smbus|designware`)
	var hits []string
	for _, l := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if re.MatchString(l) {
			hits = append(hits, l)
		}
	}
	if len(hits) > 10 {
		hits = hits[len(hits)-10:]
	}
	for _, l := range hits {
		c.errf("%s\n", l)
	}
	return 1
}

// dxp480tApply: DXP480T Plus has no controller at 0x3a, only a power LED on a simpler chip
// (0x26), which the tool sets to static white. Fails on every other model.
func (c *Ctl) dxp480tApply() int {
	if globMatch("DXP480T*", c.product) {
		return c.tool("dxp480t", "white")
	}
	return 1
}

func (c *Ctl) status() int {
	bus, found := "", "failed"

	// One failed probe (the controller did not answer while the daemon or an effect was
	// writing) is not "no controller": ask up to three times, then fall back to the bus
	// that answered within the last 10 minutes.
	for range 3 {
		if probe, rc := c.toolOut(false, "probe"); rc == 0 {
			if m := busI2CRe.FindStringSubmatch(probe); m != nil {
				bus = m[1]
			} else if m := busMemRe.FindStringSubmatch(probe); m != nil {
				bus = `"` + m[1] + `"`
			}
		}
		if bus != "" {
			break
		}
		c.Sleep(300 * time.Millisecond)
	}
	if bus != "" {
		found = "ok"
		_ = fsx.WriteFile(c.Env.BusCache, []byte(bus+"\n"), 0o644)
	} else if st, err := os.Stat(c.Env.BusCache); err == nil && c.Now().Sub(st.ModTime()) < 10*time.Minute {
		bus, _ = readLine(c.Env.BusCache)
		if bus != "" {
			found = "cached"
		}
	}

	kernel, _ := readLine(c.Env.OSRelease)
	mode := "manual"
	if c.cfg["mode"] == "status" {
		mode = "status"
	}
	fx := ""
	if c.fxRunning() {
		fx = c.cfg["fx_name"]
	}
	c.syncStateLoad()
	syncAction, syncPercent := "", int64(0)
	if c.syncSt.active {
		syncAction, syncPercent = c.syncSt.action, c.syncSt.percent
	}
	alertActive := false
	if v, _ := readLine(c.Env.AlertState); v == "alert" {
		alertActive = true
	}
	identify := c.identifyingLEDs()
	netDev := c.networkDevice()
	netdevMode := c.netdevStatus()

	var sb strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&sb, format, a...) }
	p("{\n")
	p("  \"ok\": true,\n")
	p("  \"foreign_monitor\": %t,\n", c.foreignMonitorRunning())
	p("  \"model\": \"%s\",\n", c.product)
	p("  \"bays\": %d,\n", c.bays)
	p("  \"kernel\": \"%s\",\n", kernel)
	p("  \"mapping\": \"%s\",\n", c.mappingSource())
	p("  \"mode\": \"%s\",\n", mode)
	p("  \"daemon\": %t,\n", c.daemonRunning())
	p("  \"fx\": \"%s\",\n", fx)
	p("  \"fx_leds\": \"%s\",\n", c.cfg["fx_leds"])
	p("  \"identify\": %s,\n", identify)
	p("  \"sync_action\": \"%s\",\n", syncAction)
	p("  \"sync_percent\": %d,\n", syncPercent)
	p("  \"sync_enabled\": %t,\n", c.boolNot0("sync_enabled"))
	p("  \"sync_color\": \"%s\",\n", c.colorCfg("sync_color", defaultSyncColor))
	p("  \"standby_mode\": \"%s\",\n", c.get("standby_mode", "dim"))
	p("  \"standby_level\": %s,\n", c.get("standby_level", "30"))
	p("  \"smart_enabled\": %t,\n", c.boolNot0("smart_enabled"))
	p("  \"error_enabled\": %t,\n", c.boolNot0("error_enabled"))
	p("  \"activity_enabled\": %t,\n", c.boolNot0("activity_enabled"))
	p("  \"smart_color\": \"%s\",\n", c.colorCfg("smart_color", defaultSmartColor))
	p("  \"disk_color_mode\": \"%s\",\n", c.get("disk_color_mode", "own"))
	p("  \"netdev_status\": \"%s\",\n", netdevMode)
	p("  \"alert_enabled\": %t,\n", c.bool1("alert_enabled"))
	p("  \"alert_level\": \"%s\",\n", c.get("alert_level", "warning"))
	p("  \"alert_color\": \"%s\",\n", c.colorCfg("alert_color", defaultErrorColor))
	p("  \"shutdown_blink\": %t,\n", c.bool1("shutdown_blink"))
	p("  \"shutdown_color\": \"%s\",\n", c.colorCfg("shutdown_color", defaultShutdownColor))
	shutdownEffect := c.cfg["shutdown_effect"]
	if shutdownEffect != "breath" {
		shutdownEffect = "blink"
	}
	p("  \"shutdown_effect\": \"%s\",\n", shutdownEffect)
	p("  \"alert_count\": %d,\n", c.unreadAlerts())
	p("  \"alert_active\": %t,\n", alertActive)
	p("  \"fx_speed\": %s,\n", c.fxSpeed())
	p("  \"fx_reverse\": %t,\n", c.bool1("fx_reverse"))
	p("  \"fx_idle\": %t,\n", c.bool1("fx_idle"))
	p("  \"night\": %t,\n", c.isNight())
	p("  \"disk_error_color\": \"%s\",\n", c.colorCfg("disk_error_color", defaultErrorColor))
	p("  \"saved_brightness\": %s,\n", c.savedBrightnessJSON())
	p("  \"network_device\": \"%s\",\n", netDev)
	p("  \"netdev_speed\": %s,\n", c.linkSpeed(netDev, 0))
	p("  \"network_ports\": %s,\n", c.networkPortsJSON())
	p("  \"netdev_speed_enabled\": %t,\n", c.bool1("netdev_speed_enabled"))
	p("  \"netdev_speed_colors\": %s,\n", c.speedColorsJSON())
	p("  \"controller\": \"ugreen-led-i2c\",\n")
	if bus != "" {
		p("  \"i2c_bus\": %s,\n", bus)
		if bus == `"/dev/port"` {
			p("  \"i2c_address\": \"cs201x\",\n")
		} else {
			p("  \"i2c_address\": \"0x3a\",\n")
		}
		p("  \"i2c_probe\": \"%s\",\n", found)
	} else {
		p("  \"i2c_bus\": null,\n")
		p("  \"i2c_address\": null,\n")
		p("  \"i2c_probe\": \"failed\",\n")
	}
	p("  \"leds\": ")
	sb.WriteString(c.listJSON())
	p("}\n")
	c.out("%s", sb.String())
	return 0
}
