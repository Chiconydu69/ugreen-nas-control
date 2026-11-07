package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Poll is the vendor's period (ug_gpio_btn.c:39,163-171@c75c5ab).
const Poll = 50 * time.Millisecond

// Pad is one button line: bit Bit of the 32-bit pad register at physical address Addr. The vendor reads some
// registers at an address that is not 4-byte aligned (readl at 0xFED81526, bit 0); we read the aligned word and
// shift the byte offset into the bit number, which selects the same bit on a little-endian CPU.
type Pad struct {
	Addr       uint64
	Bit        uint
	ActiveHigh bool
}

func (p Pad) word() uint64 { return p.Addr &^ 3 }
func (p Pad) bit() uint    { return p.Bit + uint(p.Addr&3)*8 }

// Pressed reports whether the button is held, from the register value w.
func (p Pad) Pressed(w uint32) bool {
	high := (w>>p.bit())&1 == 1
	return high == p.ActiveHigh
}

// ButtonSpec is how one model exposes its power and reset button.
type ButtonSpec struct {
	Name  string // vendor's group name, for the docs
	Match matcher
	Power *Pad // nil: not readable by address
	Reset *Pad
	// PowerPressPolls is the number of polls the power button must stay down before the vendor reports a press: 1
	// (the MCU forwards the press) and 100 (5 s) on the DXP2800 GT (ug_gpio_btn.c:88-96@c75c5ab).
	PowerPressPolls int
	Cite            string
}

// Hold lengths, counted in polls of Poll (ug_gpio_btn.c:73-82,99-115@c75c5ab). The vendor's reset comment says a
// five-second hold, but its constant is 260 polls, which is 13 s; we follow the constant.
const (
	resetPressAfter   = 260  // polls of continuous hold before a reset press is reported
	powerReleaseAfter = 100  // a power release is reported only if the button was held at least this long (5 s)
	countCeiling      = 1200 // the hold counter stops here (one minute)
)

func pad(addr uint64, high bool) *Pad { return &Pad{Addr: addr, Bit: 1, ActiveHigh: high} }

// ButtonSpecs is in the vendor's if/else order (ug_gpio_btn.c:248-471@c75c5ab). Power is active high and reset active
// low everywhere except the DXP2800 GT, where both are active low on bit 16.
var ButtonSpecs = []ButtonSpec{
	{Name: "DXP2800, DXP2800S, DXP4800, DXP4800S", Match: matcher{exact: []string{"DXP2800", "DXP2800S", "DXP4800", "DXP4800S"}},
		Power: pad(0xFD6A0980, true), Reset: pad(0xFD6D0B50, false), PowerPressPolls: 1, Cite: "ug_gpio_btn.c:248-272@c75c5ab"},
	{Name: "DXP4800 Plus, DXP4800 Pro", Match: matcher{prefix: []string{"DXP4800 Plus", "DXP4800 Pro"}},
		Power: pad(0xFD6A0980, true), Reset: pad(0xFD6D0A10, false), PowerPressPolls: 1, Cite: "ug_gpio_btn.c:274-299@c75c5ab"},
	{Name: "DXP6800*, DXP8800*, FORT 6", Match: matcher{prefix: []string{"DXP6800", "DXP8800", "FORT 6"}},
		Power: pad(0xFD6A0B40, true), Reset: pad(0xFD6D0A10, false), PowerPressPolls: 1, Cite: "ug_gpio_btn.c:301-325@c75c5ab"},
	{Name: "DXP480T Plus", Match: matcher{prefix: []string{"DXP480T Plus"}},
		Power: pad(0xFD6E0810, true), Reset: pad(0xFD6E0770, false), PowerPressPolls: 1, Cite: "ug_gpio_btn.c:326-350@c75c5ab"},
	// The vendor comment says the iDX power key sits on ACPI and cannot be read by address (:353), yet it lists an
	// address; both are kept, the address is unconfirmed.
	{Name: "iDX6011*, iDX6012*", Match: matcher{prefix: []string{"iDX6011", "iDX6012"}},
		Power: pad(0xE0D20860, true), Reset: pad(0xE0D508A0, false), PowerPressPolls: 1, Cite: "ug_gpio_btn.c:351-375@c75c5ab"},
	{Name: "DH2600", Match: matcher{prefix: []string{"DH2600"}},
		Power: pad(0xFD6D0B20, true), Reset: pad(0xFD6E0800, false), PowerPressPolls: 1, Cite: "ug_gpio_btn.c:376-399@c75c5ab"},
	{Name: "DX4600*, DX4700*", Match: matcher{prefix: []string{"DX4600", "DX4700"}},
		Power: pad(0xFD6D0690, true), Reset: pad(0xFD6D0890, false), PowerPressPolls: 1, Cite: "ug_gpio_btn.c:400-423@c75c5ab"},
	{Name: "DXP4800 GT", Match: matcher{exact: []string{"DXP4800 GT"}},
		Power: &Pad{Addr: 0xFED81526, Bit: 0, ActiveHigh: true}, Reset: &Pad{Addr: 0xFED81522, Bit: 0, ActiveHigh: false},
		PowerPressPolls: 1, Cite: "ug_gpio_btn.c:424-447@c75c5ab"},
	{Name: "DXP2800 GT", Match: matcher{exact: []string{"DXP2800 GT"}},
		Power: &Pad{Addr: 0xFED81510, Bit: 16, ActiveHigh: false}, Reset: &Pad{Addr: 0xFED8150C, Bit: 16, ActiveHigh: false},
		PowerPressPolls: 100, Cite: "ug_gpio_btn.c:448-471@c75c5ab"},
}

// ButtonsFor is the entry for a DMI product name, or nil.
func ButtonsFor(product string) *ButtonSpec {
	for i := range ButtonSpecs {
		if ButtonSpecs[i].Match.ok(product) {
			return &ButtonSpecs[i]
		}
	}
	return nil
}

// State is the held state of both buttons. An Unreadable button has no valid reading and its held flag is false.
type State struct{ Power, Reset, PowerUnreadable, ResetUnreadable bool }

// Unreadable reports whether either button had no valid reading.
func (st State) Unreadable() bool { return st.PowerUnreadable || st.ResetUnreadable }

// ErrUnreadable is returned by Watch when a pad register reads as all ones.
var ErrUnreadable = errors.New("the buttons are " + unreadableNote)

// padMasked is what a read of memory the bus does not answer returns (master abort). A real pad register has
// reserved bits that read 0, so it is never the value of a working pad.
const padMasked = 0xFFFFFFFF

// ReadState reads both pads. The DXP2800 GT driver also clears bit 23 of each register (pin to input); we never
// write, so on that model the pins are read as the firmware left them.
func (s *ButtonSpec) ReadState(m MemReader) (State, error) {
	var st State
	for _, b := range []struct {
		p     *Pad
		out   *bool
		unrdb *bool
	}{{s.Power, &st.Power, &st.PowerUnreadable}, {s.Reset, &st.Reset, &st.ResetUnreadable}} {
		if b.p == nil {
			continue
		}
		w, err := m.Read32(b.p.word())
		if err != nil {
			return st, err
		}
		if w == padMasked {
			*b.unrdb = true
			continue
		}
		*b.out = b.p.Pressed(w)
	}
	return st, nil
}

// PadWord is one pad register as read: the aligned physical address and its raw 32-bit value.
type PadWord struct {
	Name  string // "power" or "reset"
	Addr  uint64
	Value uint32
	Err   error
}

// ErrNoPad is the Err of a pad the model has no address for.
var ErrNoPad = errors.New("not readable by address")

// ReadWords reads the raw word of each pad, for diagnostics. Read only: MemReader has no write. A value of 0xFFFFFFFF
// means the box does not answer there (the buttons are then unreadable).
func (s *ButtonSpec) ReadWords(m MemReader) []PadWord {
	var out []PadWord
	for _, b := range []struct {
		name string
		p    *Pad
	}{{"power", s.Power}, {"reset", s.Reset}} {
		if b.p == nil {
			out = append(out, PadWord{Name: b.name, Err: ErrNoPad})
			continue
		}
		w := PadWord{Name: b.name, Addr: b.p.word()}
		w.Value, w.Err = m.Read32(w.Addr)
		out = append(out, w)
	}
	return out
}

// Event names a Tracker emits.
const (
	EvDown    = "down"    // pad went active
	EvPress   = "press"   // the vendor driver reports the key press (power KEY_F20, reset KEY_F24)
	EvRelease = "release" // the vendor driver reports the key release
	EvUp      = "up"      // pad went inactive
)

// Event is one transition; Polls is how many 50 ms polls the pad had been active (for up/release).
type Event struct {
	Name  string
	Polls int
}

// Tracker turns a stream of pad readings (one per Poll) into events for one button. It mirrors the vendor driver's
// observable rules, not its code: the press is reported once the hold reaches a threshold; whether the later release
// is reported depends on the button. Power: the release is reported only after a hold of powerReleaseAfter polls or
// more, so a short press yields down and up but no release. Reset: the release is reported whenever the press was
// reported (the vendor needs a hold above the threshold and misses a release at exactly 260 polls; we do not).
type Tracker struct {
	isReset   bool
	threshold int  // polls of hold that make a press
	held      int  // length of the current hold, capped at countCeiling
	pressed   bool // the pad is currently active
	announced bool // the press event of this hold has been emitted
}

func NewPowerTracker(pressPolls int) *Tracker { return &Tracker{threshold: pressPolls} }
func NewResetTracker() *Tracker               { return &Tracker{isReset: true, threshold: resetPressAfter} }

// releaseWanted: should this hold end with a release event?
func (t *Tracker) releaseWanted() bool {
	if t.isReset {
		return t.announced
	}
	return t.announced && t.held >= powerReleaseAfter
}

// Poll feeds one reading and returns the events it causes.
func (t *Tracker) Poll(active bool) (ev []Event) {
	switch {
	case active:
		if !t.pressed {
			t.pressed = true
			ev = append(ev, Event{Name: EvDown})
		}
		if t.held < countCeiling {
			t.held++
			if t.held == t.threshold {
				t.announced = true
				ev = append(ev, Event{Name: EvPress, Polls: t.held})
			}
		}
	case t.pressed:
		ev = append(ev, Event{Name: EvUp, Polls: t.held})
		if t.releaseWanted() {
			ev = append(ev, Event{Name: EvRelease, Polls: t.held})
		}
		*t = Tracker{isReset: t.isReset, threshold: t.threshold}
	}
	return ev
}

// Watch polls until ctx ends. tick delivers the polls (a 50 ms ticker in production); now stamps the lines.
func (s *ButtonSpec) Watch(ctx context.Context, m MemReader, out io.Writer, tick <-chan time.Time, now func() time.Time) error {
	st, err := s.ReadState(m)
	if err != nil {
		return err
	}
	if st.Unreadable() {
		return ErrUnreadable
	}
	say(out, "start: %s", describe(st))
	pw, rs := NewPowerTracker(s.PowerPressPolls), NewResetTracker()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
		}
		st, err := s.ReadState(m)
		if err != nil {
			return err
		}
		if st.Unreadable() {
			return ErrUnreadable
		}
		for _, b := range []struct {
			name   string
			t      *Tracker
			active bool
		}{{"power", pw, st.Power}, {"reset", rs, st.Reset}} {
			for _, e := range b.t.Poll(b.active) {
				line := fmt.Sprintf("%s %s %s", now().Format("15:04:05.000"), b.name, e.Name)
				if e.Polls > 0 {
					line += fmt.Sprintf(" (held %.2f s)", float64(e.Polls)*Poll.Seconds())
				}
				say(out, "%s", line)
			}
		}
	}
}

func describe(st State) string {
	word := func(b, unreadable bool) string {
		if unreadable {
			return "unreadable"
		}
		if b {
			return "held"
		}
		return "released"
	}
	return fmt.Sprintf("power %s, reset %s", word(st.Power, st.PowerUnreadable), word(st.Reset, st.ResetUnreadable))
}
