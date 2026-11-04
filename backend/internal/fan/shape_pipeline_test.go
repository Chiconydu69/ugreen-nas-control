package fan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cfgFrom(t *testing.T, lines string) Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fan.cfg")
	if err := os.WriteFile(p, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	c := LoadConfig(p)
	if len(c.Warnings) > 0 {
		t.Fatalf("warnings: %v", c.Warnings)
	}
	return c
}

func sens(cpu, disk int) Sensors {
	s := Sensors{CPU: &cpu}
	if disk > 0 {
		s.Disks = []Disk{{Name: "disk1", Temp: &disk}}
	}
	return s
}

var (
	ids4   = []string{"fan1", "fan2", "fan3", "fan4"}
	mon3am = time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC) // Monday
	mon3pm = time.Date(2026, 10, 26, 15, 0, 0, 0, time.UTC)
)

func stepOnce(k *controller, c Config, s Sensors, now time.Time, jobs JobStatus) Plan {
	return k.step(context.Background(), tickIn{Cfg: c, Sens: s, In: newInputSource(c, s), Now: now, Dt: dt2, Jobs: jobs, IDs: ids4})
}

// With the default (v1) configuration the pipeline gives Decide and Smooth, tick after tick.
func TestPipelineDefaultsEqualDecideAndSmooth(t *testing.T) {
	for _, c := range []Config{
		defaultConfig(),
		cfgFrom(t, "mode=\"custom\"\ncpu_curve=\"30:20,60:50,90:100\"\ncase_curve=\"30:30,50:100\"\ncase_follows_cpu=\"1\"\nmin_pct=\"20\"\n"),
		cfgFrom(t, "mode=\"quiet\"\ncfg_version=\"2\"\n"),
	} {
		k := newController()
		cur := map[string]int{}
		for cpu := 100; cpu >= 25; cpu -= 2 {
			for _, disk := range []int{0, 30, 46, 52, 58, 41} {
				s := sens(cpu, disk)
				d := Decide(c, s)
				p := stepOnce(k, c, s, mon3am, JobStatus{})
				for _, id := range ids4 {
					target := d.Case
					if roles[id] == "cpu" {
						target = d.CPU
					}
					c0, ok := cur[id]
					if !ok || d.Failsafe != "" {
						c0 = -1
					}
					want := Smooth(c0, target)
					cur[id] = want
					if p.Out[id] != want {
						t.Fatalf("cpu %d disk %d %s: %d, want %d (%+v)", cpu, disk, id, p.Out[id], want, d)
					}
				}
				if p.QuietActive || p.Job != "" || p.Cap != nil {
					t.Fatalf("schedule active by default: %+v", p)
				}
			}
		}
	}
}

const v2Base = "cfg_version=\"2\"\nmode=\"standard\"\nmin_pct=\"25\"\n"

// emergency > job floor > quiet cap, and the cap never below min_run.
func TestPrecedenceMatrix(t *testing.T) {
	both := v2Base + "quiet_enabled=\"1\"\nquiet_from=\"22:00\"\nquiet_to=\"06:00\"\nquiet_cap=\"30\"\njob_mover=\"floor\"\njob_mover_pct=\"60\"\n"
	cpuOnly := v2Base + "quiet_enabled=\"1\"\nquiet_from=\"22:00\"\nquiet_to=\"06:00\"\nquiet_cap=\"30\"\nquiet_scope=\"all\"\n"
	lowCap := v2Base + "quiet_enabled=\"1\"\nquiet_from=\"22:00\"\nquiet_to=\"06:00\"\nquiet_cap=\"10\"\nquiet_scope=\"all\"\n"
	mover := JobStatus{Mover: true}
	hotDisk := 40 // case target 37 %
	for _, tc := range []struct {
		name     string
		cfg      string
		s        Sensors
		now      time.Time
		jobs     JobStatus
		cpu, cas int // fan2 / fan3
		fs       string
	}{
		{"neither", both, sens(45, hotDisk), mon3pm, JobStatus{}, 25 + 0, 37, ""},
		{"quiet only", both, sens(45, hotDisk), mon3am, JobStatus{}, 25, 30, ""},
		{"job only", both, sens(45, hotDisk), mon3pm, mover, 25, 60, ""},
		{"floor beats cap", both, sens(45, hotDisk), mon3am, mover, 25, 60, ""},
		{"cap leaves cpu alone (scope case)", both, sens(80, hotDisk), mon3am, JobStatus{}, 80, 30, ""},
		{"scope all caps cpu", cpuOnly, sens(80, hotDisk), mon3am, JobStatus{}, 30, 30, ""},
		{"cap never below min_run", lowCap, sens(80, hotDisk), mon3am, JobStatus{}, 25, 25, ""},
		{"emergency in quiet hours", both, sens(95, hotDisk), mon3am, JobStatus{}, 100, 100, "cpu_hot"},
		{"emergency with a job", both, sens(45, 60), mon3am, mover, 100, 100, "disk_hot"},
		{"unreadable cpu in quiet hours", both, Sensors{}, mon3am, mover, 100, 100, "cpu_unreadable"},
	} {
		k := newController()
		p := stepOnce(k, cfgFrom(t, tc.cfg), tc.s, tc.now, tc.jobs)
		if p.Out["fan2"] != tc.cpu || p.Out["fan3"] != tc.cas || p.Failsafe != tc.fs {
			t.Errorf("%s: fan2 %d fan3 %d fs %q, want %d %d %q", tc.name, p.Out["fan2"], p.Out["fan3"], p.Failsafe, tc.cpu, tc.cas, tc.fs)
		}
		if tc.fs != "" && (p.QuietActive || p.Job != "" || p.Cap != nil) {
			t.Errorf("%s: schedule reported during an emergency: %+v", tc.name, p)
		}
	}
}

func TestPipelineStateFields(t *testing.T) {
	cfg := v2Base + "quiet_enabled=\"1\"\nquiet_from=\"22:00\"\nquiet_to=\"06:00\"\nquiet_cap=\"30\"\njob_parity=\"boost\"\njob_parity_pct=\"15\"\n"
	c := cfgFrom(t, cfg)
	k := newController()
	p := stepOnce(k, c, sens(50, 40), mon3am, JobStatus{Parity: true})
	if !p.QuietActive || p.Job != "parity" || p.Cap == nil || *p.Cap != 30 {
		t.Fatalf("%+v", p)
	}
	// boost 37+15 = 52, cap 30 lifted by 15
	if p.Out["fan3"] != 45 {
		t.Fatalf("boosted and capped: %d", p.Out["fan3"])
	}
	p = stepOnce(k, c, sens(50, 40), mon3pm, JobStatus{})
	if p.QuietActive || p.Job != "" || p.Cap != nil {
		t.Fatalf("%+v", p)
	}
}

// After a job or the quiet window the speed ramps down from what was written, not from the shaped value.
func TestRampsStartFromWrittenSpeed(t *testing.T) {
	c := cfgFrom(t, v2Base+"job_mover=\"floor\"\njob_mover_pct=\"60\"\n")
	k := newController()
	stepOnce(k, c, sens(50, 40), mon3pm, JobStatus{Mover: true})
	p := stepOnce(k, c, sens(50, 40), mon3pm, JobStatus{})
	if p.Out["fan3"] != 58 {
		t.Fatalf("after the job: %d, want 58", p.Out["fan3"])
	}
}

func TestMultiTickKickAndDeadband(t *testing.T) {
	c := cfgFrom(t, v2Base+"case_start_pct=\"70\"\ncase_start_ms=\"4000\"\ncase_deadband=\"4\"\ncase_min_run=\"25\"\n")
	k := newController()
	var outs []int
	for _, disk := range []int{30, 30, 40, 41, 39, 43, 47} {
		p := stepOnce(k, c, sens(40, disk), mon3pm, JobStatus{})
		outs = append(outs, p.Out["fan3"])
	}
	// 25 (curve low); disk 40 = 37 % moves; 41 (40 %) is inside the band of 4; 39 (33 %) leaves it, ramping down 2; 43 (47 %) and 47 (65 %) move
	want := []int{25, 25, 37, 37, 35, 47, 65}
	if !equalInts(outs, want) {
		t.Fatalf("outs %v, want %v", outs, want)
	}
	// a raised min_run kicks the fan that now runs below it
	k = newController()
	stepOnce(k, c, sens(40, 30), mon3pm, JobStatus{})
	c = cfgFrom(t, v2Base+"case_start_pct=\"70\"\ncase_start_ms=\"4000\"\ncase_min_run=\"40\"\n")
	p := stepOnce(k, c, sens(40, 30), mon3pm, JobStatus{})
	if p.Out["fan3"] != 70 {
		t.Fatalf("no kick-start when min_run is raised: %d", p.Out["fan3"])
	}
}

type fixedInputs map[string]float64

func (f fixedInputs) Read(_ context.Context, spec InputSpec) (float64, bool) {
	v, ok := f[spec.String()]
	return v, ok
}

func TestMissingInputRules(t *testing.T) {
	run := func(rule string, in InputSource, k *controller) Plan {
		c := cfgFrom(t, v2Base+"case_input=\"gpu\"\ncase_on_missing=\""+rule+"\"\n")
		s := sens(40, 52) // the default input says 52 °C = 88 %
		return k.step(context.Background(), tickIn{Cfg: c, Sens: s, In: in, Now: mon3pm, Dt: dt2, IDs: ids4})
	}
	hot := fixedInputs{"gpu": 70}
	if p := run("fallback", hot, newController()); p.Out["fan3"] != 100 || len(p.Missing) != 0 {
		t.Fatalf("readable input: %+v", p)
	}
	for rule, want := range map[string]int{"fallback": 88, "curve_low": 25} {
		p := run(rule, fixedInputs{}, newController())
		if p.Out["fan3"] != want || len(p.Missing) != 1 || p.Missing[0] != "gpu" {
			t.Errorf("%s: %+v, want %d", rule, p, want)
		}
	}
	// hold keeps the last target; with none yet it is the curve's low point
	k := newController()
	if p := run("hold", fixedInputs{"gpu": 44}, k); p.Out["fan3"] != 50 {
		t.Fatalf("hold, readable: %d", p.Out["fan3"])
	}
	if p := run("hold", fixedInputs{}, k); p.Out["fan3"] != 50 {
		t.Errorf("hold: %d, want 50", p.Out["fan3"])
	}
	if p := run("hold", fixedInputs{}, newController()); p.Out["fan3"] != 25 {
		t.Errorf("hold without history: %d", p.Out["fan3"])
	}
}

// A configured input never reaches the emergency: it only sets the curve target.
func TestInputNeverTriggersEmergency(t *testing.T) {
	c := cfgFrom(t, v2Base+"case_input=\"gpu\"\n")
	p := stepOnce(newController(), c, sens(40, 0), mon3pm, JobStatus{})
	_ = p
	p = newController().step(context.Background(), tickIn{Cfg: c, Sens: sens(40, 0), In: fixedInputs{"gpu": 120}, Now: mon3pm, Dt: dt2, IDs: ids4})
	if p.Failsafe != "" || p.Out["fan3"] != 100 {
		t.Fatalf("%+v", p)
	}
	// 100 % from the curve, not an emergency
}

func TestSensorSourceMax(t *testing.T) {
	a, b := 40, 50
	s := Sensors{CPU: &a, Disks: []Disk{{Name: "disk1", Temp: &a}, {Name: "disk2", Temp: &b}, {Name: "disk3"}}}
	src := sensorSource{defaultConfig(), s}
	for spec, want := range map[string]float64{"cpu": 40, "disk_max": 50, "disk_avg": 45, "disk_avg:disk1": 40, "max:cpu|disk_max": 50} {
		in, err := ParseInputSpec(spec)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := src.Read(context.Background(), in); !ok || got != want {
			t.Errorf("%s = %v %v, want %v", spec, got, ok, want)
		}
	}
	for _, spec := range []string{"nvme", "gpu", "nic:eth0", "script:x", "disk_max:nope"} {
		in, _ := ParseInputSpec(spec)
		if _, ok := src.Read(context.Background(), in); ok {
			t.Errorf("%s readable", spec)
		}
	}
}
