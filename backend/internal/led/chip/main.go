// Package chip drives the LED controller of UGREEN NAS devices directly over SMBus (/dev/i2c-N) or, on the
// DXP4800 GT, through /dev/mem, so no kernel module is needed: the protocol, the effects and the ugreen-led-i2c
// command line (Run).
package chip

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

const usage = `Usage:
  ugreen-led-i2c [--bus /dev/i2c-N] [--force] probe
  ugreen-led-i2c --debug <command>
  ugreen-led-i2c [--bus /dev/i2c-N] [--force] [--plain] get <led[,led...]|all>
  ugreen-led-i2c [--bus /dev/i2c-N] [--force] set <led[,led...]|all> <command>
  ugreen-led-i2c [--bus /dev/i2c-N] scan [all [--include-display]]
  ugreen-led-i2c [--bus /dev/i2c-N] [--force] dxp480t white
  ugreen-led-i2c cs201x [restore]
  ugreen-led-i2c lpss-probe [--write]
  ugreen-led-i2c [--speed 0-4] [--reverse] [--idle-only] effect <rainbow|temperature> <led[,led...]|all>

LEDs:     power, netdev, disk1 ... disk8
Commands: color <R> <G> <B> | brightness <0-255> | on | off
          blink <on_ms> <off_ms> | breath <on_ms> <off_ms>
          reset (iDX only: leave a stuck blink/breath mode, then send on)

scan      lists the I2C addresses that answer (read only), on one bus or with
          "all" on every /dev/i2c-N except graphics (display) buses, which --include-display adds,
          for models whose controller isn't at 0x3a
dxp480t   DXP480T Plus: the power LED (controller at 0x26) to static white
cs201x    DXP2800 GT: dump the ARGB registers of the four LEDs (read only); "restore" writes
          back the state saved before the first write. The LED backend of this model is off
          until cs201x_leds="1" is set in leds.cfg
lpss-probe DXP2800 SUB2 boards: the LED controller sits behind an Intel LPSS I2C controller without a
          kernel driver. Read only by default: prints every check (ACPI path, PCI function, driver, power state,
          BAR0, DesignWare type) and stops before any write. With --write it then sets up the controller
          (writes controller registers, never an LED frame) and reads the chip id. Needs no opt-in; LED
          control uses this path by default when every check passes (lpss_leds="0" in leds.cfg turns it off)

--bus     the SMBus device (default: the "SMBus I801 adapter")
--debug   print every step of the DXP2800 SUB2 path (power, bring-up, each transfer) to stderr;
          UNC_LPSS_DEBUG=1 does the same
--force   use the address even while a kernel driver (led_ugreen) holds it
--plain   get prints one line per LED for shell scripts:
          <name> <mode> <brightness> <R> <G> <B> <on_ms> <off_ms>

effect runs until SIGTERM: rainbow moves a colour wave over the LEDs
(--speed, --reverse), temperature colours them by the CPU temperature
(blue 35 °C, green 45 °C, yellow 55 °C, red 65 °C). --idle-only pauses it
while any disk is busy.
`

// Run is the ugreen-led-i2c command line: it returns the exit code and writes to the given streams, so the
// LED daemon can drive the chip in-process (ledctl.InProcChip) as well as the led-i2c tool of cmd/ugreen-nas.
func Run(args []string, stdout, stderr io.Writer) int {
	return (&cli{out: stdout, err: stderr}).run(args)
}

type cli struct{ out, err io.Writer }

func (c *cli) fail(err error) int {
	_, _ = fmt.Fprintln(c.err, "ERROR:", err)
	return 1
}

// failErr is fail with the exit code of the state: ExitRejected when the controller rejects everything.
func (c *cli) failErr(err error) int {
	rc := c.fail(err)
	if errors.Is(err, ErrRejected) {
		return ExitRejected
	}

	return rc
}

func (c *cli) usageError() int {
	_, _ = fmt.Fprint(c.err, usage)
	return 2
}

func (c *cli) printJSON(value any) {
	out, _ := json.MarshalIndent(value, "", "  ")
	_, _ = fmt.Fprintln(c.out, string(out))
}

// ledBus is what the commands need from the controller; tests replace it with a fake.
type ledBus interface {
	change(id, command byte, params [4]byte) error
	status(name string) (ledStatus, error)
	close()
	busPath() string
}

var openLedBus = func(path string, force bool) (ledBus, error) {
	if getLayout() == layoutCS201x {
		return openCS201xBus(path, force)
	}
	b, err := openBus(path, force)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (c *cli) run(args []string) int {
	flags := flag.NewFlagSet("ugreen-led-i2c", flag.ContinueOnError)
	flags.SetOutput(c.err)
	flags.Usage = func() { _, _ = fmt.Fprint(c.err, usage) }
	busPath := flags.String("bus", "", "")
	force := flags.Bool("force", false, "")
	plain := flags.Bool("plain", false, "")
	speed := flags.Int("speed", 2, "")
	reverse := flags.Bool("reverse", false, "")
	idleOnly := flags.Bool("idle-only", false, "")
	debug := flags.Bool("debug", false, "")

	if err := flags.Parse(args); err != nil {
		return 2
	}
	setLPSSDebug(*debug, c.err)
	args = flags.Args()

	if len(args) == 0 || (args[0] != "probe" && args[0] != "scan" && args[0] != "cs201x" && args[0] != "lpss-probe" && len(args) < 2) {
		return c.usageError()
	}

	if args[0] == "cs201x" {
		return c.runCS201x(args[1:])
	}

	if args[0] == "lpss-probe" {
		if len(args) > 2 || (len(args) == 2 && args[1] != "--write") {
			return c.usageError()
		}
		return c.runLPSSProbe(len(args) == 2)
	}

	if args[0] == "scan" {
		return c.runScan(*busPath, args[1:])
	}

	if args[0] == "dxp480t" {
		if len(args) != 2 || args[1] != "white" {
			return c.usageError()
		}
		return c.runDXP480T(*busPath, *force)
	}

	if args[0] == "effect" {
		if getLayout() == layoutCS201x {
			return c.fail(errors.New("effects are not supported on the DXP2800 GT"))
		}
		if len(args) != 3 {
			return c.usageError()
		}
		leds, err := parseLeds(args[2])
		if err != nil {
			return c.fail(err)
		}
		options := effectOptions{
			name: args[1], leds: leds, speed: *speed, reverse: *reverse,
			idleOnly: *idleOnly, busPath: *busPath, force: *force,
		}
		if err := validateEffect(options); err != nil {
			return c.fail(err)
		}
		if err := runEffect(options); err != nil {
			return c.fail(err)
		}
		return 0
	}

	var leds []string
	var command byte
	var params [4]byte

	switch args[0] {
	case "probe":
		leds = ledNames
	case "get", "set":
		var err error
		if leds, err = parseLeds(args[1]); err != nil {
			return c.fail(err)
		}
		if args[0] == "set" {
			if command, params, err = buildCommand(args[2:]); err != nil {
				return c.fail(err)
			}
		}
	default:
		return c.usageError()
	}

	b, err := openLedBus(*busPath, *force)
	if err != nil {
		return c.fail(err)
	}
	defer b.close()

	switch args[0] {
	case "probe":
		var found []string
		for _, name := range leds {
			_, err := b.status(name)
			if err == nil {
				found = append(found, name)
			} else if errors.Is(err, ErrRejected) {
				return c.failErr(err) // the other LEDs would fail the same way, five retries each
			}
		}
		c.printJSON(map[string]any{"bus": b.busPath(), "address": probeAddress(b), "leds": found})
		if len(found) == 0 {
			return c.fail(fmt.Errorf("no LED answered on %s", b.busPath()))
		}

	case "get":
		statuses := map[string]ledStatus{}
		for _, name := range leds {
			s, err := b.status(name)
			if err != nil {
				// "all" lists what exists; a named LED that doesn't answer is an error.
				if args[1] == "all" && !errors.Is(err, ErrRejected) {
					continue
				}
				return c.failErr(err)
			}
			statuses[name] = s
		}
		if *plain {
			for _, name := range leds {
				if s, ok := statuses[name]; ok {
					_, _ = fmt.Fprintln(c.out, plainLine(s))
				}
			}
		} else if len(leds) == 1 {
			c.printJSON(statuses[leds[0]])
		} else {
			c.printJSON(statuses)
		}

	case "set":
		for _, name := range leds {
			id, _ := ledID(name)
			if err := b.change(id, command, params); err != nil {
				return c.failErr(fmt.Errorf("%s: %w", name, err))
			}
		}
	}

	return 0
}

// runScan handles "scan [all [--include-display]]". With all, graphics (display) buses are listed as skipped
// unless --include-display is given.
func (c *cli) runScan(busPath string, args []string) int {
	all, includeDisplay := false, false
	for i, a := range args {
		switch {
		case i == 0 && a == "all":
			all = true
		case all && a == "--include-display":
			includeDisplay = true
		default:
			return c.usageError()
		}
	}
	paths, err := scanBuses(busPath, all)
	if err != nil {
		return c.fail(err)
	}

	nameOf := adapterName
	if !all {
		nameOf = func(string) string { return "" }
	}
	streamScan(bufio.NewWriter(c.out), planScan(paths, includeDisplay || !all, nameOf), scanBus)
	return 0
}

// The DXP480T Plus has a single power LED, on a simpler controller at 0x26. These are
// the register writes of ich777's ugreenleds-driver (tested there on a DXP480T Plus).
func (c *cli) runDXP480T(busPath string, force bool) int {
	b, err := openBusAt(busPath, dxp480tAddr, force)
	if err != nil {
		return c.fail(err)
	}
	defer b.close()

	for _, w := range dxp480tWhite {
		if err := b.writeByteData(w[0], w[1]); err != nil {
			return c.fail(fmt.Errorf("0x26 register 0x%02x: %w", w[0], err))
		}
	}
	return 0
}

// probeAddress is the controller address shown by probe; the DXP2800 GT has none (it is an embedded controller).
func probeAddress(b ledBus) string {
	if a, ok := b.(interface{ address() string }); ok {
		return a.address()
	}
	return "0x3a"
}
