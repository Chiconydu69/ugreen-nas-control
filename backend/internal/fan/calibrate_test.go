package fan

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// rpmFans: two fans whose rpm is 40 per percent of duty; records the last duty per fan.
type rpmFans struct {
	Fans
	duty map[string]byte
	sets int
}

func (f *rpmFans) Outputs() []Channel             { return []Channel{{ID: "fan1"}, {ID: "fan3"}} }
func (f *rpmFans) MaxDuty() byte                  { return 100 }
func (f *rpmFans) RPM(c Channel) (int, error)     { return int(f.duty[c.ID]) * 40, nil }
func (f *rpmFans) SetPWM(c Channel, d byte) error { f.sets++; f.duty[c.ID] = d; return nil }

func testCalibrator(t *testing.T, cpu int) (*calibrator, *rpmFans) {
	t.Helper()
	t.Setenv("UGREEN_FAN_LOCK", "")
	lockPath = filepath.Join(t.TempDir(), "lock")
	chip := &rpmFans{duty: map[string]byte{}}
	return &calibrator{
		chip: chip, cfg: defaultConfig, sensors: func() Sensors { return Sensors{CPU: &cpu} },
		daemonAlive: func() bool { return true }, stop: make(chan struct{}),
		progress: func(CalibProgress) {}, logf: func(string, ...any) {},
		settle: time.Millisecond, settleBig: time.Millisecond, gap: time.Millisecond,
	}, chip
}

func TestCalibrateSweep(t *testing.T) {
	cb, chip := testCalibrator(t, 50)
	tbl, reason := cb.run()
	if reason != "" || tbl.Time == 0 {
		t.Fatalf("reason %q", reason)
	}
	for _, id := range []string{"fan1", "fan3"} {
		rows := tbl.Fans[id]
		if len(rows) != len(calibSteps) || rows[0] != [2]int{10, 400} || rows[11] != [2]int{100, 4000} {
			t.Fatalf("%s = %v", id, rows)
		}
	}
	if chip.duty["fan1"] != calibHold || chip.duty["fan3"] != 100 {
		t.Fatalf("end state %v", chip.duty)
	}
}

func TestCalibrateAbortsHot(t *testing.T) {
	cb, chip := testCalibrator(t, 85)
	if _, reason := cb.run(); reason != "cpu_hot" {
		t.Fatalf("reason %q", reason)
	}
	if chip.duty["fan1"] != 100 || chip.duty["fan3"] != 100 {
		t.Fatalf("fans not at 100: %v", chip.duty)
	}

	cb, _ = testCalibrator(t, 50)
	hot := 60
	cb.sensors = func() Sensors { c := 50; return Sensors{CPU: &c, Disks: []Disk{{Name: "disk1", Temp: &hot}}} }
	if _, reason := cb.run(); reason != "disk_hot" {
		t.Fatalf("reason %q", reason)
	}
}

// A CPU burst of a few checks does not end the measurement; a long one or a very hot one does.
func TestCalibrateIgnoresCPUBurst(t *testing.T) {
	cb, _ := testCalibrator(t, 50)
	reads := 0
	cb.sensors = func() Sensors {
		reads++
		c := 50
		if reads%10 == 3 || reads%10 == 4 {
			c = 85
		}
		return Sensors{CPU: &c}
	}
	if _, reason := cb.run(); reason != "" {
		t.Fatalf("a short burst stopped the run: %q", reason)
	}

	cb, _ = testCalibrator(t, 96)
	if _, reason := cb.run(); reason != "cpu_hot" || cb.hotRuns != 1 {
		t.Fatalf("very hot: reason %q after %d checks", reason, cb.hotRuns)
	}
}

func TestCalibrateStop(t *testing.T) {
	cb, chip := testCalibrator(t, 50)
	stop := make(chan struct{})
	close(stop)
	cb.stop = stop
	if _, reason := cb.run(); reason != "stopped" || chip.duty["fan1"] != 100 {
		t.Fatalf("reason %q duty %v", reason, chip.duty)
	}
}

func TestQuietPreset(t *testing.T) {
	cpu, err := ParseCurve(presets["quiet"][0])
	if err != nil || cpu[0].Pct != 15 {
		t.Fatalf("quiet cpu = %v %v", cpu, err)
	}
	for i := 1; i < len(cpu); i++ {
		if cpu[i].Pct <= cpu[i-1].Pct || cpu[i].Temp <= cpu[i-1].Temp {
			t.Fatalf("not monotonic: %v", cpu)
		}
	}
}

func TestCalibPause(t *testing.T) {
	calibPausePath = filepath.Join(t.TempDir(), "pause")
	if calibPaused() {
		t.Fatal("paused without file")
	}
	_ = writeJSONAtomic(calibPausePath, 1)
	if !calibPaused() {
		t.Fatal("not paused")
	}
}

// A temperature spike in the middle of a long settle stops the measurement at once, not at the next step.
func TestCalibrateAbortsDuringWait(t *testing.T) {
	cb, chip := testCalibrator(t, 50)
	cb.settle, cb.settleBig, cb.check = time.Minute, time.Minute, 5*time.Millisecond
	calls := 0
	cb.sensors = func() Sensors {
		calls++
		c := 50
		if calls > 3 {
			c = 85
		}
		return Sensors{CPU: &c}
	}
	start := time.Now()
	if _, reason := cb.run(); reason != "cpu_hot" {
		t.Fatalf("reason %q", reason)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	if chip.duty["fan1"] != 100 || chip.duty["fan3"] != 100 {
		t.Fatalf("fans not at 100: %v", chip.duty)
	}
}

// The second start is refused and the lock is free again once the first is done.
func TestCalibrationLock(t *testing.T) {
	calibLockPath = filepath.Join(t.TempDir(), "calib.lock")
	first, err := lockCalibration()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockCalibration(); !errors.Is(err, errBusy) {
		t.Fatalf("second start: %v", err)
	}
	first.Close()
	again, err := lockCalibration()
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	again.Close()
}

func TestStallStart(t *testing.T) {
	spins := &CalibTable{Fans: map[string][][2]int{"fan1": {{10, 400}, {15, 600}, {20, 800}}}}
	late := &CalibTable{Fans: map[string][][2]int{"fan1": {{10, 0}, {15, 0}, {20, 700}}}}
	for _, c := range []struct {
		t    *CalibTable
		fan  string
		want int
	}{{nil, "fan1", 20}, {spins, "fan1", 10}, {late, "fan1", 20}, {spins, "fan2", 20}} {
		if got := stallStart(c.t, c.fan); got != c.want {
			t.Errorf("%v %s: got %d want %d", c.t, c.fan, got, c.want)
		}
	}
}

func TestHeartbeatStopWaits(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pause")
	beat := func() { time.Sleep(30 * time.Millisecond); _ = os.WriteFile(p, []byte("x"), 0o644) }
	stop := startHeartbeat(beat, 5*time.Millisecond, func() bool { return true })
	time.Sleep(20 * time.Millisecond)
	stop()
	_ = os.Remove(p)
	time.Sleep(80 * time.Millisecond)
	if _, err := os.Stat(p); err == nil {
		t.Fatal("pause file recreated after stop")
	}
}

func TestTableCacheReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fan_rpm.json")
	var c tableCache
	if c.get(p) != nil {
		t.Fatal("table without file")
	}
	_ = writeJSONAtomic(p, CalibTable{Fans: map[string][][2]int{"fan1": {{10, 400}}}})
	if got := stallStart(c.get(p), "fan1"); got != 10 {
		t.Fatalf("got %d", got)
	}
	_ = writeJSONAtomic(p, CalibTable{Fans: map[string][][2]int{"fan1": {{10, 0}, {20, 700}}}})
	if got := stallStart(c.get(p), "fan1"); got != 20 {
		t.Fatalf("not reloaded: %d", got)
	}
}

func TestHeartbeatStopsWhenStalled(t *testing.T) {
	var n atomic.Int32
	var ok atomic.Bool
	ok.Store(true)
	stop := startHeartbeat(func() { n.Add(1) }, 5*time.Millisecond, ok.Load)
	time.Sleep(30 * time.Millisecond)
	ok.Store(false)
	time.Sleep(20 * time.Millisecond)
	before := n.Load()
	time.Sleep(50 * time.Millisecond)
	stop()
	if n.Load() != before || before < 2 {
		t.Fatalf("beats: %d before, %d after the stall", before, n.Load())
	}
}

type restoreSpy struct {
	*rpmFans
	restores int
}

func (r *restoreSpy) Restore(Snapshot) error { r.restores++; return nil }

func TestHandBack(t *testing.T) {
	for _, c := range []struct {
		name              string
		crashed           bool
		reason            string
		daemonUp, biosMod bool
		duty              byte
		restores          int
	}{
		{"crash, daemon down", true, "hardware", false, false, 100, 1},
		{"crash, daemon up", true, "hardware", true, false, 100, 0},
		{"read_error, daemon down", false, "read_error", false, false, 10, 1},
		{"read_error, daemon up", false, "read_error", true, false, 10, 0},
		{"daemon_stopped", false, "daemon_stopped", true, false, 10, 1},
		{"bios mode", false, "stopped", true, true, 10, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			cb, chip := testCalibrator(t, 40)
			chip.duty["fan1"], chip.duty["fan3"] = 10, 10
			spy := &restoreSpy{rpmFans: chip}
			cb.chip = spy
			cb.logf = t.Logf
			handBack(cb, Snapshot{}, c.crashed, c.reason, c.daemonUp, c.biosMod)
			if chip.duty["fan1"] != c.duty || spy.restores != c.restores {
				t.Fatalf("duty %d restores %d, want %d and %d", chip.duty["fan1"], spy.restores, c.duty, c.restores)
			}
		})
	}
}
