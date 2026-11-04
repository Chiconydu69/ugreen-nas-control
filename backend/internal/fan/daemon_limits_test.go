package fan

import (
	"testing"
)

// The iDX Pro limits (CPU 88, disk 60) cap a lenient user config; other models keep the user's values.
func TestApplyModelLimitsCapsLenientConfig(t *testing.T) {
	lenient := cfgFrom(t, v2Base+"cpu_crit=\"100\"\ndisk_crit=\"70\"\n")
	if lenient.CPUCrit != 100 || lenient.DiskCrit != 70 {
		t.Fatalf("setup: %d %d", lenient.CPUCrit, lenient.DiskCrit)
	}
	if c := ApplyModelLimits(lenient, "iDX6011 Pro"); c.CPUCrit != 88 || c.DiskCrit != 60 {
		t.Fatalf("iDX Pro: cpu %d disk %d", c.CPUCrit, c.DiskCrit)
	}
	if c := ApplyModelLimits(lenient, "DXP4800 Plus"); c.CPUCrit != 100 || c.DiskCrit != 70 {
		t.Fatalf("other model changed: %+v", c)
	}
	// The stricter value wins in both directions.
	strict := cfgFrom(t, v2Base+"cpu_crit=\"75\"\ndisk_crit=\"50\"\n")
	if c := ApplyModelLimits(strict, "iDX6011 Pro"); c.CPUCrit != 75 || c.DiskCrit != 50 {
		t.Fatalf("user's stricter values lost: %+v", c)
	}
}

// In the daemon loop: 90 degrees CPU is fine for a user limit of 100, but an emergency on the iDX Pro.
func TestDaemonAppliesModelLimits(t *testing.T) {
	cfg := v2Base + "cpu_crit=\"100\"\n"
	st, _ := runLoopModel(t, cfg, "iDX6011 Pro", sens(90, 40))
	if st.Failsafe != "cpu_hot" || st.Targets["fan2"] != 100 {
		t.Fatalf("iDX Pro: %+v", st)
	}
	st, _ = runLoopModel(t, cfg, "DXP6800 Pro", sens(90, 40))
	if st.Failsafe != "" {
		t.Fatalf("other model: %+v", st)
	}
}

func runLoopModel(t *testing.T, cfg, model string, s Sensors) (State, *loopChip) {
	t.Helper()
	old := daemonModel
	daemonModel = func() string { return model }
	t.Cleanup(func() { daemonModel = old })
	return runLoop(t, cfg, mon3pm, nil, false, s)
}

const allowOffCurves = v2Base + "mode=\"custom\"\ncpu_curve=\"30:0,60:50,90:100\"\ncase_curve=\"30:0,50:60\"\nmin_pct=\"10\"\n"

// allow_off lets a group reach 0 when its curve says 0; without it the minimum holds.
func TestAllowOffReachesZeroOnlyWhenCurveSaysZero(t *testing.T) {
	cold := sens(25, 25)
	for _, tc := range []struct {
		name string
		cfg  string
		s    Sensors
		job  JobStatus
		want map[string]int // fan2 cpu, fan3 case
	}{
		{"off by default", allowOffCurves, cold, JobStatus{}, map[string]int{"fan2": 10, "fan3": 10}},
		{"on, curve says 0", allowOffCurves + "cpu_allow_off=\"1\"\ncase_allow_off=\"1\"\n", cold, JobStatus{}, map[string]int{"fan2": 0, "fan3": 0}},
		{"on, curve above 0", allowOffCurves + "cpu_allow_off=\"1\"\ncase_allow_off=\"1\"\n", sens(60, 45), JobStatus{}, map[string]int{"fan2": 50, "fan3": 45}},
		{"one channel only", allowOffCurves + "fan.fan3.allow_off=\"1\"\n", cold, JobStatus{}, map[string]int{"fan2": 10, "fan3": 0}},
		{"emergency wins", allowOffCurves + "cpu_allow_off=\"1\"\ncase_allow_off=\"1\"\n", sens(95, 25), JobStatus{}, map[string]int{"fan2": 100, "fan3": 100}},
		{"job floor wins", allowOffCurves + "case_allow_off=\"1\"\njob_mover=\"floor\"\njob_mover_pct=\"40\"\n", cold, JobStatus{Mover: true}, map[string]int{"fan3": 40}},
		{"global minimum applies to follow-cpu", allowOffCurves + "case_allow_off=\"1\"\ncase_follows_cpu=\"1\"\n", sens(60, 25), JobStatus{}, map[string]int{"fan3": 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cfgFrom(t, tc.cfg)
			p := stepOnce(newController(), c, tc.s, mon3pm, tc.job)
			for id, want := range tc.want {
				if p.Out[id] != want {
					t.Errorf("%s = %d, want %d (%+v)", id, p.Out[id], want, p.Out)
				}
			}
		})
	}
}

// The DXP2800 GT writes nothing below 25 %, so a lower minimum speed is raised to what runs.
func TestApplyModelLimitsRaisesMinPctToTheHardwareFloor(t *testing.T) {
	low := cfgFrom(t, v2Base+"min_pct=\"10\"\n")
	if c := ApplyModelLimits(low, "DXP2800 GT"); c.MinPct != csFloorPct {
		t.Fatalf("DXP2800 GT: min %d", c.MinPct)
	}
	if c := ApplyModelLimits(low, "DXP4800 Plus"); c.MinPct != 10 {
		t.Fatalf("other model: min %d", c.MinPct)
	}
	if c := ApplyModelLimits(cfgFrom(t, v2Base+"min_pct=\"40\"\n"), "DXP2800 GT"); c.MinPct != 40 {
		t.Fatalf("higher user value lost: %d", c.MinPct)
	}
}
