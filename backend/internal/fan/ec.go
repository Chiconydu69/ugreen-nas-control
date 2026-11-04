package fan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ACPI devices in sysfs; tests point this at a fake tree.
var acpiDevicesDir = envOr("UGREEN_ACPI_DEVICES", "/sys/bus/acpi/devices")

// The DXP4800 GT, iDX6011 Pro and iDX6011/6012 have no Super-I/O fan chip. Their fans hang on an ITE IT5571
// embedded controller, reached through the standard ACPI EC ports (no kernel driver binds them on Unraid).
// Facts (UGREEN GPL-2.0-only drivers, drivers/ugreen/ @c75c5ab, nothing copied): data port 0x62, command/status
// port 0x66, IBF bit 1, OBF bit 0 (it55_helper.h:9-14); read = 0x80, address, wait OBF, data; write = 0x81,
// address, value (it55_helper.h:17-18, it55_helper.c:93-123). The driver waits 5 x 1 ms per flag
// (it55_helper.h:21); ours waits longer.
const (
	ecDataPort = 0x62
	ecCmdPort  = 0x66 // command when written, status when read

	ecOBF = 0x01 // output buffer full: a byte waits at the data port
	ecIBF = 0x02 // input buffer full: the controller has not taken the last byte

	ecCmdRead  = 0x80
	ecCmdWrite = 0x81

	// Bare commands (no data byte) of the DXP4800 GT: power on / stay off after AC loss
	// (ug_it55_4800gt_functions.c:55-57,134-151@c75c5ab). Only EC.SendCommand sends them, only for `power-on-ac set`.
	ecCmdACOn  = 0x88
	ecCmdACOff = 0x89

	ecPoll  = 100 * time.Microsecond
	ecTries = 3

	// The chip id sits behind the Super-I/O ports at 0x4e/0x4f.
	ecSioIndex = 0x4e
	ecSioData  = 0x4f
	ecChipID   = 0x5571

	ecModeAuto   = 0 // the controller's own curve
	ecModeManual = 1

	// Duty register range 0 to 0xc6 (198), larger values clamped (ug_it55pro_functions.c:61,64@c75c5ab;
	// ug_it55_4800gt_functions.c:71-75).
	ecMaxDuty = 0xc6

	ecRegBoardTemp = 0x5f // °C
	ecRegCPUTemp   = 0x70 // °C, follows k10temp
)

// Longest wait for IBF or OBF; a variable so that the test can shorten it.
var ecWait = 500 * time.Millisecond

// After a call timed out in every try, the controller counts as unresponsive for this long: the rest of that tick fails at once
// instead of spending ecTries x ecWait on every register. A healthy controller never reaches it. A variable for tests.
var ecDeadFor = time.Second

var (
	errECTimeout      = errors.New("embedded controller timeout")
	errECUnresponsive = errors.New("embedded controller unresponsive")
)

// IT5571 channels as wired on the DXP4800 GT: fan1 is the CPU fan, fan3 the case fan (mode 0xb0/0xb4, duty
// 0xb1/0xb5, tach 0x34-35/0x38-39: ug_it55_4800gt_functions.c:27-30,38-41@c75c5ab). fan2 and fan4 read 0 on this
// model and are left alone (compiled out there, :31-36,42-47). The tach is a 16-bit big-endian counter that UGREEN
// prints as the speed without conversion (:274-287); that it is rpm is not verified.
var ecChannels = []Channel{
	{ID: "fan1", TachHigh: 0x34, TachLow: 0x35, Duty: 0xb1, Ctrl: 0xb0},
	{ID: "fan3", TachHigh: 0x38, TachLow: 0x39, Duty: 0xb5, Ctrl: 0xb4},
}

// IT5571 channels of the iDX6011 Pro (DMI "iDX6011 Pro" exactly; no "iDX6012 Pro" exists): the two of the GT plus a
// second CPU fan (fan2) and a second system fan (fan4). Mode/duty/tach: fan1 0xb0/0xb1/0x34-35, fan2
// 0xb2/0xb3/0x36-37, fan3 0xb4/0xb5/0x38-39, fan4 0xb6/0xb7/0x3a-3b (ug_it55pro_functions.c:24-40@c75c5ab,
// tools/it55_regs_dump.sh:5-12,30-37). UGREEN drives cpu = fan1+fan2 and sys = fan3+fan4 with one duty each
// (ug_it55pro_functions.c:62-88). The map is vendor-sourced; that fan2 and fan4 answer on a unit is unconfirmed.
var ecChannelsIdxPro = []Channel{
	ecChannels[0],
	{ID: "fan2", TachHigh: 0x36, TachLow: 0x37, Duty: 0xb3, Ctrl: 0xb2},
	ecChannels[1],
	{ID: "fan4", TachHigh: 0x3a, TachLow: 0x3b, Duty: 0xb7, Ctrl: 0xb6},
}

// Plain iDX6011 / iDX6012, two system fans in another block (ug_it55_functions.c:24-31@c75c5ab): fan1 mode 0x9c,
// duty 0x9d, tach 0x96-97; fan2 mode 0x9e, duty 0x9f, tach 0x98-99. 0x98/0x99 is a tach there, not the watchdog.
var ecChannelsIdx = []Channel{
	{ID: "fan1", TachHigh: 0x96, TachLow: 0x97, Duty: 0x9d, Ctrl: 0x9c},
	{ID: "fan2", TachHigh: 0x98, TachLow: 0x99, Duty: 0x9f, Ctrl: 0x9e},
}

// idxProFloorPct: the iDX6011 Pro case fan stalls below about 20 %, so the driver never writes less than 25 %
// (community, noodlemctwoodle src/panel/fan.h:25-31; UGREEN has no floor).
const idxProFloorPct = 25

// ecWdt are the watchdog registers of one EC map: enable, timeout MSB and LSB (seconds, big-endian).
type ecWdt struct{ enable, hi, lo byte }

var (
	// GT 0x98/0xa2/0xa3 (ug_it55_4800gt_functions.c:534-536@c75c5ab); the Pro uses the same (ug_it55pro_functions.c:525-527).
	ecWdtGT = &ecWdt{ecRegWdtEnable, ecRegWdtHi, ecRegWdtLo}
	// Plain iDX 0x55/0x56/0x57 (ug_it55_functions.c:470-472). Never written by this tool.
	ecWdtIdx = &ecWdt{0x55, 0x56, 0x57}
)

var (
	ecSpecGT = ecSpec{channels: ecChannels, wdt: ecWdtGT, acCmd: &ecACCmd{on: ecCmdACOn, off: ecCmdACOff}}
	// Pro: besides the fans and the watchdog timeout only the backlight register may be written. fan4 reading 0
	// while fan3 spins shows fan3's value, as UGREEN reports it (ug_it55pro_functions.c:290-291@c75c5ab).
	ecSpecIdxPro = ecSpec{
		channels: ecChannelsIdxPro, wdt: ecWdtGT, floor: byte((idxProFloorPct*ecMaxDuty + 99) / 100),
		extraWrite: []byte{ecRegBacklight}, mirror: map[string]string{"fan4": "fan3"},
	}
	// Plain iDX6011/6012: read only (tach, duty, mode, watchdog flag); no write reaches hardware nobody tested.
	// UGREEN gates on the DMI name only (ug_it55_functions.c:583-585), so another chip id is a warning here.
	ecSpecIdx = ecSpec{channels: ecChannelsIdx, wdt: ecWdtIdx, readOnly: true, softID: true, noTemps: true}
)

// ecACCmd are the two power-on-after-AC-loss commands of a map.
type ecACCmd struct{ on, off byte }

type ecSpec struct {
	channels   []Channel
	floor      byte              // duty register value; 0 = no floor
	wdt        *ecWdt            // nil = no watchdog code for this map
	extraWrite []byte            // writable besides channel mode/duty and the watchdog timeout
	mirror     map[string]string // channel id -> channel whose tach it shows while its own reads 0
	readOnly   bool
	softID     bool     // a foreign chip id is a warning, not a refusal
	noTemps    bool     // 0x5f/0x70 are not known for this map
	acCmd      *ecACCmd // nil = no AC-loss command for this map (the Pro and plain iDX use memory-mapped registers)
}

var (
	errReadOnly   = errors.New("this model is read only: no register is written")
	errNotAllowed = errors.New("register is not on this model's write list")
)

type EC struct {
	ports     Ports
	spec      ecSpec
	idNote    string    // chip id warning of a softID spec
	fed       bool      // this process fed the watchdog (Petter.tickEC); only then does Restore clear the enable byte
	deadUntil time.Time // unresponsive until then (see ecDeadFor)
}

func (c *EC) sp() ecSpec {
	if c.spec.channels == nil {
		return ecSpecGT
	}
	return c.spec
}

func (c *EC) channels() []Channel { return c.sp().channels }

// writable is the per-model write list: fan mode and duty, the watchdog timeout (the enable byte only with 0, see
// write), and the spec's extras. Anything else, e.g. the wake, EUP and beeper registers 0x99/0x9a/0x9c/0x9e/0xa6
// that other models of the family use (it55_regs_dump.sh:21-24, ug_sataio_beeper.c:31-35@c75c5ab), is refused.
func (c *EC) writable(reg byte) bool {
	sp := c.sp()
	if sp.readOnly {
		return false
	}
	for _, ch := range sp.channels {
		if reg == ch.Ctrl || reg == ch.Duty {
			return true
		}
	}
	if w := sp.wdt; w != nil && (reg == w.enable || reg == w.hi || reg == w.lo) {
		return true
	}
	return slices.Contains(sp.extraWrite, reg)
}

func DetectEC(p Ports) (*EC, error) { return detectEC(p, ecSpecGT) }

func detectEC(p Ports, spec ecSpec) (*EC, error) {
	// With the kernel's ACPI EC driver bound, both sides would talk to the
	// controller at once and garble each other's transfers.
	bound, _ := filepath.Glob(filepath.Join(acpiDevicesDir, "PNP0C09:*", "driver"))
	for _, d := range bound {
		if _, err := os.Lstat(d); err == nil {
			return nil, fmt.Errorf("the kernel's ACPI embedded controller driver is bound (%s); refusing to share the controller with it", filepath.Dir(d))
		}
	}

	read := func(reg byte) (byte, error) {
		if err := p.Out(ecSioIndex, reg); err != nil {
			return 0, err
		}
		return p.In(ecSioData)
	}
	hi, err := read(0x20)
	if err != nil {
		return nil, err
	}
	lo, err := read(0x21)
	if err != nil {
		return nil, err
	}
	c := &EC{ports: p, spec: spec}
	if id := uint16(hi)<<8 | uint16(lo); id != ecChipID {
		if !spec.softID {
			return nil, fmt.Errorf("no supported fan controller found (embedded controller id 0x%04x)", id)
		}
		c.idNote = fmt.Sprintf("chip id 0x%04x, expected 0x%04x; UGREEN checks the model name only", id, ecChipID)
	}
	for _, ch := range c.channels() {
		mode, err := c.read(ch.Ctrl)
		if err != nil {
			return nil, fmt.Errorf("IT5571 embedded controller does not answer: %w", err)
		}
		if mode > ecModeManual {
			return nil, fmt.Errorf("IT5571 embedded controller answers unexpectedly (mode 0x%02x)", mode)
		}
	}
	return c, nil
}

func (c *EC) wait(flag byte, set bool) error {
	deadline := time.Now().Add(ecWait)
	for {
		st, err := c.ports.In(ecCmdPort)
		if err != nil {
			return err
		}
		if (st&flag != 0) == set {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (status 0x%02x)", errECTimeout, st)
		}
		time.Sleep(ecPoll)
	}
}

func (c *EC) send(port uint16, v byte) error {
	if err := c.wait(ecIBF, false); err != nil {
		return err
	}
	return c.ports.Out(port, v)
}

// revive clears the dead window: a hand-back (Restore, disarming the watchdog) always gets the full retry budget.
func (c *EC) revive() { c.deadUntil = time.Time{} }

func (c *EC) retry(fn func() error) (err error) {
	if time.Now().Before(c.deadUntil) {
		return errECUnresponsive
	}
	for i := 0; i < ecTries; i++ {
		if err = fn(); err == nil {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 5 * time.Millisecond)
	}
	if errors.Is(err, errECTimeout) {
		c.deadUntil = time.Now().Add(ecDeadFor)
	}
	return err
}

func (c *EC) read(reg byte) (v byte, err error) {
	err = c.retry(func() error {
		// A byte left over from an aborted read would be taken for the answer.
		if st, err := c.ports.In(ecCmdPort); err == nil && st&ecOBF != 0 {
			_, _ = c.ports.In(ecDataPort)
		}
		if err := c.send(ecCmdPort, ecCmdRead); err != nil {
			return err
		}
		if err := c.send(ecDataPort, reg); err != nil {
			return err
		}
		if err := c.wait(ecOBF, true); err != nil {
			return err
		}
		v, err = c.ports.In(ecDataPort)
		return err
	})
	return
}

func (c *EC) write(reg, value byte) error {
	if c.sp().readOnly {
		return errReadOnly
	}
	if w := c.sp().wdt; w != nil && reg == w.enable && value != 0 {
		return errNeverArm
	}
	if !c.writable(reg) {
		return fmt.Errorf("%w: 0x%02x", errNotAllowed, reg)
	}
	return c.retry(func() error {
		if err := c.send(ecCmdPort, ecCmdWrite); err != nil {
			return err
		}
		if err := c.send(ecDataPort, reg); err != nil {
			return err
		}
		return c.send(ecDataPort, value)
	})
}

// SendCommand sends one bare command byte to the command port: only the AC-loss commands of the model's map, and
// never on a read-only map. The controller reports nothing back that this code could trust (the vendor's check of
// status bit 0x08 only says the last byte was a command), so success means the byte was taken, not that it applied.
func (c *EC) SendCommand(cmd byte) error {
	sp := c.sp()
	if sp.readOnly {
		return errReadOnly
	}
	if sp.acCmd == nil || (cmd != sp.acCmd.on && cmd != sp.acCmd.off) {
		return fmt.Errorf("%w: command 0x%02x", errNotAllowed, cmd)
	}
	if err := c.send(ecCmdPort, cmd); err != nil {
		return err
	}
	return c.wait(ecIBF, false)
}

func (c *EC) Name() string { return "IT5571" }

func (c *EC) Describe() string { return "IT5571 embedded controller" }

func (c *EC) Details() map[string]any {
	d := map[string]any{"ports": fmt.Sprintf("0x%02x/0x%02x", ecDataPort, ecCmdPort)}
	if c.idNote != "" {
		d["warning"] = c.idNote
	}
	if c.sp().readOnly {
		d["read_only"] = true
	}
	return d
}

func (c *EC) Outputs() []Channel { return c.channels() }

func (c *EC) MaxDuty() byte { return ecMaxDuty }

// In auto mode the duty register keeps the last manual value, not the real output.
func (c *EC) AutoDutyKnown() bool { return false }

// RPM is the tach counter of the channel, 16-bit big-endian. A channel with a mirror rule (iDX6011 Pro fan4) that
// reads 0 while its sibling spins reports the sibling's value, so it is neither shown nor treated as stopped.
func (c *EC) RPM(ch Channel) (int, error) {
	v, err := c.tach(ch)
	if err != nil || v != 0 {
		return v, err
	}
	if src, ok := c.sp().mirror[ch.ID]; ok {
		for _, o := range c.channels() {
			if o.ID == src {
				if sv, err := c.tach(o); err == nil && sv > 0 {
					return sv, nil
				}
			}
		}
	}
	return 0, nil
}

func (c *EC) tach(ch Channel) (int, error) {
	hi, err := c.read(ch.TachHigh)
	if err != nil {
		return 0, err
	}
	lo, err := c.read(ch.TachLow)
	if err != nil {
		return 0, err
	}
	return int(hi)<<8 | int(lo), nil
}

func (c *EC) PWM(ch Channel) (duty byte, auto bool, err error) {
	if duty, err = c.read(ch.Duty); err != nil {
		return 0, false, err
	}
	mode, err := c.read(ch.Ctrl)
	if err != nil {
		return 0, false, err
	}
	return duty, mode == ecModeAuto, nil
}

func (c *EC) SetPWM(ch Channel, duty byte) error {
	if c.sp().readOnly {
		return errReadOnly
	}
	if duty > ecMaxDuty {
		duty = ecMaxDuty
	}
	if duty < c.sp().floor {
		duty = c.sp().floor
	}
	mode, err := c.read(ch.Ctrl)
	if err != nil {
		return err
	}
	// Duty first, then manual mode: the fan never sits at the stale duty.
	if err := c.write(ch.Duty, duty); err != nil {
		return err
	}
	if mode != ecModeManual {
		return c.write(ch.Ctrl, ecModeManual)
	}
	return nil
}

func (c *EC) temp(reg byte) *int {
	if c.sp().noTemps {
		return nil
	}
	v, err := c.read(reg)
	if err != nil || v == 0 || v == 0xff {
		return nil
	}
	t := int(v)
	return &t
}

// BoardTemp is the mainboard temperature in °C; the GT has no acpitz sensor.
func (c *EC) BoardTemp() *int { return c.temp(ecRegBoardTemp) }

// Temps in °C: the EC's own CPU reading (the mainboard goes through BoardTemp).
func (c *EC) Temps() []*int { return []*int{c.temp(ecRegCPUTemp)} }

func (c *EC) Snapshot() (Snapshot, error) {
	s := Snapshot{}
	for _, ch := range c.channels() {
		mode, err := c.read(ch.Ctrl)
		if err != nil {
			return nil, err
		}
		duty, err := c.read(ch.Duty)
		if err != nil {
			return nil, err
		}
		s[ch.ID] = [2]byte{mode, duty}
	}
	return s, nil
}

// Restore hands the fans back to the EC's own curve, whatever the snapshot
// says: it may have been taken while a fan was still in manual mode. The watchdog enable byte is cleared only if
// this process fed the timer (a stop must not leave an armed timer that nobody feeds); a timer somebody else armed
// is left alone, also on the `restore` command, which never feeds.
func (c *EC) Restore(Snapshot) error {
	if c.sp().readOnly {
		return nil // nothing was written
	}
	var errs []error
	for _, ch := range c.channels() {
		c.revive()
		errs = append(errs, c.write(ch.Ctrl, ecModeAuto))
	}
	// Last, so that a stop never leaves an armed watchdog that nobody feeds.
	if c.fed && c.sp().wdt != nil {
		err := c.DisarmWatchdog()
		errs = append(errs, err)
		if err == nil {
			c.fed = false
		}
	}
	return errors.Join(errs...)
}
