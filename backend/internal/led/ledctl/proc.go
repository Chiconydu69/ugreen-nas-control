package ledctl

import (
	"crypto/rand"
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
)

func (c *Ctl) daemonRunning() bool { return c.pidAlive(c.Env.PidFile) }

func (c *Ctl) daemonStart() int {
	if c.daemonRunning() {
		c.out("Status daemon is already running.\n")
		return 0
	}
	if c.foreignMonitorRunning() {
		c.errf("ERROR: The LED monitor of ugreenleds-driver is running; uninstall that plugin first.\n")
		return 1
	}
	if names := c.ledRivals(); names != "" {
		c.errf("ERROR: %s controls the LEDs; remove or stop it first.\n", names)
		return 1
	}
	pid, err := c.Run.Spawn(c.Env.selfSpec("daemon", "run"))
	if err != nil {
		c.errf("ERROR: Could not start the status daemon: %v\n", err)
		return 1
	}
	_ = fsx.WriteFile(c.Env.PidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644)
	c.out("Status daemon started.\n")
	return 0
}

func (c *Ctl) daemonStop() int {
	if c.daemonRunning() {
		// It restores the LEDs itself on SIGTERM; the config is applied only once it is gone.
		c.killPid(c.Env.PidFile, stopWait())
	}
	_ = os.Remove(c.Env.PidFile)

	if c.isCS201x() {
		// Back to the state found before our first write, not to the saved manual settings.
		c.cs201xRestore()
		c.out("Status daemon stopped.\n")
		return 0
	}
	// Hand the LEDs back to the saved manual settings.
	c.silent(func() { c.applyConfig() })

	c.out("Status daemon stopped.\n")
	return 0
}

func (c *Ctl) daemonCommand(sub string) int {
	switch sub {
	case "start":
		return c.daemonStart()
	case "stop":
		return c.daemonStop()
	case "restart":
		c.silent(func() { c.daemonStop() })
		return c.daemonStart()
	case "status":
		if c.daemonRunning() {
			data, _ := os.ReadFile(c.Env.PidFile)
			c.out("running (pid %s)\n", strings.TrimRight(string(data), "\n"))
			return 0
		}
		c.out("stopped\n")
		return 3
	}
	c.errf("Usage: %s daemon start|stop|restart|status|run\n", c.Name)
	return 2
}

// fx_name rainbow|temperature, fx_leds "power,disk1,...", fx_speed 0-4,
// fx_reverse 0|1, fx_idle 0|1 - run by ugreen-led-i2c until stopped.
func (c *Ctl) fxRunning() bool { return c.pidAlive(c.Env.FxPidFile) }

func (c *Ctl) fxStop() {
	if c.fxRunning() {
		c.killPid(c.Env.FxPidFile, stopWait())
	}
	_ = os.Remove(c.Env.FxPidFile)
}

func (c *Ctl) fxStart() int {
	name := c.fxName()
	if name == "" {
		c.out("No effect configured.\n")
		return 0
	}

	c.fxStop()

	leds := c.fxLEDs()
	args := []string{"--speed", c.fxSpeed()}
	if c.cfg["fx_reverse"] == "1" {
		args = append(args, "--reverse")
	}
	if c.cfg["fx_idle"] == "1" {
		args = append(args, "--idle-only")
	}
	args = append(args, "effect", name, leds)

	pid, err := c.Run.Spawn(c.Env.toolSpec(args, c.Env.FxLog))
	if err != nil {
		c.errf("ERROR: Effect '%s' did not start: %v\n", name, err)
		return 1
	}
	_ = fsx.WriteFile(c.Env.FxPidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644)

	// A missing temperature sensor ends the process right away.
	c.Sleep(500 * time.Millisecond)
	if !c.fxRunning() {
		_ = os.Remove(c.Env.FxPidFile)
		first := ""
		if lines, ok := readLines(c.Env.FxLog); ok && len(lines) > 0 {
			first = lines[0]
		}
		c.errf("ERROR: Effect '%s' did not start: %s\n", name, first)
		return 1
	}

	c.out("Effect %s started on %s.\n", name, leds)
	return 0
}

func (c *Ctl) fxCommand(sub string) int {
	switch sub {
	case "start", "restart":
		return c.fxStart()
	case "stop":
		c.fxStop()
		c.out("Effect stopped.\n")
		return 0
	case "status":
		if c.fxRunning() {
			c.out("running: %s on %s\n", c.cfg["fx_name"], c.cfg["fx_leds"])
			return 0
		}
		c.out("stopped\n")
		return 3
	}
	c.errf("Usage: %s fx start|stop|restart|status\n", c.Name)
	return 2
}

// While "<prefix><led>" exists, the bay blinks white and the status daemon leaves it
// alone. The file holds a token and the LED's state from before.
func (c *Ctl) identifyStart(led, seconds string) int {
	marker := c.Env.IdentifyPrefix + led
	token := newToken()

	if !globMatch("disk[1-8]", led) {
		c.errf("ERROR: Only bay LEDs (disk1..diskN) can be identified.\n")
		return 1
	}
	if !c.requireLED(led) {
		return 1
	}
	if !isDigits(seconds) {
		c.errf("ERROR: Seconds must be a number.\n")
		return 1
	}

	// Pressed again while blinking: keep the original state, renew the timer.
	snapshot := ""
	if st, err := os.Stat(marker); err == nil && st.Mode().IsRegular() {
		if data, err := os.ReadFile(marker); err == nil {
			if lines := strings.Split(string(data), "\n"); len(lines) > 1 {
				snapshot = lines[1]
			}
		}
	} else {
		line, rc := c.ledLine(led)
		if rc != 0 {
			return 1
		}
		snapshot = line
	}
	_ = fsx.WriteFile(marker, []byte(token+"\n"+snapshot+"\n"), 0o644) //nolint:gosec // led is checked against disk[1-8] above

	if c.tool("set", led, "color", "255", "255", "255") != 0 ||
		c.tool("set", led, "brightness", "255") != 0 ||
		c.tool("set", led, "blink", "100", "100") != 0 {
		return 1
	}

	if _, err := c.Run.Spawn(c.Env.selfSpec("_after", seconds, "identify-stop", led, token)); err != nil {
		c.errf("ERROR: Could not start the identify timer: %v\n", err)
		return 1
	}

	c.out("Identifying %s for %s s.\n", led, seconds)
	return 0
}

// identifyStop always stops without a token; the timer passes its token, so an older
// timer doesn't end a renewed identify early.
func (c *Ctl) identifyStop(led, token string) int {
	marker := c.Env.IdentifyPrefix + led
	st, err := os.Stat(marker)
	if err != nil || !st.Mode().IsRegular() {
		return 0
	}
	data, _ := os.ReadFile(marker)
	lines := strings.Split(string(data), "\n")
	if token != "" && lines[0] != token {
		return 0
	}
	snapshot := ""
	if len(lines) > 1 {
		snapshot = lines[1]
	}
	f := splitRead(snapshot, 8)
	mode, brightness, r, g, b, on, off := f[1], f[2], f[3], f[4], f[5], f[6], f[7]
	_ = os.Remove(marker)

	c.quiet(func() {
		c.tool("set", led, "color", r, g, b)
		c.tool("set", led, "brightness", brightness)
		switch mode {
		case "off", "on":
			c.tool("set", led, mode)
		case "blink", "breath":
			c.tool("set", led, mode, on, off)
		}
	})

	c.out("Stopped identifying %s.\n", led)
	return 0
}

func (c *Ctl) identifyingLEDs() string {
	var parts []string
	for _, p := range listPrefix(c.Env.IdentifyPrefix + "disk") {
		if exists(p) {
			parts = append(parts, `"`+strings.TrimPrefix(p, c.Env.IdentifyPrefix)+`"`)
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// newToken tells a renewed identify from the timer of the earlier one.
func newToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return strconv.FormatUint(binary.BigEndian.Uint64(b[:]), 10)
}
