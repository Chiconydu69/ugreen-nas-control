package panel

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestButtonsFor(t *testing.T) {
	for _, c := range []struct {
		model        string
		power, reset uint64 // 0 = no entry
	}{
		{"DXP4800", 0xFD6A0980, 0xFD6D0B50},
		{"DXP2800S", 0xFD6A0980, 0xFD6D0B50},
		{"DXP4800 Plus", 0xFD6A0980, 0xFD6D0A10},
		{"DXP4800 Pro", 0xFD6A0980, 0xFD6D0A10},
		{"DXP6800 Pro", 0xFD6A0B40, 0xFD6D0A10},
		{"DXP8800 Plus", 0xFD6A0B40, 0xFD6D0A10},
		{"FORT 6", 0xFD6A0B40, 0xFD6D0A10},
		{"DXP480T Plus", 0xFD6E0810, 0xFD6E0770},
		{"iDX6011 Pro", 0xE0D20860, 0xE0D508A0},
		{"iDX6012", 0xE0D20860, 0xE0D508A0},
		{"DH2600", 0xFD6D0B20, 0xFD6E0800},
		{"DX4700", 0xFD6D0690, 0xFD6D0890},
		{"DX4600 Pro", 0xFD6D0690, 0xFD6D0890},
		{"DXP4800 GT", 0xFED81524, 0xFED81520},
		{"DXP2800 GT", 0xFED81510, 0xFED8150C},
		{"DXP2800 GTX", 0, 0}, // exact match only
		{"DXP6800 Ultra", 0, 0},
		{"DXP8800 Ultra", 0, 0},
		{"DXP4800+", 0, 0},
		{"", 0, 0},
		{"Standard PC", 0, 0},
	} {
		s := ButtonsFor(c.model)
		if c.power == 0 {
			if s != nil {
				t.Errorf("%q: unexpected entry %s", c.model, s.Name)
			}
			continue
		}
		if s == nil {
			t.Errorf("%q: no entry", c.model)
			continue
		}
		if s.Power.word() != c.power || s.Reset.word() != c.reset {
			t.Errorf("%q: power 0x%X reset 0x%X", c.model, s.Power.word(), s.Reset.word())
		}
	}
}

func TestPadBitAndLevel(t *testing.T) {
	gt := ButtonsFor("DXP4800 GT")
	// The vendor reads 0xFED81526 bit 0 as a 32-bit load: bit 16 of the aligned word.
	if gt.Power.bit() != 16 || gt.Reset.bit() != 16 {
		t.Fatalf("bits %d %d", gt.Power.bit(), gt.Reset.bit())
	}
	if !gt.Power.Pressed(1<<16) || gt.Power.Pressed(0) {
		t.Error("GT power is active high")
	}
	if !gt.Reset.Pressed(0) || gt.Reset.Pressed(1<<16) {
		t.Error("GT reset is active low")
	}
	gt2 := ButtonsFor("DXP2800 GT")
	if gt2.PowerPressPolls != 100 || !gt2.Power.Pressed(0) || gt2.Power.Pressed(1<<16) {
		t.Error("DXP2800 GT power is active low on bit 16, 100 polls")
	}
	d := ButtonsFor("DXP4800")
	if d.Power.bit() != 1 || !d.Power.Pressed(2) || d.Power.Pressed(0) || !d.Reset.Pressed(0) || d.Reset.Pressed(2) {
		t.Error("DXP4800 levels")
	}
	for _, s := range ButtonSpecs {
		if s.PowerPressPolls < 1 || s.Cite == "" || !strings.Contains(s.Cite, "@c75c5ab") {
			t.Errorf("%s: incomplete entry", s.Name)
		}
	}
}

func TestReadState(t *testing.T) {
	m := newFakeMem()
	s := ButtonsFor("DXP4800")
	m.words[0xFD6A0980] = 2 // power held
	m.words[0xFD6D0B50] = 2 // reset released (active low)
	st, err := s.ReadState(m)
	if err != nil || st != (State{Power: true}) {
		t.Fatalf("%+v %v", st, err)
	}
	m.words[0xFD6D0B50] = 0
	st, _ = s.ReadState(m)
	if !st.Reset {
		t.Error("reset held")
	}
	// GT: reads are at the aligned word only.
	m2 := newFakeMem()
	g := ButtonsFor("DXP4800 GT")
	m2.words[0xFED81524] = 1 << 16
	m2.words[0xFED81520] = 1 << 16
	st, err = g.ReadState(m2)
	if err != nil || st != (State{Power: true}) {
		t.Fatalf("%+v %v", st, err)
	}
}

func events(tr *Tracker, active ...bool) (out []string) {
	for _, a := range active {
		for _, e := range tr.Poll(a) {
			out = append(out, e.Name)
		}
	}
	return
}

func run(n int, v bool) []bool { return slices.Repeat([]bool{v}, n) }

func TestTrackerPower(t *testing.T) {
	// Press at the first poll; a short press never gets a release (vendor).
	got := events(NewPowerTracker(1), true, true, false)
	if !slices.Equal(got, []string{"down", "press", "up"}) {
		t.Errorf("short: %v", got)
	}
	// Held 100 polls: release is reported.
	got = events(NewPowerTracker(1), append(run(100, true), false)...)
	if !slices.Equal(got, []string{"down", "press", "up", "release"}) {
		t.Errorf("long: %v", got)
	}
	// 99 polls: still no release.
	got = events(NewPowerTracker(1), append(run(99, true), false)...)
	if slices.Contains(got, "release") {
		t.Errorf("99: %v", got)
	}
	// DXP2800 GT: press only after 100 polls.
	got = events(NewPowerTracker(100), run(99, true)...)
	if slices.Contains(got, "press") {
		t.Errorf("GT 99: %v", got)
	}
	got = events(NewPowerTracker(100), append(run(100, true), false)...)
	if !slices.Equal(got, []string{"down", "press", "up", "release"}) {
		t.Errorf("GT 100: %v", got)
	}
	got = events(NewPowerTracker(100), append(run(50, true), false)...)
	if !slices.Equal(got, []string{"down", "up"}) {
		t.Errorf("GT short: %v", got)
	}
}

func TestTrackerReset(t *testing.T) {
	got := events(NewResetTracker(), append(run(259, true), false)...)
	if !slices.Equal(got, []string{"down", "up"}) {
		t.Errorf("259: %v", got)
	}
	got = events(NewResetTracker(), append(run(260, true), false)...)
	if !slices.Equal(got, []string{"down", "press", "up", "release"}) {
		t.Errorf("260: %v", got)
	}
	// The press is reported once, however long it is held, and the counter stops at 1200.
	tr := NewResetTracker()
	got = events(tr, append(run(2000, true), false)...)
	if n := strings.Count(strings.Join(got, ","), "press"); n != 1 {
		t.Errorf("presses %d", n)
	}
	// A new press starts counting from zero.
	got = events(tr, run(10, true)...)
	if !slices.Equal(got, []string{"down"}) {
		t.Errorf("again: %v", got)
	}
}

func TestWatch(t *testing.T) {
	s := ButtonsFor("DXP4800")
	frames := [][2]bool{} // power held, reset held per tick
	for range 100 {
		frames = append(frames, [2]bool{true, false})
	}
	frames = append(frames, [2]bool{false, false})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newFakeMem()
	m.onRead = func(n int, phys uint64) uint32 {
		// Two reads for the start state, then two per tick (power, reset).
		k := (n - 2) / 2
		if n < 2 {
			return 0x2 &^ 2 // all released at start; power active high, reset (active low) needs bit set
		}
		if k >= len(frames)-1 && n%2 == 1 {
			cancel()
		}
		f := frames[min(k, len(frames)-1)]
		var w uint32
		if phys == 0xFD6A0980 && f[0] {
			w = 2
		}
		if phys == 0xFD6D0B50 && !f[1] {
			w = 2
		}
		return w
	}
	ticks := make(chan time.Time, len(frames))
	for range frames {
		ticks <- time.Time{}
	}
	var out bytes.Buffer
	err := s.Watch(ctx, m, &out, ticks, func() time.Time { return time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"start: power released, reset held", // the start frame reads 0 on the active-low reset: held
		"12:00:00.000 power down",
		"12:00:00.000 power press",
		"12:00:00.000 power up (held 5.00 s)",
		"12:00:00.000 power release (held 5.00 s)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
}
