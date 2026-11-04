package fan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// IT8613E Super-I/O sessions and the watchdog of that chip.
//
// Facts (it87_wdt.c, GPL-2.0, and UGREEN's ug_it86x-sio.c, GPL-2.0-only, kernel-6.12 @c75c5ab; nothing copied): the
// watchdog is logical device 7; WDTCTRL 0x71, WDTCFG 0x72, value LSB 0x73, MSB 0x74 (it87_wdt.c:86-89). WDTCFG
// bit 7 selects seconds (clear = minutes), bit 6 resets through KRST#, bit 4 also through PWROK (:91-96,170-183). The
// driver starts and keeps alive with the same write: WDTCFG, then LSB, then MSB (here MSB first when it grows) (:168-187,213-216,273-278); a zero value
// stops it (:218-221). There is no enable bit, so a keepalive write of a non-zero value also starts a stopped timer.
// That is why this tool pets only a timer it has seen count down (Petter.tickITE) and never writes WDTCTRL.
const (
	sioLDNGPIO = 0x07 // the watchdog's logical device (it87_wdt.c:44-46)

	iteRegWdtCtrl = 0x71 // read only here
	iteRegWdtCfg  = 0x72
	iteRegWdtLo   = 0x73
	iteRegWdtHi   = 0x74

	iteWdtSeconds = 0x80 // WDTCFG: unit is seconds, clear = minutes

	iteWdtMaxValue = 65535 // 16-bit value register of the IT8613 (it87_wdt.c:56)
)

// The two samples that prove the timer counts lie on different daemon ticks at least this far apart: sioGap for a
// seconds timer, sioGapSlow for a minutes timer (WDTCFG bit 7 clear), which moves once a minute. Variables for tests.
var (
	sioGap     = 1500 * time.Millisecond
	sioGapSlow = 61 * time.Second
)

func sampleGap(cfg byte) time.Duration {
	if cfg&iteWdtSeconds == 0 {
		return sioGapSlow
	}
	return sioGap
}

var (
	errNotIT8613  = errors.New("Super-I/O chip is not an IT8613E")
	errNoSIOWrite = errors.New("register is not on the Super-I/O write list")
)

// sio is one open Super-I/O config session. rd never fails (a port error reads 0xff, as Detect always did); wr keeps
// the first error and refuses any register outside allow.
type sio struct {
	p     Ports
	id    uint16
	rev   byte
	ldn   byte
	allow func(reg byte) bool
	err   error
}

func (s *sio) rd(reg byte) byte {
	if err := s.p.Out(sioIndex, reg); err != nil {
		return 0xff
	}
	v, err := s.p.In(sioData)
	if err != nil {
		return 0xff
	}
	return v
}

func (s *sio) wr(reg, v byte) {
	if s.err != nil {
		return
	}
	if s.allow == nil || !s.allow(reg) {
		s.err = fmt.Errorf("%w: LDN 0x%02x reg 0x%02x", errNoSIOWrite, s.ldn, reg)
		return
	}
	if s.err = s.p.Out(sioIndex, reg); s.err == nil {
		s.err = s.p.Out(sioData, v)
	}
}

// sioOpts describe a session: the logical device to select, whether any chip id is accepted (Detect reports a
// foreign id itself) and the registers the session may write (none by default).
type sioOpts struct {
	ldn     byte
	anyChip bool
	allow   func(reg byte) bool
}

// runSIO unlocks the config ports, checks the chip id (0x8613 unless anyChip), selects the logical device, runs fn and
// always leaves config mode again.
func runSIO(p Ports, o sioOpts, fn func(s *sio) error) error {
	for _, b := range sioEnterKey {
		if err := p.Out(sioIndex, b); err != nil {
			return err
		}
	}
	defer func() {
		_ = p.Out(sioIndex, sioRegExit)
		_ = p.Out(sioData, 0x02)
	}()

	s := &sio{p: p, ldn: o.ldn, allow: o.allow}
	s.id = uint16(s.rd(sioRegChipID))<<8 | uint16(s.rd(sioRegChipID+1))
	s.rev = s.rd(sioRegRev)
	if s.id != 0x8613 && !o.anyChip {
		return fmt.Errorf("%w (id 0x%04x)", errNotIT8613, s.id)
	}
	_ = p.Out(sioIndex, sioRegLDN)
	_ = p.Out(sioData, o.ldn)
	if err := fn(s); err != nil {
		return err
	}
	return s.err
}

func (c *Chip) session(o sioOpts, fn func(s *sio) error) error { return runSIO(c.ports, o, fn) }

// ITEWdt is the watchdog as read: control, config and the 16-bit value.
type ITEWdt struct {
	ctrl, cfg byte
	val       uint16
}

// WatchdogRead reads the four registers, nothing else, no write. The value is read high byte, low byte, high byte
// again (a borrow between the two reads would be off by 256) and taken as is when it keeps moving.
func (c *Chip) WatchdogRead() (w ITEWdt, err error) {
	err = c.session(sioOpts{ldn: sioLDNGPIO}, func(s *sio) error {
		w.ctrl, w.cfg = s.rd(iteRegWdtCtrl), s.rd(iteRegWdtCfg)
		for range 3 {
			hi := s.rd(iteRegWdtHi)
			lo := s.rd(iteRegWdtLo)
			w.val = uint16(hi)<<8 | uint16(lo)
			if s.rd(iteRegWdtHi) == hi {
				break
			}
		}
		return nil
	})
	return
}

// wdtUnits converts the seconds to the unit cfg selects: seconds, or minutes rounded up.
func wdtUnits(cfg byte, seconds int) int {
	if cfg&iteWdtSeconds == 0 {
		seconds = (seconds + 59) / 60
	}
	return min(max(seconds, 1), iteWdtMaxValue)
}

// WatchdogPet reloads a timer that was seen counting: cfg as read (keeps the BIOS's reset bits), then the value. It
// refuses a stopped timer (cur.val == 0: this write would start it) and never writes 0x71.
func (c *Chip) WatchdogPet(cur ITEWdt, seconds int) error {
	if cur.val == 0 || seconds < 1 {
		return errNeverArm
	}
	return c.writeWdt(cur.cfg, uint16(wdtUnits(cur.cfg, seconds)), cur.val)
}

// writeWdt writes cfg and the value over the value cur. The two bytes cannot change together, so the order keeps the
// timer from reading 0 in between: MSB first when it grows (LSB may wrap to 0 while the new MSB is not yet there),
// LSB first otherwise.
func (c *Chip) writeWdt(cfg byte, n, cur uint16) error {
	return c.session(sioOpts{ldn: sioLDNGPIO, allow: func(r byte) bool { return r >= iteRegWdtCfg && r <= iteRegWdtHi }}, func(s *sio) error {
		s.wr(iteRegWdtCfg, cfg)
		if n>>8 > cur>>8 {
			s.wr(iteRegWdtHi, byte(n>>8))
			s.wr(iteRegWdtLo, byte(n))
		} else {
			s.wr(iteRegWdtLo, byte(n))
			s.wr(iteRegWdtHi, byte(n>>8))
		}
		return nil
	})
}

// wdtMarker is what a clean stop leaves behind when it disarms a timer the BIOS had armed: the config and the value to
// put back. The next daemon start that finds the timer at 0 restores it, so a restart does not end the protection.
type wdtMarker struct {
	Cfg   byte   `json:"cfg"`
	Value uint16 `json:"value"`
}

// iteWdtMarkerPath is on tmpfs: a reboot clears it, and the BIOS arms the timer again then.
var iteWdtMarkerPath = envOr("UGREEN_FAN_WATCHDOG_ARMED", "/var/run/ugreen-nas-control.watchdog-armed")

func readWdtMarker() *wdtMarker {
	b, err := os.ReadFile(iteWdtMarkerPath)
	if err != nil {
		return nil
	}
	var m wdtMarker
	if json.Unmarshal(b, &m) != nil || m.Value == 0 {
		return nil
	}
	return &m
}

func removeWdtMarker() { _ = os.Remove(iteWdtMarkerPath) }

// WatchdogRestore puts a timer back that this tool stopped on a clean stop (marker): the only place besides the feed
// that writes a non-zero value, and only for a timer that was seen counting before it was stopped.
func (c *Chip) WatchdogRestore(m wdtMarker) error {
	return c.writeWdt(m.Cfg, m.Value, 0)
}

// DisarmWatchdog stops the timer: value 0, config untouched. The only time this tool writes a zero.
func (c *Chip) DisarmWatchdog() error {
	return c.session(sioOpts{ldn: sioLDNGPIO, allow: func(r byte) bool { return r == iteRegWdtLo || r == iteRegWdtHi }}, func(s *sio) error {
		s.wr(iteRegWdtLo, 0)
		s.wr(iteRegWdtHi, 0)
		return nil
	})
}
