package fan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
)

// Fan driver of the DXP2800 GT (CS201x embedded controller). Registers and sequence after UGREEN's GPL-2.0-only
// driver ug_201x.c@c75c5ab (facts only): the fan registers are plain bytes behind the host-to-EC window, the duty
// is a percentage (0..100, ug_201x.c:311-328), the speed register is rpm (ug_201x.c:293-309). The driver has no
// automatic mode and does not hand the fans back on unload (ug_201x.c:816-832), so Restore writes the duty found
// at the start back.
//
// Off by default: the model counts as a fan model only with a confirmed map (`cs201x confirm`,
// stored in fan_map.json like the IT8613E models). The host interface (cfg 0x30) is never switched on: with
// it off the BIOS does not expose the fan controller, and Snapshot, SetPWM and Restore refuse.
const (
	csChip = "CS201x"

	// csFloorPct: no duty below this is written. The stall point of the fans is not measured on this model; the
	// other GT-class models stall near 20 % (see probeFloorPct).
	csFloorPct = probeFloorPct
	// csSafePct is written back when no usable duty was saved: UGREEN's driver has no automatic mode to return to.
	csSafePct = 60
	// csMaxRPM: a reading above this (a floating bus reads 0xffff) is no fan.
	csMaxRPM = 30000
)

// csWrotePath marks that this tool wrote a CS201x duty since boot and has not handed the fans back. A snapshot taken
// while it exists would hold our own duty, not the BIOS's. /var/run is cleared on boot, as the fan state is.
var csWrotePath = envOr("UGREEN_FAN_CS201X_WROTE", "/var/run/ugreen-nas-control.cs201x-wrote")

type csFan struct {
	id, role      string
	en, duty, spd uint32
}

// fan1 is the CPU fan (UGREEN fan0, "cpufan"), fan3 the system fan (UGREEN fan1, "sysfan1"): ug_201x.c:86-94,453-457.
var csFans = []csFan{
	{"fan1", "cpu", csRegFan0En, csRegFan0Duty, csRegFan0Spd},
	{"fan3", "case", csRegFan1En, csRegFan1Duty, csRegFan1Spd},
}

func csFanOf(id string) (csFan, bool) {
	i := slices.IndexFunc(csFans, func(f csFan) bool { return f.id == id })
	if i < 0 {
		return csFan{}, false
	}
	return csFans[i], true
}

type CS201x struct {
	p        Ports
	ec       csEC
	chans    []Channel
	active   bool // the host interface is on
	hostOn   bool // writes allowed: the host interface was on when checked
	prepared bool // fan enables and temperature inputs are set
}

var errCSHostOff = errors.New("host interface off: the BIOS does not expose the fan controller")

var errCSNotConfirmed = errors.New("fan control of the DXP2800 GT is off until you confirm the fans: /usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan cs201x confirm")

// DetectCS201x opens the driver for a confirmed map. It only reads.
func DetectCS201x(p Ports, model string) (*CS201x, error) {
	m, err := loadCS201xMap(model)
	if err != nil {
		if errors.Is(err, errNoFanMap) {
			return nil, errCSNotConfirmed
		}
		return nil, err
	}
	w, err := csReadWindow(p)
	if err != nil {
		return nil, err
	}
	if !plausibleBase(w.Base) {
		return nil, fmt.Errorf("implausible base address 0x%04x of the embedded controller", w.Base)
	}
	c := &CS201x{p: p, ec: csEC{P: p, Base: w.Base}, active: w.Active&0x01 != 0}
	for _, o := range m.Outputs {
		c.chans = append(c.chans, Channel{ID: o.ID})
	}
	return c, nil
}

func (c *CS201x) Name() string     { return csChip }
func (c *CS201x) Describe() string { return "Chipsea CS201x embedded controller" }
func (c *CS201x) Details() map[string]any {
	return map[string]any{"base": fmt.Sprintf("0x%04x", c.ec.Base), "host_interface": c.active}
}
func (c *CS201x) Outputs() []Channel  { return c.chans }
func (c *CS201x) MaxDuty() byte       { return 100 }  // percent
func (c *CS201x) AutoDutyKnown() bool { return true } // no automatic mode: the register is the output

// hostReady checks that the host interface is on (it never switches it on); every snapshot and write path calls it.
func (c *CS201x) hostReady() error {
	if c.hostOn {
		return nil
	}
	w, err := csReadWindow(c.p)
	if err != nil {
		return err
	}
	if w.Active&0x01 == 0 {
		c.active = false
		return errCSHostOff
	}
	if !plausibleBase(w.Base) {
		return fmt.Errorf("implausible base address 0x%04x of the embedded controller", w.Base)
	}
	c.ec.Base, c.active, c.hostOn = w.Base, true, true
	return nil
}

// prepare sets what UGREEN sets at load before it uses the fans and temperatures (ug_201x.c:267-272), but only
// where it reads 0: the fan enables, and the temperature input of a channel whose temperature reads 0.
func (c *CS201x) prepare() error {
	if c.prepared {
		return nil
	}
	if err := c.hostReady(); err != nil {
		return err
	}
	set := func(reg uint32) error {
		if v, err := c.ec.Read(reg); err != nil || v != 0 {
			return err
		}
		return c.ec.Write(reg, 1)
	}
	for _, ch := range c.chans {
		f, _ := csFanOf(ch.ID)
		if err := c.setEnableBit(f.en, 1); err != nil {
			return err
		}
	}
	for _, t := range []struct{ temp, cfg uint32 }{{csRegNTC2, csRegADC2Cfg}, {csRegNTC4, csRegADC4Cfg}} {
		if v, err := c.ec.Read(t.temp); err != nil {
			return err
		} else if v == 0 {
			if err := set(t.cfg); err != nil {
				return err
			}
		}
	}
	c.prepared = true
	return nil
}

// setEnableBit sets bit 0 of an enable register to bit, leaves the other bits alone and writes only when bit 0 differs.
func (c *CS201x) setEnableBit(reg uint32, bit byte) error {
	v, err := c.ec.Read(reg)
	if err != nil || v&1 == bit {
		return err
	}
	return c.ec.Write(reg, v&^1|bit)
}

// readable: reads need the interface on too; it is looked at again while it was off (the LED backend may have switched it on).
func (c *CS201x) readable() error {
	if c.active {
		return nil
	}
	return c.hostReady()
}

func (c *CS201x) RPM(ch Channel) (int, error) {
	f, ok := csFanOf(ch.ID)
	if !ok {
		return 0, fmt.Errorf("unknown fan %q", ch.ID)
	}
	if err := c.readable(); err != nil {
		return 0, err
	}
	return csRPM(c.ec, f.spd)
}

func (c *CS201x) PWM(ch Channel) (byte, bool, error) {
	f, ok := csFanOf(ch.ID)
	if !ok {
		return 0, false, fmt.Errorf("unknown fan %q", ch.ID)
	}
	if err := c.readable(); err != nil {
		return 0, false, err
	}
	d, err := c.ec.Read(f.duty)
	return d, false, err
}

func (c *CS201x) SetPWM(ch Channel, duty byte) error {
	f, ok := csFanOf(ch.ID)
	if !ok {
		return fmt.Errorf("unknown fan %q", ch.ID)
	}
	if err := c.prepare(); err != nil {
		return err
	}
	want := min(max(duty, csFloorPct), 100)
	// Marked before the first write: a crash between the write and the mark would leave our duty unmarked.
	if err := fsx.WriteFile(csWrotePath, []byte("1\n"), 0o644); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot mark the fan duty as written: %w", err)
	}
	// Read back: a duty the controller did not take (one retry) is an error, not a silent no-op.
	for try := 0; ; try++ {
		if err := c.ec.Write(f.duty, want); err != nil {
			return err
		}
		got, err := c.ec.Read(f.duty)
		if err != nil {
			return err
		}
		if got == want {
			return nil
		}
		if try == 1 {
			return fmt.Errorf("%s duty reads %d after writing %d", ch.ID, got, want)
		}
	}
}

func (c *CS201x) temp(reg uint32) *int {
	if c.readable() != nil {
		return nil
	}
	v, err := c.ec.Read(reg)
	if err != nil || v == 0 || v == 0xff {
		return nil
	}
	t := int(v)
	if !plausibleTemp(t) {
		return nil
	}
	return &t
}

// Temps: NTC2, UGREEN's cpu_temp. The mainboard (NTC4, board_temp) goes through BoardTemp.
func (c *CS201x) Temps() []*int   { return []*int{c.temp(csRegNTC2)} }
func (c *CS201x) BoardTemp() *int { return c.temp(csRegNTC4) }

// Snapshot is {enable, duty} per fan as found. It refuses while the host interface is off and writes nothing. If this tool wrote a duty since boot (csWrotePath), the duty register holds ours, not the BIOS's: it
// is saved as 0, which Restore turns into the safe duty.
func (c *CS201x) Snapshot() (Snapshot, error) {
	if err := c.hostReady(); err != nil {
		return nil, err
	}
	s := Snapshot{}
	_, werr := os.Stat(csWrotePath)
	ours := werr == nil
	for _, ch := range c.chans {
		f, _ := csFanOf(ch.ID)
		en, err := c.ec.Read(f.en)
		if err != nil {
			return nil, err
		}
		duty, err := c.ec.Read(f.duty)
		if err != nil {
			return nil, err
		}
		if ours {
			duty = 0
		}
		s[ch.ID] = [2]byte{en, duty}
	}
	return s, nil
}

// Restore writes the saved duty back, but never below csSafePct, and the enable value where it differs. There is no
// automatic mode to give the fans to, so what stays behind must be safe: a saved duty below the floor (the BIOS
// value was lost, or a snapshot kept across restarts holds one we wrote) and a missing or 0 duty get csSafePct.
func (c *CS201x) Restore(s Snapshot) error {
	if err := c.hostReady(); err != nil {
		return err
	}
	var errs []error
	for _, ch := range c.chans {
		f, _ := csFanOf(ch.ID)
		saved, have := s[ch.ID]
		duty := byte(csSafePct)
		if have {
			duty = min(max(saved[1], csSafePct), 100)
		}
		errs = append(errs, c.ec.Write(f.duty, duty))
		if have {
			errs = append(errs, c.setEnableBit(f.en, saved[0]&1))
		}
	}
	err := errors.Join(errs...)
	if err == nil {
		_ = os.Remove(csWrotePath)
	}
	return err
}

// forgetCS201x hands the fans back before the map goes: without the map nothing can open the CS201x again, and
// the duty we wrote would stay. Without a saved BIOS snapshot nothing was ever written, so the map just goes. A failed
// restore keeps the map.
func forgetCS201x(p Ports, model string) error {
	if _, err := os.Stat(biosPath); err == nil {
		if err := withLock(func() error {
			c, err := DetectCS201x(p, model)
			if err != nil {
				if errors.Is(err, errCSNotConfirmed) {
					return nil // no map: nothing to open
				}
				return err
			}
			var s Snapshot
			if b, err := os.ReadFile(biosPath); err == nil {
				_ = json.Unmarshal(b, &s)
			}
			return c.Restore(s)
		}); err != nil {
			return fmt.Errorf("cannot hand the fans back, map kept: %w", err)
		}
	}
	return forgetMap()
}

// loadCS201xMap reads the confirmed map of the DXP2800 GT from fan_map.json; errNoFanMap when there is none.
func loadCS201xMap(model string) (*FanMap, error) {
	b, err := os.ReadFile(fanMapPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoFanMap
	}
	if err != nil {
		return nil, err
	}
	var m FanMap
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("fan map unreadable: %w", err)
	}
	if m.Version != 1 || m.Chip != csChip {
		return nil, errors.New("fan map ignored: unknown format")
	}
	if m.Model != model {
		return nil, fmt.Errorf("fan map ignored: made for %q, this is %q", m.Model, model)
	}
	var seen []string
	for _, o := range m.Outputs {
		f, ok := csFanOf(o.ID)
		if !ok || f.role != o.Role || slices.Contains(seen, o.ID) {
			return nil, fmt.Errorf("fan map ignored: bad entry %q", o.ID)
		}
		seen = append(seen, o.ID)
	}
	if len(seen) == 0 {
		return nil, errors.New("fan map ignored: no fans in it")
	}
	return &m, nil
}

// confirmCS201x saves the fans the user names (default: all) as the confirmed map. With the host interface on, a
// fan that reads no rpm is refused (nothing is connected there). It reads only.
func confirmCS201x(p Ports, model string, ids []string) (*FanMap, error) {
	if len(ids) == 0 {
		for _, f := range csFans {
			ids = append(ids, f.id)
		}
	}
	rd, err := ProbeCS201x(p, model)
	if err != nil {
		return nil, err
	}
	if rd.HostEC != "active" {
		return nil, errors.New("host interface off: the BIOS does not expose the fan controller, so no fan could be checked")
	}
	m := &FanMap{Version: 1, Model: model, Chip: csChip, Time: time.Now().Unix()}
	for _, id := range ids {
		f, ok := csFanOf(id)
		if !ok {
			return nil, fmt.Errorf("unknown fan %q (fan1 = cpu, fan3 = case)", id)
		}
		// The probe lists the fans in the order of csFans.
		if i := slices.IndexFunc(csFans, func(x csFan) bool { return x.id == id }); i < len(rd.Fans) && rd.Fans[i].RPM == 0 {
			return nil, fmt.Errorf("%s reads no rpm: nothing connected, or the fan is stopped", id)
		} else if i < len(rd.Fans) && rd.Fans[i].RPM > csMaxRPM {
			return nil, fmt.Errorf("%s reads %d rpm: not a plausible speed, the interface does not answer like the fan controller", id, rd.Fans[i].RPM)
		}
		m.Outputs = append(m.Outputs, FanMapOutput{ID: id, Role: f.role, PWM: "duty", Tach: "rpm"})
	}
	if err := writeJSONAtomic(fanMapPath, m); err != nil {
		return nil, fmt.Errorf("cannot save %s: %w", fanMapPath, err)
	}
	return m, nil
}
