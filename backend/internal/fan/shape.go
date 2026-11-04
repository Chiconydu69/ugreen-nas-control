package fan

import "time"

// Output shaping (F3): what happens to a curve target between the curve and the job/quiet rules. Pure: the daemon keeps
// one ShapeState per fan and feeds back the speed that was finally written.
//
// Order inside Shape: min_run raise, deadband, ramps, kick-start. An emergency skips all of it.

type ShapeParams struct {
	// Deadband: a new target is only taken when it differs from the last taken one by at least this many % (0 = always).
	Deadband int
	// StartPct and StartMS: when the fan is stopped or below MinRun and has to speed up, it first gets StartPct % for
	// StartMS. StallStart (the lowest measured turning speed from the calibration, 0 = no calibration) raises StartPct
	// to at least that and also serves as StartPct when that is 0 ("use measured"). StartMS 0 = no kick-start.
	StartPct, StartMS, StallStart int
	// MinRun: the lowest speed while running (already at least the global min_pct).
	MinRun int
	// AllowOff: a target of 0 stays 0 instead of being raised to MinRun.
	AllowOff bool
	// UpStep and DownStep: the most the speed changes per call (100 = at once).
	UpStep, DownStep int
}

type ShapeState struct {
	// Current is the speed written last; -1 = unknown (first tick, after a pause).
	Current int
	// Held is the last target the deadband took; -1 = none.
	Held int
	// Kick is the time left of a kick-start.
	Kick time.Duration
}

func NewShapeState() ShapeState { return ShapeState{Current: -1, Held: -1} }

// Shape gives the speed for this tick and the new state. dt is the time since the previous call. emergency: 100 %
// at once, untouched by deadband, ramps, kick-start and everything after it.
func Shape(st ShapeState, target int, p ShapeParams, dt time.Duration, emergency bool) (int, ShapeState) {
	if emergency {
		return 100, ShapeState{Current: 100, Held: 100}
	}

	if target > 0 || !p.AllowOff {
		target = max(target, p.MinRun)
	}
	target = min(target, 100)

	// deadband: 100 % is never held back
	if st.Held >= 0 && target < 100 && abs(target-st.Held) < p.Deadband {
		target = st.Held
	} else {
		st.Held = target
	}

	// kick-start: from a stop or from below min_run, up to the target
	kickPct := max(p.StartPct, p.StallStart)
	if st.Kick <= 0 && st.Current >= 0 && st.Current < p.MinRun && target > st.Current && p.StartMS > 0 && kickPct > 0 && target > 0 {
		st.Kick = time.Duration(p.StartMS) * time.Millisecond
	}
	if st.Kick > 0 {
		st.Kick -= dt
		out := max(kickPct, target)
		st.Current = out
		return out, st
	}

	out := target
	if st.Current >= 0 {
		switch {
		case target > st.Current && target-st.Current > p.UpStep:
			out = st.Current + p.UpStep
		case target < st.Current && st.Current-target > p.DownStep:
			out = st.Current - p.DownStep
		}
	}
	st.Current = out
	return out, st
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
