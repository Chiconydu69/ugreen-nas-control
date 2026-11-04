package fan

import (
	"testing"
	"time"
)

func at(t *testing.T, zone, ts string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	tm, err := time.ParseInLocation("2006-01-02 15:04", ts, loc)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func quiet(from, to string, days ...string) Quiet {
	q := Quiet{Enabled: true, Cap: 40, Scope: "case"}
	_ = setClock(&q.From, from)
	_ = setClock(&q.To, to)
	if len(days) == 0 {
		days = dayNames[:]
	}
	for _, d := range days {
		for i, n := range dayNames {
			if n == d {
				q.Days[i] = true
			}
		}
	}
	return q
}

func TestQuietWindow(t *testing.T) {
	cases := []struct {
		name string
		q    Quiet
		time string
		want bool
	}{
		{"inside, same day", quiet("09:00", "17:00"), "2026-10-26 12:00", true},
		{"start is in", quiet("09:00", "17:00"), "2026-10-26 09:00", true},
		{"end is out", quiet("09:00", "17:00"), "2026-10-26 17:00", false},
		{"before", quiet("09:00", "17:00"), "2026-10-26 08:59", false},
		{"across midnight, evening", quiet("23:00", "07:00"), "2026-10-26 23:30", true},
		{"across midnight, morning", quiet("23:00", "07:00"), "2026-10-27 06:59", true},
		{"across midnight, end is out", quiet("23:00", "07:00"), "2026-10-27 07:00", false},
		{"across midnight, day", quiet("23:00", "07:00"), "2026-10-27 12:00", false},
		{"disabled", Quiet{From: 0, To: 600}, "2026-10-26 03:00", false},
		{"weekday not listed", quiet("09:00", "17:00", "sat"), "2026-10-26 12:00", false},
		{"weekday listed", quiet("09:00", "17:00", "mon"), "2026-10-26 12:00", true},
		// the day is the one the window starts on
		{"fri night, fri evening", quiet("23:00", "07:00", "fri"), "2026-10-30 23:30", true},
		{"fri night, sat morning", quiet("23:00", "07:00", "fri"), "2026-10-31 03:00", true},
		{"fri night, fri morning", quiet("23:00", "07:00", "fri"), "2026-10-30 03:00", false},
		{"fri night, sat evening", quiet("23:00", "07:00", "fri"), "2026-10-31 23:30", false},
		{"sun night wraps to mon", quiet("23:00", "07:00", "sun"), "2026-10-26 03:00", true},
		{"mon morning, only mon", quiet("23:00", "07:00", "mon"), "2026-10-26 03:00", false},
		{"from = to", quiet("07:00", "07:00"), "2026-10-26 07:00", false},
	}
	for _, tc := range cases {
		if got := QuietActive(tc.q, at(t, "Europe/Berlin", tc.time)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The window follows the wall clock: on the days the clock jumps it neither opens late nor closes early.
func TestQuietDST(t *testing.T) {
	night := quiet("23:00", "07:00")
	// 2026-03-29: 02:00 -> 03:00 (23 h day); 2026-10-25: 03:00 -> 02:00 (25 h day)
	for _, tc := range []struct {
		time string
		want bool
	}{
		{"2026-03-29 01:59", true}, {"2026-03-29 03:00", true}, {"2026-03-29 06:59", true}, {"2026-03-29 07:00", false},
		{"2026-10-25 01:30", true}, {"2026-10-25 06:59", true}, {"2026-10-25 07:00", false},
	} {
		if got := QuietActive(night, at(t, "Europe/Berlin", tc.time)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.time, got, tc.want)
		}
	}
	// the repeated hour of the fall back is quiet in both passes
	first := at(t, "Europe/Berlin", "2026-10-25 00:30").Add(2*time.Hour + 30*time.Minute) // 02:00 CEST
	second := first.Add(time.Hour)                                                        // 02:00 CET
	for _, tm := range []time.Time{first, second} {
		if !QuietActive(night, tm) {
			t.Errorf("%s not quiet", tm)
		}
	}
	// a window inside the jumped hour still opens
	if !QuietActive(quiet("02:30", "04:00"), at(t, "Europe/Berlin", "2026-03-29 03:00")) {
		t.Error("window 02:30-04:00 closed on the spring-forward day")
	}
}

func TestApplyQuietNeverBelowMinRun(t *testing.T) {
	off := Job{Mode: "off"}
	for _, tc := range []struct{ out, capPct, minRun, want int }{
		{80, 40, 25, 40}, {80, 20, 25, 25}, {30, 40, 25, 30}, {25, 10, 25, 25}, {100, 40, 35, 40},
	} {
		if got := ApplyQuiet(tc.out, tc.capPct, tc.minRun, off); got != tc.want {
			t.Errorf("%+v: %d", tc, got)
		}
	}
	// a job floor holds against the cap, a boost lifts it
	if got := ApplyQuiet(80, 30, 25, Job{"floor", 60}); got != 60 {
		t.Errorf("floor vs cap: %d", got)
	}
	if got := ApplyQuiet(80, 30, 25, Job{"floor", 20}); got != 30 {
		t.Errorf("low floor vs cap: %d", got)
	}
	if got := ApplyQuiet(80, 30, 25, Job{"boost", 15}); got != 45 {
		t.Errorf("boost vs cap: %d", got)
	}
}

func TestActiveJob(t *testing.T) {
	m, p := Job{"floor", 50}, Job{"boost", 20}
	for _, tc := range []struct {
		run  JobStatus
		want string
	}{
		{JobStatus{}, ""}, {JobStatus{Mover: true}, "mover"}, {JobStatus{Parity: true}, "parity"},
		{JobStatus{true, true}, "mover"}, // higher pct wins
	} {
		if got, _ := ActiveJob(m, p, tc.run); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.run, got, tc.want)
		}
	}
	if got, _ := ActiveJob(Job{"off", 50}, p, JobStatus{Mover: true}); got != "" {
		t.Errorf("job set to off still counts: %q", got)
	}
	if got, _ := ActiveJob(m, p, JobStatus{true, true}); got != "mover" {
		t.Error(got)
	}
	if got, _ := ActiveJob(Job{"floor", 20}, p, JobStatus{true, true}); got != "parity" {
		t.Errorf("tie or lower: %q", got)
	}
}
