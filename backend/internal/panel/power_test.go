package panel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// DXP8800 Plus pads: power 0xFD6A0B40 (active high), reset 0xFD6D0A10 (active low), both bit 1.
const (
	pwrAddr, rstAddr = 0xFD6A0B40, 0xFD6D0A10
	pwrHeld, pwrUp   = 0x84000102, 0x84000100
	rstUp, rstHeld   = 0x84000102, 0x84000100
	masked           = 0xFFFFFFFF
	pastGrace        = 1210 // polls of 50 ms: just over PowerGrace
	rigModel         = "DXP8800 Plus"
)

type frame struct{ power, reset uint32 }

func rep(n int, f frame) []frame { return slices.Repeat([]frame{f}, n) }

var (
	idle     = frame{pwrUp, rstUp}
	held     = frame{pwrHeld, rstUp}
	resetHit = frame{pwrUp, rstHeld}
	dead     = frame{masked, masked}
)

func seq(parts ...[]frame) (out []frame) {
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

type rig struct {
	model     string
	confirmed bool
	settings  map[string]string
	args      []string
	frames    []frame
	failWith  error
}

type result struct {
	code      int
	out, errs string
	events    []string // "notify", "shutdown" in call order
	logs      []string
	opened    bool
}

func (r rig) run(t *testing.T) result {
	t.Helper()
	dir := t.TempDir()
	if err := writeFile(dir, r.model); err != nil {
		t.Fatal(err)
	}
	var res result
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newFakeMem()
	m.onRead = func(n int, phys uint64) uint32 {
		i := n / 2
		if i >= len(r.frames) {
			cancel()
			return pwrUp
		}
		if i == len(r.frames)-1 && n%2 == 1 {
			cancel()
		}
		f := r.frames[i]
		if phys == pwrAddr {
			return f.power
		}
		return f.reset
	}
	ticks := make(chan time.Time, len(r.frames))
	for range r.frames {
		ticks <- time.Time{}
	}
	base := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	env := Env{
		DMIDir:    dir,
		OpenMem:   func(bool) (Mem, error) { res.opened = true; return m, nil },
		Tick:      func() (<-chan time.Time, func()) { return ticks, func() {} },
		Now:       func() time.Time { return base.Add(time.Duration(len(m.reads)/2) * Poll) },
		Status:    func(string) string { return "confirmed by users, #165" },
		Confirmed: func(string) bool { return r.confirmed },
		Settings:  func() map[string]string { return r.settings },
		Logf:      func(f string, a ...any) { res.logs = append(res.logs, strings.TrimSpace(fmt.Sprintf(f, a...))) },
		Notify:    func(subject, _ string) { res.events = append(res.events, "notify:"+subject) },
		Shutdown:  func() error { res.events = append(res.events, "shutdown"); return r.failWith },
	}
	var out, errs bytes.Buffer
	res.code = Main(ctx, env, append([]string{"power-action"}, r.args...), &out, &errs)
	res.out, res.errs = out.String(), errs.String()
	return res
}

func on(extra ...string) map[string]string {
	s := map[string]string{CfgPowerShutdown: "1"}
	for i := 0; i+1 < len(extra); i += 2 {
		s[extra[i]] = extra[i+1]
	}
	return s
}

func newRig(frames []frame) rig {
	return rig{model: rigModel, confirmed: true, settings: on(), frames: frames}
}

const shutdownNote = "notify:Shutdown requested by front power button"

func TestPowerTapDoesNothing(t *testing.T) {
	for _, tap := range []int{1, 4, 10, 29} { // 29 polls = 1.45 s, one short of the default 1.5 s
		r := newRig(seq(rep(pastGrace, idle), rep(tap, held), rep(40, idle))).run(t)
		if len(r.events) != 0 || r.code != 0 {
			t.Errorf("tap of %d polls: code %d, events %v", tap, r.code, r.events)
		}
	}
}

func TestPowerHoldFiresOnceWhileStillHeld(t *testing.T) {
	// 30 polls = 1.5 s: the 30th poll fires, with the button still down (no release in the frames).
	r := newRig(seq(rep(pastGrace, idle), rep(30, held))).run(t)
	if want := []string{shutdownNote, "shutdown"}; !slices.Equal(r.events, want) {
		t.Fatalf("events %v, want %v", r.events, want)
	}
	// A long hold, then a second hold: still exactly one action (the run ends after the first).
	r = newRig(seq(rep(pastGrace, idle), rep(400, held), rep(10, idle), rep(100, held))).run(t)
	if want := []string{shutdownNote, "shutdown"}; !slices.Equal(r.events, want) {
		t.Fatalf("long hold: events %v, want %v", r.events, want)
	}
	if !strings.Contains(strings.Join(r.logs, "\n"), "shutting down Unraid") {
		t.Errorf("no log line before the action: %v", r.logs)
	}
}

func TestPowerHoldTimeIsConfigurable(t *testing.T) {
	c := newRig(seq(rep(pastGrace, idle), rep(59, held), rep(5, idle)))
	c.settings = on(CfgPowerHold, "3")
	if r := c.run(t); len(r.events) != 0 {
		t.Errorf("3 s hold, 59 polls: %v", r.events)
	}
	c.frames = seq(rep(pastGrace, idle), rep(60, held))
	if r := c.run(t); len(r.events) != 2 {
		t.Errorf("3 s hold, 60 polls: %v", r.events)
	}
	for hold, polls := range map[string]int{"": 30, "1": 20, "1.5": 30, "2.35": 47, "3": 60, "0.5": 30, "3.5": 30, "4": 30, "abc": 30, "NaN": 30, "-2": 30} {
		if got := ParsePowerConfig(on(CfgPowerHold, hold)).Polls(); got != polls {
			t.Errorf("hold %q: %d polls, want %d", hold, got, polls)
		}
	}
	if ParsePowerConfig(nil).Enabled || ParsePowerConfig(map[string]string{CfgPowerShutdown: "0"}).Enabled || !ParsePowerConfig(on()).Enabled {
		t.Error("enabled parsed wrongly")
	}
}

func TestPowerUnreadableMeansNoAction(t *testing.T) {
	// Registers masked the whole time, even with a held word around it.
	r := newRig(seq(rep(pastGrace, idle), rep(200, dead), rep(5, idle))).run(t)
	if len(r.events) != 0 {
		t.Errorf("masked registers: %v", r.events)
	}
	if !strings.Contains(strings.Join(r.logs, "\n"), "cannot be read") {
		t.Errorf("the unreadable state is not logged: %v", r.logs)
	}
	// A masked read in the middle of a hold restarts it: 20 + gap + 20 polls never add up to 30.
	r = newRig(seq(rep(pastGrace, idle), rep(20, held), rep(1, dead), rep(20, held), rep(5, idle))).run(t)
	if len(r.events) != 0 {
		t.Errorf("hold interrupted by a masked read: %v", r.events)
	}
	// The reset register alone masked also counts as unreadable.
	r = newRig(seq(rep(pastGrace, idle), rep(60, frame{pwrHeld, masked}))).run(t)
	if len(r.events) != 0 {
		t.Errorf("reset masked: %v", r.events)
	}
}

func TestPowerGracePeriod(t *testing.T) {
	// A hold in the first minute is ignored.
	r := newRig(rep(200, held)).run(t)
	if len(r.events) != 0 {
		t.Errorf("hold in the grace period: %v", r.events)
	}
	// Idle for 59 s, then a long hold across the end of the grace period: no action while it began before it ended and is not released...
	r = newRig(seq(rep(1100, idle), rep(300, held), rep(5, idle))).run(t)
	if len(r.events) != 0 {
		t.Errorf("hold that began in the grace period: %v", r.events)
	}
	// ...but the next hold after a release counts.
	r = newRig(seq(rep(1100, idle), rep(300, held), rep(5, idle), rep(30, held))).run(t)
	if len(r.events) != 2 {
		t.Errorf("hold after the grace period: %v", r.events)
	}
}

func TestPowerNeverActsOnReset(t *testing.T) {
	r := newRig(seq(rep(pastGrace, idle), rep(400, resetHit), rep(5, idle))).run(t)
	if len(r.events) != 0 {
		t.Errorf("reset held: %v", r.events)
	}
}

func TestPowerModelNotConfirmed(t *testing.T) {
	for _, model := range []string{"DXP4800 Plus", "DXP8800 Plus"} {
		c := newRig(seq(rep(pastGrace, idle), rep(60, held)))
		c.model, c.confirmed = model, false
		r := c.run(t)
		if r.code == 0 || r.opened || len(r.events) != 0 || !strings.Contains(r.errs, "not confirmed") {
			t.Errorf("%s: code %d opened %v events %v err %q", model, r.code, r.opened, r.events, r.errs)
		}
	}
	// A model without a button map.
	c := newRig(nil)
	c.model = "Standard PC"
	if r := c.run(t); r.code == 0 || r.opened {
		t.Errorf("unknown model: code %d opened %v", r.code, r.opened)
	}
}

func TestPowerOptionOffRunsNothing(t *testing.T) {
	for _, settings := range []map[string]string{nil, {CfgPowerShutdown: "0"}, {CfgPowerShutdown: "true"}} {
		c := newRig(seq(rep(pastGrace, idle), rep(60, held)))
		c.settings = settings
		r := c.run(t)
		if r.code != 0 || r.opened || len(r.events) != 0 || !strings.Contains(r.out, "is off") {
			t.Errorf("settings %v: code %d opened %v events %v out %q", settings, r.code, r.opened, r.events, r.out)
		}
	}
}

func TestPowerDryRun(t *testing.T) {
	c := newRig(seq(rep(pastGrace, idle), rep(10, held), rep(10, idle), rep(30, held)))
	c.args = []string{"--dry-run"}
	r := c.run(t)
	if len(r.events) != 0 {
		t.Errorf("dry run called %v", r.events)
	}
	if r.code != 0 || strings.Count(r.out, "\nwould shut down now\n") != 1 {
		t.Errorf("dry run output (code %d):\n%s", r.code, r.out)
	}
	if len(r.logs) != 0 {
		t.Errorf("dry run wrote to the syslog: %v", r.logs)
	}
	// A tap prints nothing of the kind, and the option may be off: a dry run is how to try it first.
	c = newRig(seq(rep(pastGrace, idle), rep(10, held), rep(10, idle)))
	c.args, c.settings = []string{"--dry-run"}, nil
	r = c.run(t)
	if strings.Contains(r.out, "\nwould shut down now") || !r.opened {
		t.Errorf("dry run, tap, option off: opened %v\n%s", r.opened, r.out)
	}
	// The model rule holds for a dry run too.
	c.confirmed = false
	if r = c.run(t); r.code == 0 || r.opened {
		t.Errorf("dry run on an unconfirmed model: code %d opened %v", r.code, r.opened)
	}
}

func TestPowerShutdownFailureIsReportedNotRetried(t *testing.T) {
	c := newRig(seq(rep(pastGrace, idle), rep(400, held)))
	c.failWith = errors.New("boom")
	r := c.run(t)
	if r.code != 1 || !slices.Equal(r.events, []string{shutdownNote, "shutdown"}) {
		t.Errorf("code %d events %v", r.code, r.events)
	}
}
