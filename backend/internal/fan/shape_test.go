package fan

import (
	"testing"
	"time"
)

const dt2 = 2 * time.Second

func params() ShapeParams {
	return ShapeParams{MinRun: 10, UpStep: 100, DownStep: stepDown}
}

func TestShapeDefaultsEqualSmooth(t *testing.T) {
	for cur := -1; cur <= 100; cur++ {
		for target := 10; target <= 100; target += 3 {
			st := ShapeState{Current: cur, Held: -1}
			got, _ := Shape(st, target, params(), dt2, false)
			if want := Smooth(cur, target); got != want {
				t.Fatalf("Shape(cur %d, target %d) = %d, Smooth = %d", cur, target, got, want)
			}
		}
	}
}

func TestShapeDeadbandBoundary(t *testing.T) {
	p := params()
	p.Deadband = 3
	p.DownStep = 100
	st := ShapeState{Current: 40, Held: 40}
	// flapping inside the band changes nothing, 3 points moves
	for _, tc := range []struct{ target, want int }{
		{41, 40}, {39, 40}, {42, 40}, {38, 40}, {43, 43}, {42, 43}, {41, 43}, {40, 40},
	} {
		var out int
		out, st = Shape(st, tc.target, p, dt2, false)
		if out != tc.want {
			t.Fatalf("target %d: got %d, want %d", tc.target, out, tc.want)
		}
	}
	// 100 % is never held back
	p.Deadband = 10
	st = ShapeState{Current: 98, Held: 98}
	if out, _ := Shape(st, 100, p, dt2, false); out != 100 {
		t.Fatalf("100 held back: %d", out)
	}
}

func TestShapeKickStart(t *testing.T) {
	p := params()
	p.AllowOff, p.MinRun, p.StartPct, p.StartMS = true, 20, 60, 3000
	// from a stop: StartPct for StartMS (two ticks of 2 s), then the target
	st := ShapeState{Current: 0, Held: 0}
	var out int
	var got []int
	for range 4 {
		out, st = Shape(st, 30, p, dt2, false)
		got = append(got, out)
	}
	if want := []int{60, 60, 58, 56}; !equalInts(got, want) {
		t.Fatalf("kick = %v, want %v", got, want)
	}

	// calibration: the measured stall start serves as StartPct when none is set, and raises a lower one
	p.StartPct, p.StallStart = 0, 35
	if out, _ = Shape(ShapeState{Current: 0, Held: 0}, 30, p, dt2, false); out != 35 {
		t.Fatalf("measured kick = %d", out)
	}
	p.StartPct = 20
	if out, _ = Shape(ShapeState{Current: 0, Held: 0}, 30, p, dt2, false); out != 35 {
		t.Fatalf("kick below the measured start = %d", out)
	}
	p.StartPct = 50
	if out, _ = Shape(ShapeState{Current: 0, Held: 0}, 30, p, dt2, false); out != 50 {
		t.Fatalf("kick above the measured start = %d", out)
	}

	// none without StartMS, without a known speed, when already running, or when the target is not higher
	for name, tc := range map[string]struct {
		cur, target int
		ms          int
	}{
		"no ms":      {0, 30, 0},
		"unknown":    {-1, 30, 3000},
		"running":    {25, 30, 3000},
		"at min_run": {20, 30, 3000},
	} {
		q := p
		q.StartMS, q.StartPct = tc.ms, 60
		if out, _ := Shape(ShapeState{Current: tc.cur, Held: -1}, tc.target, q, dt2, false); out >= 60 {
			t.Errorf("%s: kicked (%d)", name, out)
		}
	}
	// no calibration, no StartPct: nothing to kick with
	p.StartPct, p.StallStart = 0, 0
	if out, _ = Shape(ShapeState{Current: 0, Held: 0}, 30, p, dt2, false); out != 30 {
		t.Fatalf("kick without a speed = %d", out)
	}
}

func TestShapeMinRun(t *testing.T) {
	p := params()
	p.MinRun = 30
	for _, tc := range []struct {
		target   int
		allowOff bool
		want     int
	}{
		{15, false, 30}, {0, false, 30}, {0, true, 0}, {5, true, 30}, {30, false, 30}, {80, false, 80},
	} {
		p.AllowOff = tc.allowOff
		if out, _ := Shape(NewShapeState(), tc.target, p, dt2, false); out != tc.want {
			t.Errorf("target %d allow_off %v: %d, want %d", tc.target, tc.allowOff, out, tc.want)
		}
	}
}

func TestShapeRamps(t *testing.T) {
	p := params()
	p.UpStep, p.DownStep = 5, 3
	st := ShapeState{Current: 30, Held: -1}
	var got []int
	for range 4 {
		var out int
		out, st = Shape(st, 50, p, dt2, false)
		got = append(got, out)
	}
	for range 3 {
		var out int
		out, st = Shape(st, 40, p, dt2, false)
		got = append(got, out)
	}
	for range 2 {
		var out int
		out, st = Shape(st, 38, p, dt2, false)
		got = append(got, out)
	}
	if want := []int{35, 40, 45, 50, 47, 44, 41, 38, 38}; !equalInts(got, want) {
		t.Fatalf("ramps = %v, want %v", got, want)
	}
}

// An emergency is 100 % at once: no deadband, ramp, kick-start or min_run in the way, and it ends the kick.
func TestShapeEmergencyBypass(t *testing.T) {
	p := params()
	p.Deadband, p.UpStep, p.StartPct, p.StartMS, p.MinRun = 10, 1, 60, 5000, 20
	st := ShapeState{Current: 0, Held: 5, Kick: time.Second}
	out, st := Shape(st, 25, p, dt2, true)
	if out != 100 || st.Current != 100 || st.Held != 100 || st.Kick != 0 {
		t.Fatalf("emergency: %d %+v", out, st)
	}
	// afterwards the way down is the normal one
	out, _ = Shape(st, 30, params(), dt2, false)
	if out != 98 {
		t.Fatalf("after the emergency: %d", out)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
