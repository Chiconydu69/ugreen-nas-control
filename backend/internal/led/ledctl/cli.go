package ledctl

import (
	"context"
	"strconv"
	"time"
)

const usageText = `UGREEN LED Control

Usage:
  ugreen-led-ctl status
  ugreen-led-ctl list
  ugreen-led-ctl get <led>
  ugreen-led-ctl all
  ugreen-led-ctl apply        (restore the settings saved by the web UI)
  ugreen-led-ctl bays         (which disk sits in which bay)
  ugreen-led-ctl daemon start|stop|restart|status|run
  ugreen-led-ctl schedule [--force]   (night mode and Unraid alerts; run every minute by cron)
  ugreen-led-ctl alerts [--force]     (Unraid alerts on the power LED only)
  ugreen-led-ctl fx start|stop|restart|status   (rainbow/temperature from leds.cfg)
  ugreen-led-ctl shutdown [--test]               (power LED signals shutdown/reboot; --test shows it for 6 s)
  ugreen-led-ctl smbus                           (load the SMBus driver, show the adapter or why it is missing)
  ugreen-led-ctl dxp480t                         (DXP480T Plus only: power LED to static white)
  ugreen-led-ctl identify <diskN> [seconds]      (blink a bay white, default 30 s)
  ugreen-led-ctl identify-stop <diskN>
  ugreen-led-ctl on <led>
  ugreen-led-ctl off <led>
  ugreen-led-ctl brightness <led> <0-255>
  ugreen-led-ctl color <led> <R> <G> <B>
  ugreen-led-ctl blink <led> <on_ms> <off_ms>
  ugreen-led-ctl breath <led> <on_ms> <off_ms>
  ugreen-led-ctl stop <led>
  ugreen-led-ctl help

Supported LEDs:
  power
  netdev
  disk1 ... diskN (N = number of bays)
`

func (c *Ctl) need(args []string, n int, usage string) bool {
	if len(args) == n {
		return true
	}
	c.errf("Usage: %s %s\n", c.Name, usage)
	return false
}

func arg(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

func orDef(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (c *Ctl) Main(ctx context.Context, args []string) int {
	c.cfgLoad()

	cmd := "help"
	if len(args) > 0 && args[0] != "" {
		cmd = args[0]
	}

	// Every command except help talks to the controller (tests bring their own tool).
	if !c.Env.ToolFromEnv {
		switch cmd {
		case "help", "-h", "--help", "cs201x":
		default:
			if c.isCS201x() {
				break // no SMBus: the LEDs sit in the embedded controller
			}
			if !c.hasSMBus() {
				c.ensureSMBus()
			}
		}
	}

	switch cmd {
	case "dxp480t":
		return c.dxp480tApply()
	case "cs201x":
		return c.tool(append([]string{"cs201x"}, args[1:]...)...)
	case "smbus":
		return c.smbusCommand()
	case "status":
		return c.status()
	case "list":
		c.out("%s", c.listJSON())
		return 0
	case "all":
		c.allLEDs()
		return 0
	case "apply":
		return c.applyConfig()
	case "bays":
		c.baysJSON()
		return 0
	case "daemon":
		if sub := orDef(arg(args, 1), "status"); sub == "run" {
			return c.daemonRun(ctx)
		}
		return c.daemonCommand(orDef(arg(args, 1), "status"))
	case "schedule":
		if exists(c.Env.ShutdownMark) {
			return 0
		}
		c.scheduleTick(arg(args, 1))
		return c.alertTick("")
	case "alerts":
		if exists(c.Env.ShutdownMark) {
			return 0
		}
		return c.alertTick(arg(args, 1))
	case "shutdown":
		return c.shutdownSignal(arg(args, 1))
	case "power-restore":
		c.restorePower()
		return c.alertTick("--force")
	case "fx":
		return c.fxCommand(orDef(arg(args, 1), "status"))
	case "identify":
		if len(args) < 2 {
			c.errf("Usage: %s identify <diskN> [seconds]\n", c.Name)
			return 2
		}
		return c.identifyStart(arg(args, 1), orDef(arg(args, 2), "30"))
	case "identify-stop":
		if len(args) < 2 {
			c.errf("Usage: %s identify-stop <diskN>\n", c.Name)
			return 2
		}
		return c.identifyStop(arg(args, 1), arg(args, 2))
	case "_after":
		// Internal: the detached timer of identify and shutdown --test.
		return c.after(ctx, args[1:])

	case "get":
		if !c.need(args, 2, "get <led>") {
			return 2
		}
		return c.getStatus(arg(args, 1))
	case "on":
		if !c.need(args, 2, "on <led>") {
			return 2
		}
		return c.setOn(arg(args, 1))
	case "off":
		if !c.need(args, 2, "off <led>") {
			return 2
		}
		return c.setOff(arg(args, 1))
	case "stop":
		if !c.need(args, 2, "stop <led>") {
			return 2
		}
		return c.stopEffect(arg(args, 1))
	case "brightness":
		if !c.need(args, 3, "brightness <led> <0-255>") {
			return 2
		}
		return c.setBrightness(arg(args, 1), arg(args, 2))
	case "color":
		if !c.need(args, 5, "color <led> <R> <G> <B>") {
			return 2
		}
		return c.setColor(arg(args, 1), arg(args, 2), arg(args, 3), arg(args, 4))
	case "blink":
		if !c.need(args, 4, "blink <led> <on_ms> <off_ms>") {
			return 2
		}
		return c.setEffect("blink", arg(args, 1), arg(args, 2), arg(args, 3))
	case "breath":
		if !c.need(args, 4, "breath <led> <on_ms> <off_ms>") {
			return 2
		}
		return c.setEffect("breath", arg(args, 1), arg(args, 2), arg(args, 3))

	case "help", "--help", "-h":
		c.out("%s", usageText)
		return 0
	}

	c.errf("ERROR: Unknown command '%s'.\n", cmd)
	c.out("%s", usageText)
	return 2
}

// after waits and then runs a command: the timer behind `identify` and `shutdown --test`.
func (c *Ctl) after(ctx context.Context, args []string) int {
	if len(args) < 2 {
		return 2
	}
	secs, err := strconv.ParseFloat(args[0], 64)
	if err != nil {
		return 2
	}
	select {
	case <-ctx.Done():
		return 0
	case <-time.After(time.Duration(secs * float64(time.Second))):
	}
	return c.Main(ctx, args[1:])
}
