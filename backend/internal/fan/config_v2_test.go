package fan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fan.cfg")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// A v1 file gets groups that repeat the globals, so the v2 pipeline (shape, inputs, schedule) starts out neutral.
func TestV1GroupDefaultsAreTheGlobals(t *testing.T) {
	files, _ := filepath.Glob("testdata/v1/*.cfg")
	for _, f := range files {
		c := LoadConfig(f)
		if c.Version != 1 {
			t.Errorf("%s: version %d", f, c.Version)
		}
		for role, curve := range map[string]Curve{"cpu": c.CPUCurve, "case": c.CaseCurve} {
			g := c.Group(role)
			want := Group{Curve: curve, MinRun: c.MinPct, UpStep: 100, DownStep: stepDown, OnMissing: "fallback"}
			if !reflect.DeepEqual(g, want) {
				t.Errorf("%s %s: %+v, want %+v", f, role, g, want)
			}
		}
		for id := range roles {
			if !reflect.DeepEqual(c.ForChannel(id), c.Group(roles[id])) {
				t.Errorf("%s %s: channel differs from its group", f, id)
			}
		}
		if c.Quiet.Enabled || c.Mover.Mode != "off" || c.Parity.Mode != "off" || len(c.Channels) != 0 {
			t.Errorf("%s: schedule or jobs on: %+v", f, c)
		}
		// The neutral steps are Smooth's: up at once, down by stepDown.
		g := c.Case
		for _, cur := range []int{-1, 10, 50, 100} {
			for _, target := range []int{10, 30, 49, 50, 100} {
				want := Smooth(cur, target)
				got := target
				switch {
				case cur < 0 || target >= cur:
					got = min(target, cur+g.UpStep)
					if cur < 0 {
						got = target
					}
				case cur-target > g.DownStep:
					got = cur - g.DownStep
				}
				if got != want {
					t.Errorf("Smooth(%d,%d) = %d, steps give %d", cur, target, want, got)
				}
			}
		}
	}
}

// v2 keys in a file without cfg_version are ignored, and loading never writes the file.
func TestV1IgnoresV2KeysAndIsNotRewritten(t *testing.T) {
	content := "mode=\"standard\"\nquiet_enabled=\"1\"\ncase_deadband=\"5\"\nfan.fan1.deadband=\"4\"\ncpu_input=\"gpu\"\n"
	path := writeCfg(t, content)
	before, _ := os.Stat(path)
	c := LoadConfig(path)
	if c.Version != 1 || c.Quiet.Enabled || c.Case.Deadband != 0 || c.CPU.Input.Kind != "" || len(c.Channels) != 0 || len(c.Warnings) != 0 {
		t.Fatalf("v1 read v2 keys: %+v", c)
	}
	after, _ := os.Stat(path)
	b, _ := os.ReadFile(path)
	if string(b) != content || !after.ModTime().Equal(before.ModTime()) {
		t.Error("LoadConfig changed the file")
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("LoadConfig wrote a backup")
	}
}

func TestV2Keys(t *testing.T) {
	path := writeCfg(t, strings.Join([]string{
		`cfg_version="2"`, `mode="custom"`, `cpu_curve="40:20,80:100"`, `case_curve="30:30,60:100"`, `min_pct="20"`,
		`cpu_input="max:cpu|gpu"`, `cpu_deadband="3"`, `cpu_up_step="10"`, `cpu_on_missing="hold"`,
		`case_input="disk_avg:disk1+disk2"`, `case_start_pct="60"`, `case_start_ms="2500"`, `case_min_run="15"`, `case_down_step="5"`,
		`fan.fan3.deadband="1"`, `fan.fan3.curve="35:35,55:100"`, `fan.fan3.min_run="40"`,
		`quiet_enabled="1"`, `quiet_from="22:30"`, `quiet_to="06:45"`, `quiet_days="mon,tue,wed,thu,fri"`, `quiet_cap="35"`, `quiet_scope="all"`,
		`job_mover="floor"`, `job_mover_pct="50"`, `job_parity="boost"`, `job_parity_pct="15"`, ``,
	}, "\n"))
	c := LoadConfig(path)
	if len(c.Warnings) != 0 || c.Version != 2 {
		t.Fatalf("warnings %v version %d", c.Warnings, c.Version)
	}
	if c.CPU.Input.String() != "max:cpu|gpu" || c.CPU.Deadband != 3 || c.CPU.UpStep != 10 || c.CPU.OnMissing != "hold" || c.CPU.DownStep != stepDown {
		t.Errorf("cpu %+v", c.CPU)
	}
	// min_run 15 is below the global min_pct 20: the global floor wins.
	if c.Case.Input.String() != "disk_avg:disk1+disk2" || c.Case.StartPct != 60 || c.Case.StartMS != 2500 || c.Case.MinRun != 20 || c.Case.DownStep != 5 {
		t.Errorf("case %+v", c.Case)
	}
	g := c.ForChannel("fan3")
	if g.Deadband != 1 || g.Curve.String() != "35:35,55:100" || g.MinRun != 40 || g.StartPct != 60 || g.DownStep != 5 {
		t.Errorf("fan3 %+v", g)
	}
	if got := c.ForChannel("fan4"); got.Deadband != 0 || got.Curve.String() != "30:30,60:100" {
		t.Errorf("fan4 must keep the group: %+v", got)
	}
	if c.ForChannel("fan1").Deadband != 3 {
		t.Errorf("fan1 follows cpu: %+v", c.ForChannel("fan1"))
	}
	q := c.Quiet
	if !q.Enabled || Clock(q.From) != "22:30" || Clock(q.To) != "06:45" || q.DaysString() != "mon,tue,wed,thu,fri" || q.Cap != 35 || q.Scope != "all" {
		t.Errorf("quiet %+v", q)
	}
	if c.Mover != (Job{"floor", 50}) || c.Parity != (Job{"boost", 15}) {
		t.Errorf("jobs %+v %+v", c.Mover, c.Parity)
	}
}

func TestV2InvalidValuesKeepDefaultsAndWarn(t *testing.T) {
	path := writeCfg(t, strings.Join([]string{
		`cfg_version="2"`, `cpu_deadband="99"`, `case_input="banana"`, `quiet_cap="5"`, `quiet_days="x"`, `job_parity="max"`,
		`fan.fan9.deadband="2"`, `fan.fan1.up_step="0"`, `quiet_enabled="1"`, `quiet_from="08:00"`, `quiet_to="08:00"`, ``,
	}, "\n"))
	c := LoadConfig(path)
	d := defaultConfig()
	if c.CPU.Deadband != 0 || c.Case.Input.Kind != "" || c.Quiet.Cap != d.Quiet.Cap || c.Quiet.DaysString() != d.Quiet.DaysString() || c.Parity.Mode != "off" || len(c.Channels) != 0 {
		t.Errorf("invalid values leaked: %+v", c)
	}
	if c.Quiet.Enabled {
		t.Error("equal from and to must switch quiet hours off")
	}
	for _, key := range []string{"cpu_deadband", "case_input", "quiet_cap", "quiet_days", "job_parity", "fan.fan1.up_step", "quiet_from and quiet_to"} {
		found := false
		for _, w := range c.Warnings {
			found = found || strings.Contains(w, key)
		}
		if !found {
			t.Errorf("no warning for %s: %v", key, c.Warnings)
		}
	}
	for _, w := range c.Warnings {
		if strings.Contains(w, "fan9") {
			t.Errorf("unknown channel must be ignored silently: %q", w)
		}
	}
}

// Preset modes bring their own curves: a channel curve override is accepted and ignored, like cpu_curve.
func TestV2PresetModeIgnoresCurveOverride(t *testing.T) {
	c := LoadConfig(writeCfg(t, "cfg_version=\"2\"\nmode=\"quiet\"\nfan.fan3.curve=\"40:30,80:100\"\n"))
	if got := c.ForChannel("fan3").Curve.String(); got != presets["quiet"][1] || len(c.Warnings) != 0 {
		t.Errorf("fan3 curve %s warnings %v", got, c.Warnings)
	}
}

func TestInputSpecRoundTrip(t *testing.T) {
	for _, s := range []string{
		"cpu", "disk_max", "disk_max:disk1+disk2", "disk_avg:disk1+cache", "nvme", "gpu", "hba", "nic:eth0",
		"script:my_probe", "max:cpu|gpu", "max:disk_avg:disk1+disk2|nvme|script:a|nic:eth1",
	} {
		in, err := ParseInputSpec(s)
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if in.String() != s {
			t.Errorf("%q -> %q", s, in.String())
		}
	}
	if in, err := ParseInputSpec(""); err != nil || in.String() != "auto" {
		t.Errorf("empty = %v %v", in, err)
	}
	if in, _ := ParseInputSpec("disk_avg:disk1+disk1+disk2"); in.String() != "disk_avg:disk1+disk2" {
		t.Errorf("duplicates kept: %s", in)
	}
	long := "disk_max:" + strings.Repeat("a", 300)
	if _, err := ParseInputSpec(long); err == nil {
		t.Error("overlong spec accepted")
	}
}

// The same table is run against web/plugin/include/fan_v2.php by web/src/core/fan-v2-php.test.ts.
func TestValidateSettingTable(t *testing.T) {
	b, err := os.ReadFile("testdata/v2_validation.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Key, Value string
		Ok         bool
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if err := ValidateSetting(c.Key, c.Value); (err == nil) != c.Ok {
			t.Errorf("%s=%q: error %v, want ok=%v", c.Key, c.Value, err, c.Ok)
		}
	}
}

func FuzzParseInputSpec(f *testing.F) {
	for _, s := range []string{"cpu", "max:a|b", "disk_avg:x+y", "script:a", "nic:eth0", "", "max:max:cpu|x"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		in, err := ParseInputSpec(s)
		if err != nil {
			return
		}
		again, err := ParseInputSpec(in.String())
		if err != nil || again.String() != in.String() {
			t.Errorf("%q -> %q does not round trip: %v", s, in.String(), err)
		}
	})
}

func TestView(t *testing.T) {
	c := LoadConfig(writeCfg(t, "cfg_version=\"2\"\ncase_input=\"nvme\"\nfan.fan4.deadband=\"2\"\nquiet_days=\"sat,sun\"\n"))
	v := c.View()
	if v.Version != 2 || v.Case.Input != "nvme" || v.CPU.Input != "auto" || v.Channels["fan4"].Deadband != 2 || v.Quiet.Days != "sat,sun" || v.Quiet.From != "23:00" || v.Quiet.To != "07:00" {
		t.Errorf("%+v", v)
	}
	if b, _ := json.Marshal(defaultConfig().View()); !strings.Contains(string(b), `"channels":{}`) || !strings.Contains(string(b), `"down_step":2`) {
		t.Errorf("%s", b)
	}
}
