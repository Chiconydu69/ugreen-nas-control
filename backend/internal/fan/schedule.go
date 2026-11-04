package fan

import "time"

// Schedule (F4): quiet hours and the job rules. Pure functions; the daemon passes the local time in.

// QuietActive reports whether the quiet window covers t (local wall-clock time, so a DST change moves nothing: a
// window of 23:00 to 07:00 is 23:00 to 07:00 by the clock on the wall, whatever its length that night).
// Days name the day the window starts on: with 23:00-07:00 and only "fri", Saturday 03:00 is quiet, Friday 03:00 is not.
func QuietActive(q Quiet, t time.Time) bool {
	if !q.Enabled || q.From == q.To {
		return false
	}
	m := t.Hour()*60 + t.Minute()
	day := (int(t.Weekday()) + 6) % 7 // Monday = 0
	if q.From < q.To {
		return q.Days[day] && m >= q.From && m < q.To
	}
	if m >= q.From {
		return q.Days[day]
	}
	return m < q.To && q.Days[(day+6)%7]
}

// ActiveJob is the job that raises the case fans now: the one with the higher pct (parity on a tie), "" for none.
func ActiveJob(mover, parity Job, running JobStatus) (name string, job Job) {
	if running.Parity && parity.Mode != "off" {
		name, job = "parity", parity
	}
	if running.Mover && mover.Mode != "off" && (name == "" || mover.Pct > job.Pct) {
		name, job = "mover", mover
	}
	return name, job
}

func ApplyJob(out int, j Job) int {
	switch j.Mode {
	case "floor":
		return max(out, j.Pct)
	case "boost":
		return min(out+j.Pct, 100)
	}
	return out
}

// ApplyQuiet caps a fan's speed. The cap never goes below the fan's min_run, and not below what an active job
// demands: a floor holds against the cap (the job wins), a boost lifts the cap by its points.
func ApplyQuiet(out, capPct, minRun int, j Job) int {
	bound := max(capPct, minRun)
	switch j.Mode {
	case "floor":
		bound = max(bound, j.Pct)
	case "boost":
		bound = min(bound+j.Pct, 100)
	}
	return min(out, bound)
}
