package panel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// Beeper is one way to sound the speaker. Off must be safe to call at any time, also when On failed half-way.
type Beeper interface {
	On(freq int) error
	Off() error
}

// Beep bounds. The vendor timer command takes 20..99999 ms (ug_sataio_beeper.c:23-25@c75c5ab); we stop at 2 s.
const (
	MinMs, MaxMs, DefaultMs = 20, 2000, 200 // 200 ms is the vendor's "one" beep
	MinHz, MaxHz            = 100, 8000
)

// BeepKind is the hardware path of a model's beeper.
type BeepKind int

const (
	PCSpeaker BeepKind = iota // PIT channel 2 and port 0x61
	ECByte                    // one byte of the IT5571 embedded controller
	PadReg                    // bit 0 of a pad register in MMIO
)

func (k BeepKind) String() string {
	return [...]string{"PC speaker", "EC register", "pad register"}[k]
}

// BeeperSpec is the beeper of one model group.
type BeeperSpec struct {
	Name  string
	Match matcher
	Kind  BeepKind
	ECReg byte // ECByte
	// ECSoftID: the chip id of the controller is only noted, not required (the plain iDX6011/6012; the fan code
	// treats them alike). Every other EC model must answer as an IT5571.
	ECSoftID bool
	PadAddr  uint64 // PadReg
	Cite     string
}

// BeeperSpecs: the generic PC speaker for the models of the SATA/beeper driver (ug_sataio_beeper.c:535-583@c75c5ab)
// and the DX4700 (dx4700/ug_sata_beep-dx4700.c:164-189@c75c5ab). Models the vendor files do not name (DXP4800 GT,
// DXP2800 GT, DX4600) get no entry: no beeper path is known for them, so we refuse.
// The iDX6011 Pro uses EC 0x9E, never 0xA5: on that model 0xA5 is the backlight (ug_sataio_beeper.c:33-35@c75c5ab).
var BeeperSpecs = []BeeperSpec{
	{Name: "DXP2800, DXP2800S, DXP4800, DXP4800S", Match: matcher{exact: []string{"DXP2800", "DXP2800S", "DXP4800", "DXP4800S"}},
		Kind: PCSpeaker, Cite: "ug_sataio_beeper.c:63-79,305-311,535-545@c75c5ab"},
	{Name: "DXP4800 Plus, DXP4800 Pro", Match: matcher{prefix: []string{"DXP4800 Plus", "DXP4800 Pro"}},
		Kind: PCSpeaker, Cite: "ug_sataio_beeper.c:63-79,305-311,547-555@c75c5ab"},
	{Name: "DXP6800*, DXP8800*, FORT 6", Match: matcher{prefix: []string{"DXP6800", "DXP8800", "FORT 6"}},
		Kind: PCSpeaker, Cite: "ug_sataio_beeper.c:63-79,305-311,556-570@c75c5ab"},
	{Name: "DX4700", Match: matcher{exact: []string{"DX4700"}},
		Kind: PCSpeaker, Cite: "dx4700/ug_sata_beep-dx4700.c:164-189@c75c5ab"},
	{Name: "iDX6011, iDX6012", Match: matcher{exact: []string{"iDX6011", "iDX6012"}},
		Kind: ECByte, ECReg: 0xA5, ECSoftID: true, Cite: "ug_sataio_beeper.c:26-35,126-171,299-304@c75c5ab"},
	{Name: "iDX6011 Pro", Match: matcher{exact: []string{"iDX6011 Pro"}},
		Kind: ECByte, ECReg: 0x9E, Cite: "ug_sataio_beeper.c:33,301-303@c75c5ab"},
	{Name: "DH2600", Match: matcher{prefix: []string{"DH2600"}},
		Kind: PadReg, PadAddr: 0xFD6E08B0, Cite: "dh2600/ug_sata_beep-dh2600.c:42,164-182@c75c5ab"},
	{Name: "DXP480T Plus", Match: matcher{prefix: []string{"DXP480T Plus"}},
		Kind: PadReg, PadAddr: 0xFD6E07E0, Cite: "dxp480t/ug-beeper-dxp480t.c:41-49,118-134@c75c5ab"},
}

// BeeperFor is the entry for a DMI product name, or nil.
func BeeperFor(product string) *BeeperSpec {
	for i := range BeeperSpecs {
		if BeeperSpecs[i].Match.ok(product) {
			return &BeeperSpecs[i]
		}
	}
	return nil
}

// Backends a Beep call may open; Env supplies the real or fake ones.
type openers struct {
	ports func() (hw.Ports, error)
	mem   func(write bool) (Mem, error)
	// lock runs fn under the flock shared with the fan daemon; nil means hw.WithLock.
	lock func(fn func() error) error
	// acpiDir is the ACPI device directory the EC driver check reads; empty means the real one.
	acpiDir string
	// conflicts lists the rival fan writers; nil means the real detection.
	conflicts func() []conflict.Conflict
}

func (o openers) withLock(fn func() error) error {
	if o.lock != nil {
		return o.lock(fn)
	}
	return hw.WithLock(fn)
}

// open returns the Beeper and a close func.
func (s *BeeperSpec) open(o openers) (Beeper, func(), error) {
	switch s.Kind {
	case PCSpeaker:
		// Ports 0x42, 0x43 and 0x61 belong to no other tool of this project, so no lock is taken.
		p, err := o.ports()
		if err != nil {
			return nil, nil, err
		}
		return &speaker{p: p}, func() { _ = p.Close() }, nil
	case ECByte:
		if err := o.guardEC(); err != nil {
			return nil, nil, err
		}
		p, err := o.ports()
		if err != nil {
			return nil, nil, err
		}
		if err := o.checkChip(p, s.ECSoftID); err != nil {
			_ = p.Close()
			return nil, nil, err
		}
		return &ecBeeper{p: p, reg: s.ECReg, wait: ecWait, lock: o.withLock}, func() { _ = p.Close() }, nil
	default:
		// The pad words are written by this tool alone (the fan code touches other MMIO words), so no lock.
		m, err := o.mem(true)
		if err != nil {
			return nil, nil, err
		}
		return &padBeeper{m: m, addr: s.PadAddr}, func() { _ = m.Close() }, nil
	}
}

// The same read-only refusals the fan code applies before it talks to the controller (fan/ec.go detectEC): the
// kernel's ACPI EC driver must not be bound, the chip behind ports 0x4E/0x4F must be an IT5571, and no rival fan
// writer may be running.
const (
	ecSioIndex = 0x4E
	ecSioData  = 0x4F
	ecChipID   = 0x5571
)

// DefaultACPIDir lists the ACPI devices; a bound PNP0C09 entry is the kernel's EC driver.
const DefaultACPIDir = "/sys/bus/acpi/devices"

func (o openers) guardEC() error {
	list := conflict.Default().Detect
	if o.conflicts != nil {
		list = o.conflicts
	}
	if rivals := conflict.Blocking(list(), conflict.Fan); len(rivals) > 0 {
		return fmt.Errorf("refusing to use the embedded controller while %s is running", rivals[0].Name)
	}
	dir := o.acpiDir
	if dir == "" {
		dir = DefaultACPIDir
	}
	bound, _ := filepath.Glob(filepath.Join(dir, "PNP0C09:*", "driver"))
	for _, d := range bound {
		if _, err := os.Lstat(d); err == nil {
			return fmt.Errorf("the kernel's ACPI embedded controller driver is bound (%s); refusing to share the controller with it", filepath.Dir(d))
		}
	}
	return nil
}

// checkChip reads the chip id word (registers 0x20, 0x21) under the shared lock, because the index port is shared.
func (o openers) checkChip(p hw.Ports, soft bool) error {
	var id uint16
	err := o.withLock(func() error {
		for _, reg := range []byte{0x20, 0x21} {
			if err := p.Out(ecSioIndex, reg); err != nil {
				return err
			}
			v, err := p.In(ecSioData)
			if err != nil {
				return err
			}
			id = id<<8 | uint16(v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if id != ecChipID && !soft {
		return fmt.Errorf("no supported embedded controller found (chip id 0x%04x, expected 0x%04x)", id, ecChipID)
	}
	return nil
}

// PC speaker: timer command 0xB6 to port 0x43, divisor low then high byte to port 0x42, on = set bits 0-1 of port
// 0x61, off = clear them (ug_sataio_beeper.c:63-79@c75c5ab). The vendor always uses divisor 0x0944 (2372, about
// 503 Hz); a chosen frequency is our own use of the same PIT (1.193182 MHz input clock).
const (
	pitCmd      = 0x43
	pitCh2      = 0x42
	pitGate     = 0x61
	pitSquare   = 0xB6
	pitClock    = 1193182
	pitVendorDv = 0x0944
)

type speaker struct{ p hw.Ports }

func (s *speaker) On(freq int) error {
	div := pitVendorDv
	if freq > 0 {
		div = (pitClock + freq/2) / freq
	}
	for _, w := range [][2]int{{pitCmd, pitSquare}, {pitCh2, div & 0xFF}, {pitCh2, div >> 8}} {
		if err := s.p.Out(uint16(w[0]), byte(w[1])); err != nil {
			return err
		}
	}
	v, err := s.p.In(pitGate)
	if err != nil {
		return err
	}
	return s.p.Out(pitGate, v|3)
}

func (s *speaker) Off() error {
	v, err := s.p.In(pitGate)
	if err != nil {
		return err
	}
	return s.p.Out(pitGate, v&^3)
}

// EC byte: ACPI EC ports data 0x62 and command 0x66, write = 0x81 reg value, read = 0x80 reg
// (ug_sataio_beeper.c:26-29,126-171@c75c5ab). Behaviour taken from the vendor driver: after a write the register is
// read back and the write is repeated until the read-back matches, and a read-back of 2 after writing 1 counts as
// success (the controller reports the active state that way on the iDX6011). We bound the repeats and wait for
// IBF/OBF as the fan code does.
const (
	ecData     = 0x62
	ecCmd      = 0x66
	ecOBF      = 0x01
	ecIBF      = 0x02
	ecCmdRead  = 0x80
	ecCmdWrite = 0x81
	ecTries    = 5
)

var ecWait = 100 * time.Millisecond

// ecBeeper writes exactly one register, fixed at construction, with 0 or 1 only. Ports 0x62/0x66 are also used by
// the fan daemon, so every write-and-read-back exchange runs under the shared flock; the lock is released between
// exchanges, never held while the beep sounds.
type ecBeeper struct {
	p    hw.Ports
	reg  byte
	wait time.Duration
	lock func(fn func() error) error // nil: hw.WithLock
}

var errECTimeout = errors.New("embedded controller timeout")

func (e *ecBeeper) locked(fn func() error) error {
	if e.lock != nil {
		return e.lock(fn)
	}
	return hw.WithLock(fn)
}

func (e *ecBeeper) waitFlag(flag byte, set bool) error {
	deadline := time.Now().Add(e.wait)
	for {
		st, err := e.p.In(ecCmd)
		if err != nil {
			return err
		}
		if (st&flag != 0) == set {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w (status 0x%02x)", errECTimeout, st)
		}
		time.Sleep(100 * time.Microsecond)
	}
}

func (e *ecBeeper) send(port uint16, v byte) error {
	if err := e.waitFlag(ecIBF, false); err != nil {
		return err
	}
	return e.p.Out(port, v)
}

// readbackOK: the register holds what was asked, or, for "on", the controller's alternative active value 2.
func readbackOK(want, got byte) bool {
	return got == want || (want == 1 && got == 2)
}

// set drives the register to v. A failed exchange or a wrong read-back is retried up to ecTries times with a
// growing pause; the last problem is returned if none succeeds.
func (e *ecBeeper) set(v byte) error {
	var problem error
	for n := 1; n <= ecTries; n++ {
		var got byte
		err := e.locked(func() (err error) { got, err = e.exchange(v); return })
		if err == nil && readbackOK(v, got) {
			return nil
		}
		problem = err
		if problem == nil {
			problem = fmt.Errorf("register 0x%02X reads back 0x%02X after writing %d", e.reg, got, v)
		}
		time.Sleep(time.Duration(n) * time.Millisecond)
	}
	return problem
}

// exchange is one complete write-then-read transaction; the caller holds the lock.
func (e *ecBeeper) exchange(v byte) (byte, error) {
	if st, err := e.p.In(ecCmd); err == nil && st&ecOBF != 0 {
		_, _ = e.p.In(ecData) // drop a stale byte left by an aborted read
	}
	steps := [...]struct {
		port uint16
		v    byte
	}{{ecCmd, ecCmdWrite}, {ecData, e.reg}, {ecData, v}, {ecCmd, ecCmdRead}, {ecData, e.reg}}
	for _, st := range steps {
		if err := e.send(st.port, st.v); err != nil {
			return 0, err
		}
	}
	if err := e.waitFlag(ecOBF, true); err != nil {
		return 0, err
	}
	return e.p.In(ecData)
}

func (e *ecBeeper) On(int) error { return e.set(1) }
func (e *ecBeeper) Off() error   { return e.set(0) }

// Pad register (dh2600/ug_sata_beep-dh2600.c:164-182, dxp480t/ug-beeper-dxp480t.c:118-134@c75c5ab): bit 0 is the
// buzzer level, bits 10..8 are a three-bit field that must hold 0b010 while the buzzer is used. A beep changes only
// those four bits; the other 28 are written back as read.
const (
	padLevelBit  = uint32(1)
	padFieldMask = uint32(0b111) << 8
	padFieldVal  = uint32(0b010) << 8
)

type padBeeper struct {
	m    Mem
	addr uint64
}

func (b *padBeeper) put(on bool) error {
	word, err := b.m.Read32(b.addr)
	if err != nil {
		return err
	}
	word = word&^(padFieldMask|padLevelBit) | padFieldVal
	if on {
		word |= padLevelBit
	}
	return b.m.Write32(b.addr, word)
}

func (b *padBeeper) On(int) error { return b.put(true) }
func (b *padBeeper) Off() error   { return b.put(false) }

// Sound turns the beeper on for ms milliseconds. It always tries Off afterwards, also on error, panic and when ctx
// ends early (SIGINT/SIGTERM in the CLI). wait must return early when ctx is done.
func Sound(ctx context.Context, b Beeper, freq, ms int, wait func(context.Context, time.Duration)) (err error) {
	defer func() {
		if offErr := b.Off(); offErr != nil {
			err = errors.Join(err, fmt.Errorf("switching the beeper off failed: %w", offErr))
		}
	}()
	if err := b.On(freq); err != nil {
		return err
	}
	wait(ctx, time.Duration(ms)*time.Millisecond)
	return nil
}
