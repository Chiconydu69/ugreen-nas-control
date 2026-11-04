package fan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

var h0 = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

func tick1(at time.Time, pct, rpm int, src string, temp float64, mods uint8) Sample {
	return Sample{At: at, CPU: ip(50), CaseTemp: ip(40), Fans: map[string]FanSample{
		"fan1": {Pct: pct, RPM: rpm, Drive: Drive{Source: src, Temp: &temp, Mods: mods}},
	}}
}

func TestHistoryAveragesOneStep(t *testing.T) {
	h := NewHistory([]string{"fan1"}, h0)
	// 5 ticks of 2 s = one 10 s step: 3 on the CPU, 2 on a disk; the CPU one wins the driver
	for i, p := range []int{40, 40, 40, 60, 60} {
		src := "cpu"
		if i >= 3 {
			src = "disk:sdb"
		}
		h.Record(tick1(h0.Add(time.Duration(i*2)*time.Second), p, 1000+p, src, 50, 0))
	}
	h.Record(tick1(h0.Add(10*time.Second), 70, 1200, "cpu", 51, 0)) // closes the step

	a := h.Answer(Range1h, h0.Add(11*time.Second))
	if len(a.T) != 2 || a.T[0] != h0.Unix() || a.T[1] != h0.Unix()+10 {
		t.Fatalf("t = %v", a.T)
	}
	s := a.Series[0]
	if *s.Pct[0] != 48 || *s.RPM[0] != 1048 || *s.Temp[0] != 50 || *s.Pct[1] != 70 {
		t.Fatalf("series %+v %v", s, *s.Pct[0])
	}
	if a.Drivers[s.Drv[0]] != "cpu" {
		t.Fatalf("driver %v %v", a.Drivers, s.Drv)
	}
	if a.CPU[0] == nil || *a.CPU[0] != 50 || *a.Case[0] != 40 {
		t.Fatalf("cpu %v case %v", a.CPU, a.Case)
	}
}

func TestHistoryEmergencyDoesNotAverageAway(t *testing.T) {
	h := NewHistory([]string{"fan1"}, h0)
	for i := range 5 {
		if i == 2 {
			h.Record(tick1(h0.Add(4*time.Second), 100, 3000, "emergency:cpu_hot", 95, ModEmergency))
			continue
		}
		h.Record(tick1(h0.Add(time.Duration(i*2)*time.Second), 30, 900, "cpu", 50, ModQuiet))
	}
	a := h.Answer(Range1h, h0.Add(10*time.Second))
	s := a.Series[0]
	if got := a.Drivers[s.Drv[0]]; got != "emergency:cpu_hot" {
		t.Fatalf("driver %q", got)
	}
	if s.Mods[0] != int(ModEmergency|ModQuiet) {
		t.Fatalf("mods %b", s.Mods[0])
	}
}

func TestHistoryRingKeepsOneHourFineAndDayCoarse(t *testing.T) {
	h := NewHistory([]string{"fan1"}, h0)
	// 26 hours of ticks, one per 2 s: rings are full, memory does not grow
	end := h0.Add(26 * time.Hour)
	for at := h0; at.Before(end); at = at.Add(2 * time.Second) {
		h.Record(tick1(at, int(at.Unix()%100), 1000, "cpu", 50, 0))
	}
	fine := h.Answer(Range1h, end)
	if len(fine.T) != fineLen+1 && len(fine.T) != fineLen {
		t.Fatalf("1h has %d points", len(fine.T))
	}
	if d := fine.T[len(fine.T)-1] - fine.T[0]; d < 3500 || d > 3610 {
		t.Fatalf("1h spans %d s", d)
	}
	day := h.Answer(Range24h, end)
	if n := len(day.T); n < coarseLen-1 || n > coarseLen+1 {
		t.Fatalf("24h has %d points", n)
	}
	if d := day.T[len(day.T)-1] - day.T[0]; d < 86400-120 || d > 86400+60 {
		t.Fatalf("24h spans %d s", d)
	}
	for i := 1; i < len(day.T); i++ {
		if day.T[i] != day.T[i-1]+coarseStep {
			t.Fatalf("gap at %d: %d -> %d", i, day.T[i-1], day.T[i])
		}
	}
	if len(h.fine.pts) != fineLen || len(h.coarse.pts) != coarseLen {
		t.Fatal("rings grew")
	}
}

func TestHistoryDriverTableIsBounded(t *testing.T) {
	h := NewHistory([]string{"fan1"}, h0)
	for i := range maxDrivers + 20 {
		h.Record(tick1(h0.Add(time.Duration(i*10)*time.Second), 40, 900, fmt.Sprintf("disk:%d", i), 40, 0))
	}
	a := h.Answer(Range1h, h0.Add(time.Hour))
	if len(a.Drivers) != maxDrivers {
		t.Fatalf("%d drivers", len(a.Drivers))
	}
	last := a.Series[0].Drv[len(a.T)-1]
	if a.Drivers[last] != "other" {
		t.Fatalf("overflow driver %q", a.Drivers[last])
	}
}

func TestHistoryUnknownValuesAreNull(t *testing.T) {
	h := NewHistory([]string{"fan1", "fan2"}, h0)
	h.Record(Sample{At: h0, Fans: map[string]FanSample{"fan1": {Pct: -1, RPM: -1, Drive: Drive{Source: SrcBIOS}}}})
	a := h.Answer(Range1h, h0)
	s := a.Series[0]
	if s.Pct[0] != nil || s.RPM[0] != nil || s.Temp[0] != nil || a.CPU[0] != nil || a.Series[1].Drv[0] != -1 {
		t.Fatalf("%+v", a)
	}
	if a.Drivers[s.Drv[0]] != SrcBIOS {
		t.Fatalf("drivers %v", a.Drivers)
	}
}

func TestHistorySocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")

	if a, err := QueryHistory(path, Range1h); err != nil || !a.OK || a.Running {
		t.Fatalf("no daemon: %+v %v", a, err)
	}

	h := NewHistory([]string{"fan1"}, h0)
	h.Record(tick1(h0, 40, 1000, "cpu", 50, 0))
	stop := serveHistory(path, h, t.Logf)
	a, err := QueryHistory(path, Range1h)
	if err != nil || !a.Running || a.Range != Range1h || len(a.T) != 1 {
		t.Fatalf("%+v %v", a, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if a, _ := QueryHistory(path, "bogus"); a.Range != Range1h {
		t.Fatalf("unknown range falls back to 1h: %q", a.Range)
	}
	stop()
	if _, err := os.Stat(path); err == nil {
		t.Fatal("socket left behind")
	}
}

func TestHistoryNoPathNoService(t *testing.T) {
	serveHistory("", NewHistory(nil, h0), t.Logf)()
}

// The drive of each fan, from the pipeline's own decisions.
func TestPipelineDrive(t *testing.T) {
	src := func(p Plan, id string) (string, float64) {
		d := p.Drive[id]
		v := -1.0
		if d.Temp != nil {
			v = *d.Temp
		}
		return d.Source, v
	}

	k := newController()
	p := stepOnce(k, cfgFrom(t, "min_pct=\"60\"\n"), sens(45, 41), mon3pm, JobStatus{})
	if s, v := src(p, "fan2"); s != SrcCPU || v != 45 {
		t.Errorf("cpu fan: %s %v", s, v)
	}
	if s, v := src(p, "fan3"); s != "disk:disk1" || v != 41 {
		t.Errorf("case fan: %s %v", s, v)
	}
	if p.Drive["fan3"].Mods&ModFloor == 0 {
		t.Errorf("a cool disk: the minimum speed holds the fan (%+v)", p.Drive["fan3"])
	}

	// all drives asleep
	if s, _ := src(stepOnce(newController(), defaultConfig(), sens(45, 0), mon3pm, JobStatus{}), "fan3"); s != SrcIdle {
		t.Errorf("asleep: %s", s)
	}

	// case fans help cool the CPU, and the CPU asks for more
	c := cfgFrom(t, "mode=\"custom\"\ncpu_curve=\"30:20,60:100\"\ncase_curve=\"30:20,60:100\"\ncase_follows_cpu=\"1\"\n")
	if s, v := src(stepOnce(newController(), c, sens(58, 35), mon3pm, JobStatus{}), "fan3"); s != SrcCPU || v != 58 {
		t.Errorf("case follows cpu: %s %v", s, v)
	}

	// emergency
	p = stepOnce(newController(), defaultConfig(), sens(99, 41), mon3pm, JobStatus{})
	if d := p.Drive["fan3"]; d.Source != "emergency:cpu_hot" || d.Mods&ModEmergency == 0 || d.Temp == nil || *d.Temp != 99 {
		t.Errorf("emergency: %+v", d)
	}

	// quiet hours cap and the mover job
	q := cfgFrom(t, v2Base+"quiet_enabled=\"1\"\nquiet_from=\"22:00\"\nquiet_to=\"06:00\"\nquiet_cap=\"30\"\n")
	p = stepOnce(newController(), q, sens(45, 50), mon3am, JobStatus{})
	if p.Drive["fan3"].Mods&ModQuiet == 0 {
		t.Errorf("quiet: %+v", p.Drive["fan3"])
	}
	if p.Drive["fan2"].Mods&ModQuiet != 0 {
		t.Errorf("quiet scope is the case fans: %+v", p.Drive["fan2"])
	}
	j := cfgFrom(t, v2Base+"job_mover=\"floor\"\njob_mover_pct=\"80\"\n")
	p = stepOnce(newController(), j, sens(45, 36), mon3pm, JobStatus{Mover: true})
	if d := p.Drive["fan3"]; d.Mods&ModMover == 0 || p.Out["fan3"] != 80 {
		t.Errorf("mover: %+v out %d", d, p.Out["fan3"])
	}

	// configured input: the part of a max: that wins, and a missing one
	g := cfgFrom(t, v2Base+"case_input=\"max:gpu|nic:eth0\"\n")
	in := fixedInputs{"gpu": 50, "nic:eth0": 61, "max:gpu|nic:eth0": 61}
	p = newController().step(context.Background(), tickIn{Cfg: g, Sens: sens(40, 0), In: in, Now: mon3pm, Dt: dt2, IDs: ids4})
	if s, v := src(p, "fan3"); s != "nic:eth0" || v != 61 {
		t.Errorf("max: %s %v", s, v)
	}
	p = newController().step(context.Background(), tickIn{Cfg: g, Sens: sens(40, 52), In: fixedInputs{}, Now: mon3pm, Dt: dt2, IDs: ids4})
	if d := p.Drive["fan3"]; d.Source != "disk:disk1" || d.Mods&ModMissing == 0 {
		t.Errorf("missing, fallback: %+v", d)
	}
}

func TestFanSampleUsesWrittenSpeedElseTheChip(t *testing.T) {
	chip := &loopChip{writes: map[string][]byte{}}
	ch := Channel{ID: "fan2"}
	d := Drive{Source: SrcCPU}

	if f := fanSample(chip, ch, 1234, true, map[string]int{"fan2": 55}, d); f.Pct != 55 || f.RPM != 1234 || f.Drive.Source != SrcCPU {
		t.Fatalf("driven: %+v", f)
	}
	// not driven (BIOS mode, rival): the chip's own duty; loopChip reports 0 and manual mode
	if f := fanSample(chip, ch, 0, false, map[string]int{}, d); f.Pct != 0 || f.RPM != -1 {
		t.Fatalf("not driven: %+v", f)
	}
}

// A panic while answering one connection must not end the daemon or the accept loop.
func TestHistorySocketSurvivesPanic(t *testing.T) {
	dir, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")

	var mu sync.Mutex
	logged := 0
	logf := func(string, ...any) { mu.Lock(); logged++; mu.Unlock() }
	stop := serveHistory(path, nil, logf) // a nil history panics in Answer
	defer stop()
	for range 3 {
		if _, err := QueryHistory(path, Range1h); err != nil {
			t.Fatalf("query: %v", err)
		}
	}
	// the client returns before the server side has recovered and logged: wait for it
	count := func() int { mu.Lock(); defer mu.Unlock(); return logged }
	for deadline := time.Now().Add(5 * time.Second); count() < 3 && time.Now().Before(deadline); {
		time.Sleep(2 * time.Millisecond)
	}
	if n := count(); n != 3 {
		t.Errorf("panics logged %d, want 3 (accept loop must keep running)", n)
	}
}
