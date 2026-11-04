package fan

import (
	"errors"
	"fmt"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

type Ports = hw.Ports

// Super-I/O config ports of ITE chips and the key that opens them.
const (
	sioIndex = 0x2e
	sioData  = 0x2f

	sioRegLDN     = 0x07
	sioRegChipID  = 0x20
	sioRegRev     = 0x22
	sioRegActive  = 0x30
	sioRegBaseHi  = 0x60
	sioRegBaseLo  = 0x61
	sioRegExit    = 0x02
	sioLDNMonitor = 0x04
)

var sioEnterKey = []byte{0x87, 0x01, 0x55, 0x55}

// Environment controller (hardware monitor) registers.
const (
	hwmVendorID  = 0x58
	hwmVendorITE = 0x90

	// In manual mode the duty cycle register sets the speed; with this bit set in
	// the control register the chip's own automatic curve does.
	ctrlAuto = 0x80
)

// IT8613E channels as wired on the DXP6800 Pro, where each PWM output was
// raised on its own to see which fan follows. Headers 1 and 5 are unused. Header 4's control register is
// unconfirmed: we use 0x7f, UGREEN's driver names 0x1e (ug_it86x-cpufan.c:82-83@c75c5ab; its tach 0x80/0x81 and duty
// 0x7b agree with ours). Not changed until read on a device.
var it8613Channels = []Channel{
	{ID: "fan2", TachLow: 0x0e, TachHigh: 0x19, Duty: 0x6b, Ctrl: 0x16},
	{ID: "fan3", TachLow: 0x0f, TachHigh: 0x1a, Duty: 0x73, Ctrl: 0x17},
	{ID: "fan4", TachLow: 0x80, TachHigh: 0x81, Duty: 0x7b, Ctrl: 0x7f},
}

// Temperature inputs of the chip; 0x80 (-128) means no sensor.
var it8613Temps = []byte{0x29, 0x2a, 0x2b}

type Chip struct {
	ports    Ports
	ID       uint16
	Revision byte
	Base     uint16
	Channels []Channel
	// floor is the lowest duty SetPWM writes (0 = none): models whose case fan stalls low never get less.
	floor byte
}

// Detect finds an IT8613E through the Super-I/O config ports at 0x2e and
// reads where its hardware monitor sits. The config mode is left again.
func Detect(p Ports) (*Chip, error) {
	var id uint16
	var rev, active byte
	var base uint16
	err := runSIO(p, sioOpts{ldn: sioLDNMonitor, anyChip: true}, func(s *sio) error {
		id, rev = s.id, s.rev
		active = s.rd(sioRegActive)
		base = uint16(s.rd(sioRegBaseHi))<<8 | uint16(s.rd(sioRegBaseLo))
		return nil
	})
	if err != nil {
		return nil, err
	}

	if id != 0x8613 {
		return nil, fmt.Errorf("no supported fan controller found (Super-I/O chip id 0x%04x)", id)
	}
	if active&0x01 == 0 || base == 0 || base == 0xffff {
		return nil, errors.New("IT8613E hardware monitor is not enabled by the BIOS")
	}

	c := &Chip{ports: p, ID: id, Revision: rev, Base: base, Channels: it8613Channels}

	if v, err := c.Read(hwmVendorID); err != nil || v != hwmVendorITE {
		return nil, fmt.Errorf("IT8613E hardware monitor at 0x%04x does not answer", base)
	}

	return c, nil
}

func (c *Chip) Read(reg byte) (byte, error) {
	if err := c.ports.Out(c.Base+5, reg); err != nil {
		return 0, err
	}
	return c.ports.In(c.Base + 6)
}

func (c *Chip) Write(reg, value byte) error {
	if err := c.ports.Out(c.Base+5, reg); err != nil {
		return err
	}
	return c.ports.Out(c.Base+6, value)
}

func (c *Chip) Channel(id string) (Channel, bool) {
	for _, ch := range c.Channels {
		if ch.ID == id {
			return ch, true
		}
	}
	return Channel{}, false
}

func (c *Chip) RPM(ch Channel) (int, error) {
	lo, err := c.Read(ch.TachLow)
	if err != nil {
		return 0, err
	}
	hi, err := c.Read(ch.TachHigh)
	if err != nil {
		return 0, err
	}
	return rpmFromCount(int(hi)<<8 | int(lo)), nil
}

// rpmFromCount: 0, 0xffff and 0x0fff are "no reading" (ug_it86x-sio.c:226-233@c75c5ab treats all three as 0 rpm).
func rpmFromCount(count int) int {
	if count == 0 || count == 0xffff || count == 0x0fff {
		return 0
	}
	return 1350000 / (2 * count)
}

func (c *Chip) PWM(ch Channel) (duty byte, auto bool, err error) {
	if duty, err = c.Read(ch.Duty); err != nil {
		return 0, false, err
	}
	ctrl, err := c.Read(ch.Ctrl)
	if err != nil {
		return 0, false, err
	}
	return duty, ctrl&ctrlAuto != 0, nil
}

func (c *Chip) SetPWM(ch Channel, duty byte) error {
	if c.floor > 0 && duty < c.floor {
		duty = c.floor
	}
	ctrl, err := c.Read(ch.Ctrl)
	if err != nil {
		return err
	}
	if ctrl&ctrlAuto != 0 {
		if err := c.Write(ch.Ctrl, ctrl&^ctrlAuto); err != nil {
			return err
		}
	}
	return c.Write(ch.Duty, duty)
}

func (c *Chip) Temps() []*int {
	out := make([]*int, len(it8613Temps))
	for i, reg := range it8613Temps {
		v, err := c.Read(reg)
		if err != nil || v == 0x80 {
			continue
		}
		t := int(int8(v))
		out[i] = &t
	}
	return out
}

func (c *Chip) Snapshot() (Snapshot, error) {
	s := Snapshot{}
	for _, ch := range c.Channels {
		ctrl, err := c.Read(ch.Ctrl)
		if err != nil {
			return nil, err
		}
		duty, err := c.Read(ch.Duty)
		if err != nil {
			return nil, err
		}
		s[ch.ID] = [2]byte{ctrl, duty}
	}
	return s, nil
}

func (c *Chip) Restore(s Snapshot) error {
	for _, ch := range c.Channels {
		v, ok := s[ch.ID]
		if !ok {
			continue
		}
		if err := c.Write(ch.Duty, v[1]); err != nil {
			return err
		}
		if err := c.Write(ch.Ctrl, v[0]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Chip) Name() string { return fmt.Sprintf("IT%04X", c.ID) }

func (c *Chip) Describe() string { return fmt.Sprintf("IT8613E at 0x%04x", c.Base) }

func (c *Chip) Details() map[string]any {
	return map[string]any{"revision": c.Revision, "base": fmt.Sprintf("0x%04x", c.Base)}
}

func (c *Chip) Outputs() []Channel { return c.Channels }

func (c *Chip) MaxDuty() byte { return 255 }

func (c *Chip) AutoDutyKnown() bool { return true }

func percentToDuty(pct int, top byte) byte {
	if pct <= 0 {
		return 0
	}
	if pct >= 100 {
		return top
	}
	return byte((pct*int(top) + 50) / 100)
}

func dutyToPercent(duty, top byte) int {
	return (int(duty)*100 + int(top)/2) / int(top)
}
