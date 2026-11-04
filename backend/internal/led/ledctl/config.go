package ledctl

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultDiskColor     = "255 160 0"
	defaultNetdevColor   = "0 200 83"
	defaultErrorColor    = "255 59 48"
	defaultSyncColor     = "0 120 255"
	defaultSmartColor    = "255 140 0"
	defaultShutdownColor = "255 140 0"
)

// Network LED by link speed (Mbit/s -> colour); a speed uses the largest step at or below it.
var speedSteps = []int{10, 100, 1000, 2500, 5000, 10000}

func defaultSpeedColor(step int) string {
	switch step {
	case 10:
		return "255 59 48"
	case 100:
		return "255 140 0"
	case 1000:
		return "255 214 10"
	case 2500:
		return "0 200 83"
	case 5000:
		return "0 200 200"
	}
	return "0 120 255"
}

var cfgLine = regexp.MustCompile(`^([A-Za-z0-9_]+)="(?s:(.*))"$`)

// cfgLoad parses the settings file (key="value" lines, the last one of a key wins,
// nothing is sourced) and again only when its content changed.
func (c *Ctl) cfgLoad() {
	raw := ""
	if isReadable(c.Env.Config) {
		if data, err := os.ReadFile(c.Env.Config); err == nil {
			// $(<file) drops trailing newlines.
			raw = strings.TrimRight(string(data), "\n")
		}
	}
	if c.cfgLoaded && raw == c.cfgRaw {
		return
	}
	c.cfg = map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		if m := cfgLine.FindStringSubmatch(line); m != nil {
			c.cfg[m[1]] = m[2]
		}
	}
	c.cfgRaw = raw
	c.cfgLoaded = true
}

// get is ${CFG[key]:-def}.
func (c *Ctl) get(key, def string) string {
	if v := c.cfg[key]; v != "" {
		return v
	}
	return def
}

func (c *Ctl) boolNot0(key string) bool { return c.cfg[key] != "0" }
func (c *Ctl) bool1(key string) bool    { return c.cfg[key] == "1" }

// netdevStatus: activity (blinks with traffic), link (steady, error colour without a
// connection) or manual (the daemon leaves it alone).
func (c *Ctl) netdevStatus() string {
	switch v := c.cfg["netdev_status"]; v {
	case "activity", "link", "manual":
		return v
	}
	return "activity"
}

func (c *Ctl) applyLED(led string) int {
	color := c.cfg[led+"_color"]
	brightness := c.cfg[led+"_brightness"]
	effect := c.cfg[led+"_effect"]

	if color != "" {
		f := strings.Fields(color)
		for len(f) < 3 {
			f = append(f, "")
		}
		c.setColor(led, f[0], f[1], f[2], f[3:]...)
	}
	if brightness != "" {
		c.setBrightness(led, brightness)
	}

	// An effect would light an LED that is meant to be off.
	if b := brightness; b == "0" {
		return 0
	}

	switch effect {
	case "blink":
		return c.setEffect("blink", led, c.msCfg(led+"_on", "450"), c.msCfg(led+"_off", "450"))
	case "breath":
		return c.setEffect("breath", led, c.msCfg(led+"_on", "1200"), c.msCfg(led+"_off", "900"))
	case "none":
		return c.stopEffect(led)
	}
	return 0
}

func (c *Ctl) applyConfig() int {
	if st, err := os.Stat(c.Env.Config); err != nil || !st.Mode().IsRegular() {
		c.out("No saved settings (%s).\n", c.Env.Config)
		return 0
	}
	for _, led := range c.leds {
		c.applyLED(led)
	}
	c.out("Saved settings applied.\n")
	return 0
}

func (c *Ctl) savedBrightness(led string) int64 {
	return atoiOr(c.get(led+"_brightness", "255"), 255)
}

// savedBrightnessJSON is {"power": 204, ...}: the day brightness of every LED that has one saved.
func (c *Ctl) savedBrightnessJSON() string {
	var parts []string
	for _, led := range c.leds {
		if v := c.cfg[led+"_brightness"]; isByte(v) {
			parts = append(parts, `"`+led+`": `+v)
		}
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// Values of the settings file reach the chip tool's command line and its registers, so each one is checked here;
// a value that does not fit is logged once and replaced by its default. (The file also comes from another plugin
// through led-import, or from a hand edit on the flash drive.)

// warn logs a rejected setting once per key and value.
func (c *Ctl) warn(key, value, use string) {
	id := key + "=" + value
	if c.warned[id] {
		return
	}
	if c.warned == nil {
		c.warned = map[string]bool{}
	}
	c.warned[id] = true
	c.log(fmt.Sprintf("setting %s=%q is not valid, using %s", key, truncate(value, 40), use))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// validColor: "R G B", three numbers 0..255 separated by single spaces.
func validColor(v string) bool {
	f := strings.Split(v, " ")
	if len(f) != 3 {
		return false
	}
	for _, p := range f {
		if !isByte(p) {
			return false
		}
	}
	return true
}

// colorCfg is a colour setting ("R G B"), def when it is missing or not a colour.
func (c *Ctl) colorCfg(key, def string) string {
	v := c.cfg[key]
	if v == "" {
		return def
	}
	if !validColor(v) {
		c.warn(key, v, def)
		return def
	}
	return v
}

// msCfg is a duration in milliseconds (digits, at most 65535), def when it is missing or not one.
func (c *Ctl) msCfg(key, def string) string {
	v := c.cfg[key]
	if v == "" {
		return def
	}
	if n, ok := atoi(v); !ok || !isDigits(v) || len(v) > 5 || n > 65535 {
		c.warn(key, v, def)
		return def
	}
	return v
}

// fxName is the effect to run: rainbow or temperature; "" for none or an unknown name (logged).
func (c *Ctl) fxName() string {
	v := c.cfg["fx_name"]
	switch v {
	case "", "rainbow", "temperature":
		return v
	}
	c.warn("fx_name", v, "no effect")
	return ""
}

func (c *Ctl) fxSpeed() string {
	v := c.cfg["fx_speed"]
	if v == "" {
		return "2"
	}
	if len(v) != 1 || v[0] < '0' || v[0] > '4' {
		c.warn("fx_speed", v, "2")
		return "2"
	}
	return v
}

// fxLEDs is the comma separated list of LEDs the effect runs on: known names only, "power" if none is left.
func (c *Ctl) fxLEDs() string {
	v := c.cfg["fx_leds"]
	if v == "" {
		return "power"
	}
	var keep []string
	for _, name := range strings.Split(v, ",") {
		if c.validLED(name) && !slices.Contains(keep, name) {
			keep = append(keep, name)
		}
	}
	if len(keep) == 0 {
		c.warn("fx_leds", v, "power")
		return "power"
	}
	if got := strings.Join(keep, ","); got != v {
		c.warn("fx_leds", v, got)
		return got
	}
	return v
}
