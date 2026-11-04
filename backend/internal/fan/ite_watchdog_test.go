package fan

import (
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

type sioWrite struct{ ldn, reg, v byte }

// fakeSIO is the IT8613E fake with the Super-I/O registers the watchdog and the AC-loss setting use: a register file
// per (logical device, register), a log of every write to it and a counter of every port access.
type fakeSIO struct {
	*fakeITE
	regs        map[[2]byte]byte
	wlog        []sioWrite
	access      int
	counting    bool // every read of the value's LSB lowers the 16-bit value by one, as a running timer would
	ignoreWrite bool // writes are logged but do not stick
	id          uint16
}

func newFakeSIO() *fakeSIO {
	return &fakeSIO{fakeITE: newFakeITE(), regs: map[[2]byte]byte{}}
}

func (f *fakeSIO) mapped() bool {
	if f.sioKey < 4 {
		return false
	}
	switch f.ldn {
	case sioLDNGPIO:
		return f.sioIndex >= iteRegWdtCtrl && f.sioIndex <= iteRegWdtHi
	case sioLDNMonitor:
		return f.sioIndex == iteRegACF2 || f.sioIndex == iteRegACF4
	}
	return false
}

func (f *fakeSIO) setVal(v uint16) {
	f.regs[[2]byte{sioLDNGPIO, iteRegWdtLo}], f.regs[[2]byte{sioLDNGPIO, iteRegWdtHi}] = byte(v), byte(v>>8)
}

func (f *fakeSIO) val() uint16 {
	return uint16(f.regs[[2]byte{sioLDNGPIO, iteRegWdtHi}])<<8 | uint16(f.regs[[2]byte{sioLDNGPIO, iteRegWdtLo}])
}

func (f *fakeSIO) In(port uint16) (byte, error) {
	f.access++
	if port == sioData && f.id != 0 && f.sioKey >= 4 && (f.sioIndex == 0x20 || f.sioIndex == 0x21) {
		return byte(f.id >> (8 * (0x21 - uint16(f.sioIndex)))), nil
	}
	if port == sioData && f.mapped() {
		v := f.regs[[2]byte{f.ldn, f.sioIndex}]
		if f.counting && f.ldn == sioLDNGPIO && f.sioIndex == iteRegWdtLo && f.val() > 0 {
			f.setVal(f.val() - 1)
		}
		return v, nil
	}
	return f.fakeITE.In(port)
}

func (f *fakeSIO) Out(port uint16, v byte) error {
	f.access++
	if port == sioData && f.mapped() {
		f.wlog = append(f.wlog, sioWrite{f.ldn, f.sioIndex, v})
		if !f.ignoreWrite {
			f.regs[[2]byte{f.ldn, f.sioIndex}] = v
		}
		return nil
	}
	return f.fakeITE.Out(port, v)
}

// wdtRig is a petter on an IT8613E model with the DMI, state files and the sampling gap isolated.
type wdtRig struct {
	f    *fakeSIO
	chip *Chip
	p    *Petter
	now  time.Time
}

func newWdtRig(t *testing.T, val uint16, cfg byte, counting bool) *wdtRig {
	t.Helper()
	isolate(t, "DXP6800 Pro")
	oldGap := sioGap
	sioGap = time.Millisecond
	t.Cleanup(func() { sioGap = oldGap })
	f := newFakeSIO()
	f.regs[[2]byte{sioLDNGPIO, iteRegWdtCfg}] = cfg
	f.setVal(val)
	f.counting = counting
	chip, err := Detect(f)
	if err != nil {
		t.Fatal(err)
	}
	f.wlog, f.access = nil, 0
	return &wdtRig{f: f, chip: chip, p: &Petter{}, now: time.Unix(1_000_000, 0)}
}

// tick advances the clock by d first.
func (r *wdtRig) tick(t *testing.T, d time.Duration) WatchdogStatus {
	t.Helper()
	r.now = r.now.Add(d)
	st, ok, err := r.p.Tick(r.chip, r.now)
	if !ok || err != nil {
		t.Fatalf("tick: ok %v err %v", ok, err)
	}
	return st
}

const cfgSecondsKRST = iteWdtSeconds | 0x40 | 0x10 // TOV1, KRST, PWROK

func TestITEWatchdogOffNoWrites(t *testing.T) {
	r := newWdtRig(t, 0, cfgSecondsKRST, false)
	for range 5 {
		if st := r.tick(t, 10*time.Second); st.State != WdtOff || st.Armed {
			t.Fatalf("status %+v", st)
		}
	}
	if len(r.f.wlog) != 0 {
		t.Fatalf("writes %v", r.f.wlog)
	}
}

func TestITEWatchdogCountingPets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    byte
		lo, hi byte
		gap    time.Duration
	}{
		{"seconds", cfgSecondsKRST, 0x2c, 0x01, 2 * time.Second}, // 300
		{"minutes", 0x50, 5, 0, 62 * time.Second},                // ceil(300 / 60)
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newWdtRig(t, 1000, tc.cfg, true)
			if st := r.tick(t, 0); st.State != WdtNotCounting || st.Armed || len(r.f.wlog) != 0 {
				t.Fatalf("first sample: %+v, writes %v", st, r.f.wlog)
			}
			st := r.tick(t, tc.gap)
			if st.State != WdtArmed || !st.Armed || st.Counter == nil || *st.Counter != 999 {
				t.Fatalf("status %+v", st)
			}
			want := []sioWrite{{7, 0x72, tc.cfg}, {7, 0x73, tc.lo}, {7, 0x74, tc.hi}}
			if !slices.Equal(r.f.wlog, want) {
				t.Fatalf("writes %v, want %v", r.f.wlog, want)
			}
			r.f.wlog = nil
			r.tick(t, 10*time.Second)
			if len(r.f.wlog) != 0 {
				t.Fatalf("fed again within the interval: %v", r.f.wlog)
			}
			r.f.setVal(1000)
			r.tick(t, 60*time.Second)
			if !slices.Equal(r.f.wlog, want) {
				t.Fatalf("second feed %v", r.f.wlog)
			}
			for _, w := range r.f.wlog {
				if w.reg == iteRegWdtCtrl {
					t.Fatal("0x71 written")
				}
			}
		})
	}
}

// The proof spans daemon ticks: nothing sleeps under the lock, and a minutes timer needs more than a minute between the
// samples (it cannot move faster, so two samples seconds apart prove nothing).
func TestITEWatchdogProofSpansTicks(t *testing.T) {
	r := newWdtRig(t, 1000, 0x50, true) // minutes; the fake falls on every read
	sioGap = 5 * time.Second
	start := time.Now()
	for _, d := range []time.Duration{0, 2 * time.Second, 30 * time.Second, 20 * time.Second} {
		if st := r.tick(t, d); st.State != WdtNotCounting || st.Armed {
			t.Fatalf("armed %v after %v: %+v", d, r.now, st)
		}
	}
	if len(r.f.wlog) != 0 {
		t.Fatalf("writes before 61 s: %v", r.f.wlog)
	}
	if st := r.tick(t, 10*time.Second); st.State != WdtArmed || len(r.f.wlog) != 3 { // 62 s after the first sample
		t.Fatalf("after 62 s: %+v, writes %v", st, r.f.wlog)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("a tick slept")
	}
}

func TestITEWatchdogStaticValueNeverWritten(t *testing.T) {
	r := newWdtRig(t, 500, cfgSecondsKRST, false)
	for i := range 20 {
		if st := r.tick(t, 10*time.Second); st.State != WdtNotCounting || st.Armed {
			t.Fatalf("tick %d: %+v", i, st)
		}
	}
	if len(r.f.wlog) != 0 {
		t.Fatalf("writes %v", r.f.wlog)
	}
	if st := ReadWatchdogStatus(); st == nil || st.State != WdtNotCounting {
		t.Fatalf("persisted %+v", st)
	}
}

func TestITEWatchdogLatchResets(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	r.tick(t, 0)
	r.tick(t, 2*time.Second)
	if !r.p.counting || len(r.f.wlog) != 3 {
		t.Fatalf("counting %v, writes %v", r.p.counting, r.f.wlog)
	}
	r.f.wlog = nil
	r.f.setVal(0)
	if st := r.tick(t, 70*time.Second); st.State != WdtOff || r.p.counting {
		t.Fatalf("status %+v counting %v", st, r.p.counting)
	}
	r.f.counting = false
	r.f.setVal(500) // someone armed it and it stands still: the old proof must not carry over
	for range 5 {
		r.tick(t, 70*time.Second)
	}
	if len(r.f.wlog) != 0 {
		t.Fatalf("writes %v", r.f.wlog)
	}
}

func TestITEWatchdogYieldsToModule(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	d := t.TempDir()
	roots := conflict.Roots{ProcModules: filepath.Join(d, "modules"), SysModule: filepath.Join(d, "sys"), Self: 1}
	if err := os.WriteFile(roots.ProcModules, []byte("it87_wdt 12288 0 - Live 0x0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.p.Yield = conflict.Present(roots.Detect(), conflict.Watchdog)
	st := r.tick(t, 0)
	if st.State != WdtYielded || r.f.access != 0 {
		t.Fatalf("state %q, %d port accesses", st.State, r.f.access)
	}
	if got := ReadWatchdogStatus(); got == nil || got.State != WdtYielded {
		t.Fatalf("persisted %+v", got)
	}
	if err := r.p.Stop(r.chip); err != nil || r.f.access != 0 {
		t.Fatalf("stop while yielding: %v, %d accesses", err, r.f.access)
	}
}

func TestITEWatchdogChipIDGate(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	r.f.id = 0x8612
	if _, ok, err := r.p.Tick(r.chip, r.now); ok || err != nil {
		t.Fatalf("ok %v err %v", ok, err)
	}
	if err := r.chip.WatchdogPet(ITEWdt{cfg: cfgSecondsKRST, val: 5}, 300); !errors.Is(err, errNotIT8613) {
		t.Fatalf("pet: %v", err)
	}
	if err := r.chip.DisarmWatchdog(); !errors.Is(err, errNotIT8613) {
		t.Fatalf("disarm: %v", err)
	}
	if len(r.f.wlog) != 0 {
		t.Fatalf("writes %v", r.f.wlog)
	}
}

func TestITEWatchdogOnlyOnItsModels(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	isolate(t, "DXP2800 GT")
	if _, ok, _ := r.p.Tick(r.chip, r.now); ok || len(r.f.wlog) != 0 {
		t.Fatalf("ok %v writes %v", ok, r.f.wlog)
	}
}

func TestITEWatchdogStopWritesZeroOnlyIfPetted(t *testing.T) {
	r := newWdtRig(t, 0, cfgSecondsKRST, false)
	r.tick(t, 0)
	if err := r.p.Stop(r.chip); err != nil || len(r.f.wlog) != 0 {
		t.Fatalf("never fed: %v %v", err, r.f.wlog)
	}

	r = newWdtRig(t, 1000, cfgSecondsKRST, true)
	r.tick(t, 0)
	r.tick(t, 2*time.Second)
	r.f.wlog = nil
	if err := r.p.Stop(r.chip); err != nil {
		t.Fatal(err)
	}
	if want := []sioWrite{{7, 0x73, 0}, {7, 0x74, 0}}; !slices.Equal(r.f.wlog, want) {
		t.Fatalf("stop writes %v", r.f.wlog)
	}
	r.f.wlog = nil
	if err := r.p.Stop(r.chip); err != nil || len(r.f.wlog) != 0 {
		t.Fatalf("second stop: %v %v", err, r.f.wlog)
	}
	if st := ReadWatchdogStatus(); st == nil || st.State != WdtOff {
		t.Fatalf("persisted %+v", st)
	}
}

func TestProbeRestoreNeverDisarms(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	rig := iteRig{chip: r.chip}
	snap, err := rig.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.Restore(snap); err != nil {
		t.Fatal(err)
	}
	if len(r.f.wlog) != 0 || r.f.val() != 1000 {
		t.Fatalf("watchdog touched: %v, value %d", r.f.wlog, r.f.val())
	}
}

func TestITEWatchdogWriteListIsStructural(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, false)
	if err := r.chip.WatchdogPet(ITEWdt{cfg: cfgSecondsKRST}, 300); !errors.Is(err, errNeverArm) {
		t.Fatalf("pet of a stopped timer: %v", err)
	}
	if err := r.chip.WatchdogPet(ITEWdt{cfg: cfgSecondsKRST, val: 9}, 0); !errors.Is(err, errNeverArm) {
		t.Fatalf("pet with no time: %v", err)
	}
	err := r.chip.session(sioOpts{ldn: sioLDNGPIO, allow: func(reg byte) bool { return reg >= iteRegWdtCfg }}, func(s *sio) error {
		s.wr(iteRegWdtCtrl, 1)
		return nil
	})
	if !errors.Is(err, errNoSIOWrite) || len(r.f.wlog) != 0 {
		t.Fatalf("0x71: %v, writes %v", err, r.f.wlog)
	}
	if err := r.chip.session(sioOpts{ldn: sioLDNGPIO}, func(s *sio) error { s.wr(iteRegWdtLo, 1); return nil }); !errors.Is(err, errNoSIOWrite) {
		t.Fatalf("read-only session wrote: %v", err)
	}
	if n := wdtUnits(0x00, 100000); n != 1667 { // minutes
		t.Fatalf("units %d", n)
	}
	if n := wdtUnits(iteWdtSeconds, 1_000_000); n != iteWdtMaxValue {
		t.Fatalf("clamp %d", n)
	}
}

// An IT8613E model without a confirmed fan map has no fan daemon; the watchdog-only daemon still feeds the timer, never
// touches a fan register, and stops the timer it fed when told to stop.
func TestWatchdogOnlyDaemonFeedsWithoutFanMap(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	dir := t.TempDir()
	oldLock, oldTick, oldNow, oldDet := lockPath, tick, now, detectConflicts
	lockPath = filepath.Join(dir, "lock")
	tick = 5 * time.Millisecond
	var clock atomic.Int64
	now = func() time.Time { return r.now.Add(time.Duration(clock.Add(1)) * 20 * time.Second) }
	detectConflicts = func() []conflict.Conflict { return nil }
	t.Cleanup(func() { lockPath, tick, now, detectConflicts = oldLock, oldTick, oldNow, oldDet })

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM) // keeps a late signal from ending the test binary
	defer signal.Stop(stop)

	done := make(chan error, 1)
	go func() { done <- runWatchdogOnly(r.chip) }()
	for clock.Load() < 10 {
		time.Sleep(5 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r.f.writes != 0 {
		t.Fatalf("%d hardware-monitor writes", r.f.writes)
	}
	var lsb []byte
	for _, w := range r.f.wlog {
		if w.reg == iteRegWdtCtrl {
			t.Fatal("0x71 written")
		}
		if w.reg == iteRegWdtLo {
			lsb = append(lsb, w.v)
		}
	}
	if len(lsb) < 3 || lsb[0] != 0x2c || lsb[len(lsb)-1] != 0 {
		t.Fatalf("value LSB writes %v", lsb)
	}
	if r.f.val() != 0 {
		t.Fatalf("timer left at %d", r.f.val())
	}
}

func TestOpenWatchdogOnlyRefusesOtherModels(t *testing.T) {
	for _, m := range []string{"DXP4800 GT", "DXP6800 Pro", "DX4600 Pro", "iDX6011", ""} {
		isolate(t, m)
		if _, err := openWatchdogOnly(); !errors.Is(err, errNoWatchdog) {
			t.Errorf("%q: %v", m, err)
		}
	}
}

// A clean stop leaves a marker, the next daemon start puts the timer back and feeds it as before.
func TestITEWatchdogRestartRestoresTimer(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	r.tick(t, 0)
	r.tick(t, 2*time.Second)
	if err := r.p.Stop(r.chip); err != nil || r.f.val() != 0 {
		t.Fatalf("stop: %v, value %d", err, r.f.val())
	}
	if m := readWdtMarker(); m == nil || m.Cfg != cfgSecondsKRST || m.Value != 300 {
		t.Fatalf("marker %+v", m)
	}

	r.p = &Petter{} // the restarted daemon
	r.f.counting = false
	r.f.wlog = nil
	st := r.tick(t, time.Second)
	if st.State != WdtArmed || !st.Armed || r.f.val() != 300 || r.f.regs[[2]byte{sioLDNGPIO, iteRegWdtCfg}] != cfgSecondsKRST {
		t.Fatalf("status %+v, value %d", st, r.f.val())
	}
	if readWdtMarker() != nil {
		t.Fatal("marker kept")
	}
	for _, w := range r.f.wlog {
		if w.reg == iteRegWdtCtrl {
			t.Fatal("0x71 written")
		}
	}
	r.f.counting = true
	r.f.wlog = nil
	if r.tick(t, 60*time.Second); len(r.f.wlog) == 0 {
		t.Fatal("restored timer was not fed")
	}
	if err := r.p.Stop(r.chip); err != nil || readWdtMarker() == nil {
		t.Fatalf("second stop: %v, marker %v", err, readWdtMarker())
	}
}

func TestITEWatchdogMarkerIgnoredWhenTimerRuns(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, false)
	if err := writeJSONAtomic(iteWdtMarkerPath, wdtMarker{Cfg: cfgSecondsKRST, Value: 300}); err != nil {
		t.Fatal(err)
	}
	r.tick(t, 0)
	if readWdtMarker() != nil || len(r.f.wlog) != 0 {
		t.Fatalf("marker %v, writes %v", readWdtMarker(), r.f.wlog)
	}
	// a stale marker must not re-arm a timer that is off later
	r.f.setVal(0)
	if st := r.tick(t, time.Second); st.State != WdtOff || len(r.f.wlog) != 0 {
		t.Fatalf("%+v %v", st, r.f.wlog)
	}
}

func TestITEWatchdogNoMarkerStaysOff(t *testing.T) {
	r := newWdtRig(t, 0, cfgSecondsKRST, false)
	if st := r.tick(t, 0); st.State != WdtOff || len(r.f.wlog) != 0 {
		t.Fatalf("%+v %v", st, r.f.wlog)
	}
}

// The value never reads 0 while a feed grows the MSB: MSB first then, LSB first when it does not grow.
func TestITEWatchdogPetOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		cur  uint16
		cfg  byte
		want []sioWrite
	}{
		{"msb grows, lsb wraps to 0", 5, cfgSecondsKRST, []sioWrite{{7, 0x72, cfgSecondsKRST}, {7, 0x74, 1}, {7, 0x73, 0x2c}}},
		{"msb shrinks", 1000, cfgSecondsKRST, []sioWrite{{7, 0x72, cfgSecondsKRST}, {7, 0x73, 0x2c}, {7, 0x74, 1}}},
		{"msb equal", 0x1ff, cfgSecondsKRST, []sioWrite{{7, 0x72, cfgSecondsKRST}, {7, 0x73, 0x2c}, {7, 0x74, 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newWdtRig(t, tc.cur, tc.cfg, false)
			cur, err := r.chip.WatchdogRead()
			if err != nil {
				t.Fatal(err)
			}
			r.f.wlog = nil
			if err := r.chip.WatchdogPet(cur, 300); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(r.f.wlog, tc.want) {
				t.Fatalf("writes %v, want %v", r.f.wlog, tc.want)
			}
		})
	}
}

// A timer that reads 0 because someone stopped it is not ours to restore: no marker, the next start stays off.
func TestITEWatchdogStopAfterExternalZeroLeavesNoMarker(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	r.tick(t, 0)
	r.tick(t, 2*time.Second)
	if !r.p.petted {
		t.Fatal("not petted")
	}
	r.f.counting = false
	r.f.setVal(0)
	r.tick(t, time.Second)
	if r.p.petted {
		t.Fatal("petted stays set after the timer read 0")
	}
	if err := r.p.Stop(r.chip); err != nil || readWdtMarker() != nil {
		t.Fatalf("stop: %v, marker %v", err, readWdtMarker())
	}

	// Stop between two ticks, before the daemon saw the 0: the read right before the disarm decides.
	r = newWdtRig(t, 1000, cfgSecondsKRST, true)
	r.tick(t, 0)
	r.tick(t, 2*time.Second)
	r.f.counting = false
	r.f.setVal(0)
	if err := r.p.Stop(r.chip); err != nil || readWdtMarker() != nil {
		t.Fatalf("stop on 0: %v, marker %v", err, readWdtMarker())
	}
	r.p = &Petter{}
	if st := r.tick(t, time.Second); st.State != WdtOff || r.f.val() != 0 {
		t.Fatalf("next start: %+v, value %d", st, r.f.val())
	}
}

// Yielding to it87_wdt drops a marker of an earlier stop for good and the petted flag with it.
func TestITEWatchdogYieldRemovesMarker(t *testing.T) {
	r := newWdtRig(t, 0, cfgSecondsKRST, false)
	if err := writeJSONAtomic(iteWdtMarkerPath, wdtMarker{Cfg: cfgSecondsKRST, Value: 300}); err != nil {
		t.Fatal(err)
	}
	r.p.petted = true
	r.p.Yield = true
	if st := r.tick(t, 0); st.State != WdtYielded || readWdtMarker() != nil || r.p.petted {
		t.Fatalf("%+v marker %v petted %v", st, readWdtMarker(), r.p.petted)
	}
	r.p.Yield = false
	if st := r.tick(t, time.Second); st.State != WdtOff || len(r.f.wlog) != 0 || readWdtMarker() != nil {
		t.Fatalf("restored after the yield: %+v %v", st, r.f.wlog)
	}
}

// A marker that cannot be written must not keep the timer armed.
func TestITEWatchdogStopDisarmsWhenMarkerFails(t *testing.T) {
	r := newWdtRig(t, 1000, cfgSecondsKRST, true)
	r.tick(t, 0)
	r.tick(t, 2*time.Second)
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	iteWdtMarkerPath = filepath.Join(blocker, "marker")
	if err := r.p.Stop(r.chip); err == nil {
		t.Fatal("marker error not reported")
	}
	if r.f.val() != 0 {
		t.Fatalf("timer left at %d", r.f.val())
	}
}
