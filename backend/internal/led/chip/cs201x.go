package chip

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
	cs "github.com/ugreen-nas-control/ugreen-nas-control/internal/hw/cs201x"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

// DXP2800 GT: four ARGB channels in the Chipsea CS201x embedded controller (power, network, disk1, disk2), reached
// through the host-to-EC window the fan driver uses (hw/cs201x). Registers and units after UGREEN's GPL-2.0-only
// drivers at c75c5ab (facts only, no code): ug_201x.c:102-120,375-407 (registers), leds-sio-201x.c:93-98
// (channels), :262,320-338 (50 ms units). UGREEN's LED driver is not built by its Makefile and every write call in
// it is marked TODO (leds-sio-201x.c:157,190,262,361): nothing here has run on a device yet.
//
// Off by default: the backend answers only with cs201x_leds="1" in leds.cfg. Every access runs under the lock the
// fan tool uses (cs.WithLock), one operation at a time.
const (
	layoutCS201x = "cs201x"

	argbChannels = 4

	regArgbMode   = 0x20005400 // + id: 0 off, 1 on, 2 blink, 3 breath (ug_201x.c:118,398-407)
	regArgbBright = 0x20005404 // + id, 0..255 (ug_201x.c:116,388-396)
	regArgbPeriod = 0x20005408 // + id, 50 ms units (ug_201x.c:119,262)
	regArgbDuty   = 0x2000540c // + id, 50 ms units of the on part (ug_201x.c:120)
	regArgbFlow   = 0x20005440 // UGREEN writes 1 at load ("disable argb led flow"), 0 at unload (ug_201x.c:102,790,817)
	regArgbColor  = 0x20005444 // + 4*id: blue, +1 red, +2 green (ug_201x.c:113-115,377-386)

	argbUnitMs = 50 // leds-sio-201x.c:262

	optInKey = "cs201x_leds"
)

// openCSPorts is replaced in tests.
var openCSPorts = hw.OpenPorts

func isCS201xProduct(product string) bool { return strings.HasPrefix(product, "DXP2800 GT") }

var cfgLineRe = regexp.MustCompile(`^([A-Za-z0-9_]+)="(.*)"$`)

// cs201xOptIn: cs201x_leds="1" in leds.cfg (the last such line wins), the same settings file ugreen-led-ctl reads.
func cs201xOptIn() bool {
	path := csPaths.config
	if path == "" {
		path = testenv.Get("UGREEN_LED_CONFIG")
	}
	if path == "" {
		path = "/boot/config/plugins/ugreen-nas-control/leds.cfg"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	on := false
	for _, line := range strings.Split(string(data), "\n") {
		if m := cfgLineRe.FindStringSubmatch(line); m != nil && m[1] == optInKey {
			on = m[2] == "1"
		}
	}
	return on
}

// csPaths overrides the files the backend reads and writes (tests); empty means the default.
var csPaths struct{ config, snapshot, modules string }

func snapshotPath() string {
	if csPaths.snapshot != "" {
		return csPaths.snapshot
	}
	if v := testenv.Get("UGREEN_CS201X_SNAPSHOT"); v != "" {
		return v
	}
	return "/var/run/ugreen-nas-control.led.cs201x"
}

// Kernel modules of UGREEN's CS201x drivers: while one is loaded it owns the channels.
var csModules = []string{"leds_sio_201x", "ug_201x", "ug_sio201"}

func csModuleLoaded() string {
	path := csPaths.modules
	if path == "" {
		path = testenv.Get("UGREEN_PROC_MODULES")
	}
	if path == "" {
		path = "/proc/modules"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		for _, m := range csModules {
			if strings.HasPrefix(line, m+" ") {
				return m
			}
		}
	}
	return ""
}

// csChannel is the register state of one ARGB channel.
type csChannel struct {
	Mode, Brightness, Period, Duty, R, G, B byte
}

func readChannel(ec cs.EC, id int) (c csChannel, err error) {
	n := uint32(id)
	for _, f := range []struct {
		reg uint32
		dst *byte
	}{
		{regArgbMode + n, &c.Mode}, {regArgbBright + n, &c.Brightness},
		{regArgbPeriod + n, &c.Period}, {regArgbDuty + n, &c.Duty},
		{regArgbColor + 4*n, &c.B}, {regArgbColor + 4*n + 1, &c.R}, {regArgbColor + 4*n + 2, &c.G},
	} {
		if *f.dst, err = ec.Read(f.reg); err != nil {
			return c, err
		}
	}
	return c, nil
}

// writeChannel writes colour, brightness and timing first and the mode last, so a channel never runs a new mode
// with old timing. UGREEN writes the mode first (ug_201x.c:398-407); the order is ours.
func writeChannel(ec cs.EC, id int, c csChannel) error {
	n := uint32(id)
	for _, f := range []struct {
		reg uint32
		v   byte
	}{
		{regArgbColor + 4*n, c.B}, {regArgbColor + 4*n + 1, c.R}, {regArgbColor + 4*n + 2, c.G},
		{regArgbBright + n, c.Brightness}, {regArgbPeriod + n, c.Period}, {regArgbDuty + n, c.Duty},
		{regArgbMode + n, c.Mode},
	} {
		if err := ec.Write(f.reg, f.v); err != nil {
			return err
		}
	}
	return nil
}

// csSnapshot is the state found before the first write, written back by restore.
type csSnapshot struct {
	Flow     byte        `json:"flow"`
	Channels []csChannel `json:"channels"`
}

func readSnapshot(ec cs.EC) (s csSnapshot, err error) {
	if s.Flow, err = ec.Read(regArgbFlow); err != nil {
		return s, err
	}
	for i := range argbChannels {
		c, err := readChannel(ec, i)
		if err != nil {
			return s, err
		}
		s.Channels = append(s.Channels, c)
	}
	return s, nil
}

// csDevice is the locked access to the controller: the Super-I/O window is read once per process.
type csDevice struct {
	ports hw.Ports
	ec    cs.EC
	win   cs.Window
	open  bool
}

// with runs fn under the lock with the EC window at hand. write makes sure the host interface is on (cfg 0x30 = 1,
// as UGREEN does on load, ug_201x.c:264-266); a read leaves it as found and fails when it is off.
func (d *csDevice) with(write bool, fn func(ec cs.EC) error) error {
	return cs.WithLock(func() error {
		if !d.open || (write && d.win.Active&1 == 0) {
			w, err := cs.ReadWindow(d.ports, write)
			if err != nil {
				return err
			}
			if w.Active&1 == 0 {
				return errors.New("the host interface of the embedded controller is off (cfg 0x30 = 0)")
			}
			if !cs.PlausibleBase(w.Base) {
				return fmt.Errorf("implausible base address 0x%04x of the embedded controller", w.Base)
			}
			d.win, d.ec, d.open = w, cs.EC{P: d.ports, Base: w.Base}, true
		}
		return fn(d.ec)
	})
}

// csBus is the ledBus of the DXP2800 GT.
type csBus struct{ dev csDevice }

// errCSOff is why the backend does not answer.
var errCSOff = errors.New(`the DXP2800 GT LED backend is off: set cs201x_leds="1" in leds.cfg to try it (untested on hardware)`)

func openCS201xBus(string, bool) (ledBus, error) {
	if !cs201xOptIn() {
		return nil, errCSOff
	}
	if m := csModuleLoaded(); m != "" {
		return nil, fmt.Errorf("the kernel module %s owns the CS201x LEDs; unload it first", m)
	}
	p, err := openCSPorts()
	if err != nil {
		return nil, err
	}
	return &csBus{dev: csDevice{ports: p}}, nil
}

func (b *csBus) close()          { _ = b.dev.ports.Close() }
func (b *csBus) busPath() string { return "/dev/port" }
func (b *csBus) address() string { return "cs201x" }
func csChannelID(id byte) (int, error) {
	if id >= argbChannels {
		return 0, fmt.Errorf("the DXP2800 GT has %d LEDs (power, netdev, disk1, disk2)", argbChannels)
	}
	return int(id), nil
}

// snapshotOnce stores the state found before our first write; an existing file (an earlier run, a crash) is the
// original and stays.
func snapshotOnce(ec cs.EC) error {
	if _, err := os.Stat(snapshotPath()); err == nil {
		return nil
	}
	s, err := readSnapshot(ec)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(s)
	return fsx.WriteFile(snapshotPath(), data, 0o600)
}

func (b *csBus) change(id, command byte, params [4]byte) error {
	n, err := csChannelID(id)
	if err != nil {
		return err
	}
	return b.dev.with(true, func(ec cs.EC) error {
		if err := snapshotOnce(ec); err != nil {
			return err
		}
		// Take the colours from the controller's own colour flow before the first write.
		if v, err := ec.Read(regArgbFlow); err != nil {
			return err
		} else if v != 1 {
			if err := ec.Write(regArgbFlow, 1); err != nil {
				return err
			}
		}
		c, err := readChannel(ec, n)
		if err != nil {
			return err
		}
		if err := applyCommand(&c, command, params); err != nil {
			return err
		}
		return writeChannel(ec, n, c)
	})
}

// applyCommand turns one controller command into the register state. Blink and breath take the whole cycle and the
// on part in ms and store them in 50 ms units (period = cycle, duty = on part; leds-sio-201x.c:262,320-338).
func applyCommand(c *csChannel, command byte, params [4]byte) error {
	switch command {
	case cmdColor:
		c.R, c.G, c.B = params[0], params[1], params[2]
	case cmdBrightness:
		c.Brightness = params[0]
	case cmdOnOff:
		c.Mode = params[0] & 1
	case cmdBlink, cmdBreath:
		cycle := int(params[0])<<8 | int(params[1])
		on := int(params[2])<<8 | int(params[3])
		period := min(max(cycle/argbUnitMs, 1), 255)
		c.Period, c.Duty = byte(period), byte(min(max(on/argbUnitMs, 1), period))
		c.Mode = 2
		if command == cmdBreath {
			c.Mode = 3
		}
	default:
		return fmt.Errorf("unknown command 0x%02x", command)
	}
	return nil
}

func (c csChannel) status(name string) (ledStatus, error) {
	if int(c.Mode) >= len(modeNames) {
		return ledStatus{}, fmt.Errorf("%s: mode register reads %d", name, c.Mode)
	}
	s := ledStatus{
		Name: name, Mode: modeNames[c.Mode], Brightness: int(c.Brightness),
		Color: fmt.Sprintf("%d %d %d", c.R, c.G, c.B),
	}
	if c.Mode >= 2 {
		s.OnMs = int(c.Duty) * argbUnitMs
		s.OffMs = max(int(c.Period)-int(c.Duty), 0) * argbUnitMs
	}
	return s, nil
}

func (b *csBus) status(name string) (ledStatus, error) {
	id, err := ledID(name)
	if err != nil {
		return ledStatus{}, err
	}
	n, err := csChannelID(id)
	if err != nil {
		return ledStatus{}, err
	}
	var c csChannel
	if err := b.dev.with(false, func(ec cs.EC) (err error) { c, err = readChannel(ec, n); return }); err != nil {
		return ledStatus{}, fmt.Errorf("%s: %w", name, err)
	}
	return c.status(name)
}

// restoreCS201x writes the snapshot back and removes it; without a snapshot nothing was written and nothing happens.
// A kernel module that owns the LEDs, or a snapshot that does not parse, is an error with the file kept.
func restoreCS201x(d *csDevice) (bool, error) {
	data, err := os.ReadFile(snapshotPath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if m := csModuleLoaded(); m != "" {
		return false, fmt.Errorf("the kernel module %s owns the CS201x LEDs now: nothing was written, the saved state is kept", m)
	}
	var s csSnapshot
	if err := json.Unmarshal(data, &s); err != nil || len(s.Channels) != argbChannels {
		return false, fmt.Errorf("the saved CS201x LED state in %s is unreadable: nothing was written, the file is kept", snapshotPath())
	}
	err = d.with(true, func(ec cs.EC) error {
		for i, c := range s.Channels {
			if err := writeChannel(ec, i, c); err != nil {
				return err
			}
		}
		return ec.Write(regArgbFlow, s.Flow)
	})
	if err != nil {
		return false, err // the file stays for the next try
	}
	return true, os.Remove(snapshotPath())
}

// csDump is the read-only report of `ugreen-led-i2c cs201x`.
type csDump struct {
	ChipID      string      `json:"chip_id"`
	Revision    string      `json:"revision"`
	HostEC      string      `json:"host_ec"`
	Base        string      `json:"base,omitempty"`
	Flow        *int        `json:"argb_flow_control,omitempty"`
	Channels    []csDumpLED `json:"channels,omitempty"`
	OptIn       bool        `json:"opt_in"`
	Snapshot    bool        `json:"snapshot_saved"`
	KernelOwner string      `json:"kernel_module,omitempty"`
	Notes       []string    `json:"notes,omitempty"`
}

type csDumpLED struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"` // mode register not 0; there is no separate enable register
	Mode       string `json:"mode"`
	ModeReg    int    `json:"mode_reg"`
	Brightness int    `json:"brightness"`
	Color      string `json:"color"` // R G B
	Period     int    `json:"period_units"`
	Duty       int    `json:"duty_units"`
	PeriodMs   int    `json:"period_ms"`
	OnMs       int    `json:"on_ms"`
}

// dumpCS201x only reads: it never switches the host interface on and never writes a register.
func dumpCS201x(p hw.Ports) (csDump, error) {
	r := csDump{OptIn: cs201xOptIn(), KernelOwner: csModuleLoaded()}
	_, err := os.Stat(snapshotPath())
	r.Snapshot = err == nil

	err = cs.WithLock(func() error {
		w, err := cs.ReadWindow(p, false)
		r.ChipID, r.Revision = fmt.Sprintf("0x%04x", w.ID), fmt.Sprintf("0x%04x", w.Rev)
		if err != nil {
			return err
		}
		if w.Active&1 == 0 {
			r.HostEC = "not active"
			r.Notes = append(r.Notes, "the host interface of the embedded controller is off (cfg 0x30 = 0); no register was read")
			return nil
		}
		r.HostEC = "active"
		if !cs.PlausibleBase(w.Base) {
			return fmt.Errorf("implausible base address 0x%04x", w.Base)
		}
		r.Base = fmt.Sprintf("0x%04x", w.Base)
		ec := cs.EC{P: p, Base: w.Base}
		s, err := readSnapshot(ec)
		if err != nil {
			return err
		}
		flow := int(s.Flow)
		r.Flow = &flow
		for i, c := range s.Channels {
			d := csDumpLED{
				Name: ledNames[i], Enabled: c.Mode != 0, ModeReg: int(c.Mode), Brightness: int(c.Brightness),
				Color: fmt.Sprintf("%d %d %d", c.R, c.G, c.B), Period: int(c.Period), Duty: int(c.Duty),
				PeriodMs: int(c.Period) * argbUnitMs, OnMs: int(c.Duty) * argbUnitMs, Mode: "unknown",
			}
			if int(c.Mode) < len(modeNames) {
				d.Mode = modeNames[c.Mode]
			}
			r.Channels = append(r.Channels, d)
		}
		return nil
	})
	return r, err
}

// runCS201x is `cs201x` (read-only dump) and `cs201x restore` (write the saved state back).
func (c *cli) runCS201x(args []string) int {
	p, err := openCSPorts()
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = p.Close() }()

	switch {
	case len(args) == 0:
		r, err := dumpCS201x(p)
		c.printJSON(r)
		if err != nil {
			return c.fail(err)
		}
	case len(args) == 1 && args[0] == "restore":
		done, err := restoreCS201x(&csDevice{ports: p})
		c.printJSON(map[string]any{"restored": done})
		if err != nil {
			return c.fail(err)
		}
	default:
		return c.usageError()
	}
	return 0
}
