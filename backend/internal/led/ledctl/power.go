package ledctl

import (
	"os"
	"strconv"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
)

// Night mode: night_enabled="1", night_start/night_end "HH:MM" (may span midnight),
// night_action "dim" (down to night_brightness, 0-255) or "off".
func (c *Ctl) isNight() bool {
	if c.cfg["night_enabled"] != "1" {
		return false
	}
	start := c.get("night_start", "22:00")
	end := c.get("night_end", "07:00")
	now := c.Env.Now
	if now == "" {
		now = c.Now().Format("1504")
	}
	s := atoiOr(strings.Replace(start, ":", "", 1), 0)
	e := atoiOr(strings.Replace(end, ":", "", 1), 0)
	n := atoiOr(now, 0)

	switch {
	case s < e:
		return n >= s && n < e
	case s > e:
		return n >= s || n < e
	}
	return false
}

// effectiveBrightness is the brightness to show: the saved one by day, at most the night level (or 0) by night.
func (c *Ctl) effectiveBrightness(saved int64, night bool) int64 {
	if night {
		if c.cfg["night_action"] == "off" {
			return 0
		}
		if level := atoiOr(c.get("night_brightness", "20"), 20); saved > level {
			saved = level
		}
	}
	return saved
}

// applyNight gives LEDs the daemon doesn't drive their night look: dim or off, effects keep running.
func (c *Ctl) applyNight(leds ...string) {
	for _, led := range leds {
		b := c.effectiveBrightness(c.savedBrightness(led), true)
		c.quiet(func() {
			if b == 0 {
				c.tool("set", led, "off")
			} else {
				c.tool("set", led, "brightness", itoa(b))
			}
		})
	}
}

func catState(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\n")
}

func writeState(path, value string) { _ = fsx.WriteFile(path, []byte(value+"\n"), 0o644) }

// scheduleTick switches between day and night when the state changes (cron runs this every minute).
func (c *Ctl) scheduleTick(force string) int {
	wanted := "day"
	if c.isNight() {
		wanted = "night"
	}
	if force != "--force" && catState(c.Env.ScheduleState) == wanted {
		return 1
	}
	writeState(c.Env.ScheduleState, wanted)

	var leds []string
	if c.daemonRunning() {
		// The daemon picks up day/night itself after a restart; the power LED is ours,
		// and so is the network LED when it stays manual.
		c.silent(func() { c.daemonCommand("restart") })
		leds = []string{"power"}
		if c.netdevStatus() == "manual" {
			leds = []string{"power", "netdev"}
		}
	} else {
		c.silent(func() { c.applyConfig() })
		leds = c.leds
	}

	if wanted == "night" {
		c.applyNight(leds...)
	}
	c.log("night mode: " + wanted)
	return 0
}

func (c *Ctl) fxOnPower() bool {
	return strings.Contains(","+c.cfg["fx_leds"]+",", ",power,")
}

func (c *Ctl) notifyDir() string {
	if !c.dynLoaded {
		c.dynLoad()
	}
	if c.Env.NotifyDir != "" {
		return c.Env.NotifyDir
	}
	if c.dynNotify != "" {
		return c.dynNotify
	}
	return "/tmp/notifications"
}

// unreadAlerts counts unread Unraid notifications at or above alert_level.
func (c *Ctl) unreadAlerts() int {
	level := c.get("alert_level", "warning")
	dir := c.notifyDir() + "/unread"
	count := 0
	for _, name := range listDir(dir) {
		if !strings.HasSuffix(name, ".notify") {
			continue
		}
		lines, ok := readLines(dir + "/" + name)
		if !ok {
			continue
		}
		importance := ""
		for _, line := range lines {
			if m := importanceRe.FindStringSubmatch(line); m != nil {
				importance = m[1]
				break
			}
		}
		switch importance {
		case "alert":
			count++
		case "warning":
			if level == "warning" {
				count++
			}
		}
	}
	return count
}

func (c *Ctl) restorePower() {
	c.quiet(func() { c.applyLED("power") })
	if c.isNight() {
		c.applyNight("power")
	}
	if c.fxOnPower() && c.cfg["fx_name"] != "" && !c.fxRunning() {
		c.quiet(func() { c.fxStart() })
	}
}

// shutdownSignal shows on the power LED that the NAS shuts down or reboots. emhttp calls
// event/stopping at the start of every array stop; only runlevel 0 (halt) or 6 (reboot)
// means the system goes down, a plain array stop leaves the LED alone.
func (c *Ctl) shutdownSignal(test string) int {
	if test != "--test" {
		if c.cfg["shutdown_blink"] != "1" {
			return 0
		}
		out, _ := c.Run.Output("runlevel")
		var levels []string
		for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
			f := strings.Fields(line)
			second := ""
			if len(f) > 1 {
				second = f[1]
			}
			levels = append(levels, second)
		}
		if lv := strings.Join(levels, "\n"); lv != "0" && lv != "6" {
			return 0
		}

		// From here on nothing else writes the power LED (night mode and alert ticks check this).
		_ = fsx.WriteFile(c.Env.ShutdownMark, nil, 0o644)
		c.fxStop()
	}

	effect := c.cfg["shutdown_effect"]
	if effect != "breath" {
		effect = "blink"
	}
	color := c.colorCfg("shutdown_color", defaultShutdownColor)
	b := c.effectiveBrightness(c.savedBrightness("power"), c.isNight())

	if b == 0 {
		c.quiet(func() { c.tool("set", "power", "off") })
		return 0
	}

	c.quiet(func() {
		c.tool(append([]string{"set", "power", "color"}, strings.Fields(color)...)...)
		c.tool("set", "power", "brightness", itoa(b))
		if effect == "breath" {
			c.tool("set", "power", "breath", "1200", "1200")
		} else {
			c.tool("set", "power", "blink", "500", "500")
		}
	})

	if test == "--test" {
		// Back to the normal look after a few seconds.
		if _, err := c.Run.Spawn(c.Env.selfSpec("_after", "6", "power-restore")); err != nil {
			c.errf("ERROR: Could not start the restore timer: %v\n", err)
			return 1
		}
		c.out("Power LED signals shutdown for 6 s.\n")
		return 0
	}
	c.log("Shutting down or rebooting: power LED signals it")
	return 0
}

// alertTick: while Unraid alerts are unread the power LED blinks in the alert colour; it is
// written every minute, so other writes (mode switch, night mode) can't hide it.
func (c *Ctl) alertTick(force string) int {
	count := 0
	if c.cfg["alert_enabled"] == "1" {
		count = c.unreadAlerts()
	}
	last := catState(c.Env.AlertState)

	if count > 0 {
		writeState(c.Env.AlertState, "alert")

		// A rainbow/temperature effect on the power LED would paint over it.
		if c.fxOnPower() && c.fxRunning() {
			c.fxStop()
		}

		b := c.effectiveBrightness(c.savedBrightness("power"), c.isNight())
		c.quiet(func() {
			if b == 0 {
				c.tool("set", "power", "off")
				return
			}
			c.tool(append([]string{"set", "power", "color"}, strings.Fields(c.colorCfg("alert_color", defaultErrorColor))...)...)
			c.tool("set", "power", "brightness", itoa(b))
			c.tool("set", "power", "blink", "500", "500")
		})

		if last != "alert" {
			c.log("Unraid alert: power LED signals " + strconv.Itoa(count) + " unread notification(s)")
		}
		return 0
	}
	if last == "alert" || force == "--force" {
		writeState(c.Env.AlertState, "none")
		if last == "alert" {
			c.restorePower()
			c.log("Unraid alerts read: power LED back to normal")
			return 0
		}
		return 1
	}
	return 0
}
