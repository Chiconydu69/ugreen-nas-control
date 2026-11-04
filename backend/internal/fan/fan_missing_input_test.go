package fan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// inputWorld is one input kind the test can make appear and disappear: setup puts the reading in the sensors or on disk.
type inputWorld struct {
	spec  string
	apply func(t *testing.T, present bool, s *Sensors)
}

func missingInputWorlds(t *testing.T) []inputWorld {
	hot := 44   // 50 % on the case curve; hot enough to see, below every limit
	disk1 := 52 // the default input: 88 %
	scripts := useScriptDir(t)
	hwmon := t.TempDir()
	old := hwmonDir
	hwmonDir = hwmon
	t.Cleanup(func() { hwmonDir = old })
	absentStorcli(t)
	oldSMI := nvidiaSMIPaths
	nvidiaSMIPaths = []string{filepath.Join(t.TempDir(), "none")}
	t.Cleanup(func() { nvidiaSMIPaths = oldSMI })

	return []inputWorld{
		{"nvme", func(_ *testing.T, p bool, s *Sensors) {
			if p {
				s.NVMe = []Temp{{Name: "nvme0", Temp: &hot}}
			}
		}},
		{"disk_max:disk2", func(_ *testing.T, p bool, s *Sensors) {
			if !p {
				s.Disks = []Disk{{Name: "disk1", Temp: &disk1}, {Name: "disk2"}} // listed, no temperature yet
			} else {
				s.Disks = []Disk{{Name: "disk1", Temp: &disk1}, {Name: "disk2", Temp: &hot}}
			}
		}},
		{"disk_avg:disk2", func(_ *testing.T, p bool, s *Sensors) {
			if !p {
				s.Disks = []Disk{{Name: "disk1", Temp: &disk1}}
			} else {
				s.Disks = []Disk{{Name: "disk1", Temp: &disk1}, {Name: "disk2", Temp: &hot}}
			}
		}},
		{"nic:eth9", func(_ *testing.T, p bool, s *Sensors) {
			if p {
				s.NIC = []NICTemp{{Iface: "eth9", Temp: &hot}}
			}
		}},
		{"gpu", func(_ *testing.T, p bool, s *Sensors) {
			if p {
				s.GPU = &hot
			}
		}},
		{"hba", func(t *testing.T, p bool, _ *Sensors) {
			h := filepath.Join(hwmon, "hwmon0")
			_ = os.RemoveAll(h)
			if p {
				if err := os.MkdirAll(h, 0o755); err != nil {
					t.Fatal(err)
				}
				_ = os.WriteFile(filepath.Join(h, "name"), []byte("mpt3sas\n"), 0o644)
				_ = os.WriteFile(filepath.Join(h, "temp1_input"), []byte("44000\n"), 0o644)
			}
		}},
		{"script:probe", func(t *testing.T, p bool, _ *Sensors) {
			_ = os.Remove(filepath.Join(scripts, "probe"))
			if p {
				writeScript(t, scripts, "probe", "#!/bin/sh\necho 44\n", 0o755)
			}
		}},
	}
}

// A configured input that gives no reading at daemon start, later, or again after a good reading: the group follows
// its on_missing rule at once, the input is reported, and it takes over again when it returns. Defaults (fallback and
// hold without history) end on the default input (disk 52 C = 88 %), never on the idle point; only curve_low, which
// the user asks for by name, goes to the curve's low point (25 %).
func TestMissingInputPerKindStartLaterRecover(t *testing.T) {
	for _, rule := range []struct {
		name  string
		start int // first tick, nothing held yet
		later int // lowest acceptable output when the input drops out after a 100 % reading
	}{
		{"fallback", 88, 88},
		{"hold", 88, 50},
		{"curve_low", 25, 25},
	} {
		t.Run(rule.name, func(t *testing.T) {
			for _, w := range missingInputWorlds(t) {
				t.Run(w.spec, func(t *testing.T) {
					c := cfgFrom(t, v2Base+"case_input=\""+w.spec+"\"\ncase_on_missing=\""+rule.name+"\"\ncase_down_step=\"100\"\n")
					clock := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
					var s Sensors
					reg := newRegistry(clock.now, func() Sensors { return s }, nil)
					k := newController()
					spec := mustSpec(t, w.spec)

					tickWith := func(present bool) Plan {
						cpu, disk := 40, 52 // the default input says 52 C = 88 %
						s = Sensors{CPU: &cpu, Disks: []Disk{{Name: "disk1", Temp: &disk}}}
						w.apply(t, present, &s)
						clock.advance(11 * time.Second) // past slowTTL: scripts and HBA are asked again
						reg.begin(c, s)
						return k.step(context.Background(), tickIn{Cfg: c, Sens: s, In: reg, Now: mon3pm, Dt: dt2, IDs: ids4})
					}
					missingOK := func(step string, p Plan, wantMissing bool) {
						t.Helper()
						got := len(p.Missing) == 1 && p.Missing[0] == spec.String()
						if got != wantMissing || (!wantMissing && len(p.Missing) != 0) {
							t.Errorf("%s: missing = %v, want %v", step, p.Missing, wantMissing)
						}
						if p.Failsafe != "" {
							t.Errorf("%s: an input must not raise an emergency: %s", step, p.Failsafe)
						}
					}

					p := tickWith(false)
					missingOK("start", p, true)
					if p.Out["fan3"] != rule.start {
						t.Errorf("missing at start: %d %%, want %d", p.Out["fan3"], rule.start)
					}
					if p.Out["fan3"] <= 25 && rule.name != "curve_low" {
						t.Errorf("missing at start fell to idle: %d %%", p.Out["fan3"])
					}
					p = tickWith(true)
					missingOK("present", p, false)
					if p.Out["fan3"] != 50 {
						t.Errorf("input present: %d %%, want 50", p.Out["fan3"])
					}
					p = tickWith(false)
					missingOK("later", p, true)
					if p.Out["fan3"] < rule.later {
						t.Errorf("input lost later: %d %%, want at least %d", p.Out["fan3"], rule.later)
					}
					p = tickWith(true)
					missingOK("recovered", p, false)
					if p.Out["fan3"] != 50 {
						t.Errorf("input back: %d %%, want 50", p.Out["fan3"])
					}
				})
			}
		})
	}
}

// The CPU is the one input whose loss is an emergency: all fans 100 %, whatever the group's input is.
func TestCPUUnreadableAtStartIsEmergencyForEveryInput(t *testing.T) {
	disk := 30
	s := Sensors{Disks: []Disk{{Name: "disk1", Temp: &disk}}}
	for _, in := range []string{"cpu", "gpu", "script:x", "hba"} {
		c := cfgFrom(t, v2Base+"case_input=\""+in+"\"\ncpu_input=\""+in+"\"\ncase_on_missing=\"curve_low\"\n")
		p := newController().step(context.Background(), tickIn{Cfg: c, Sens: s, In: fixedInputs{}, Now: mon3pm, Dt: dt2, IDs: ids4})
		if p.Failsafe != "cpu_unreadable" {
			t.Fatalf("%s: failsafe %q", in, p.Failsafe)
		}
		for id, out := range p.Out {
			if out != 100 {
				t.Errorf("%s: %s at %d %%", in, id, out)
			}
		}
	}
}

// A change in the set of unreadable inputs is logged and notified once, not on every tick, and the return is logged.
func TestReportMissingInputsOncePerChange(t *testing.T) {
	var logs []string
	logf := func(f string, _ ...any) { logs = append(logs, f) }
	last := ""
	for _, m := range [][]string{{"gpu"}, {"gpu"}, {"gpu"}, {"gpu", "hba"}, {"gpu", "hba"}, nil, nil} {
		last = reportMissingInputs(last, m, logf)
	}
	notifications.wg.Wait()
	if len(logs) != 3 || last != "" {
		t.Fatalf("logs %v last %q", logs, last)
	}
	if !strings.Contains(logs[2], "readable again") {
		t.Errorf("no recovery line: %v", logs)
	}
}

// Nothing the daemons write on a timer or per tick may land on the flash (/boot): history, state, BIOS values,
// watchdog status, calibration/probe progress and hardware caches default to /var/run (the LED daemon too: ledctl/env.go).
// /boot is written only by commands (save settings, calibrate result, fan map, power_ac, LED enable/import, one-time
// CS201x LED snapshot).
func TestPeriodicWritesStayOffTheFlash(t *testing.T) {
	for name, p := range map[string]string{
		"state": statePath, "bios": biosPath, "history": historyPath, "watchdog": watchdogStatePath,
		"watchdog marker": iteWdtMarkerPath, "calibration progress": calibStatePath, "calibration pause": calibPausePath,
		"probe progress": probeStatePath, "hw cache": cacheDir, "input test stamp": inputTestDir,
	} {
		if strings.HasPrefix(p, "/boot") {
			t.Errorf("%s is under /boot: %s", name, p)
		}
	}
}
