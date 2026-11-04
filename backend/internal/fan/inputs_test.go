package fan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func ip(n int) *int { return &n }

func sampleSensors() Sensors {
	return Sensors{
		CPU: ip(61),
		NVMe: []Temp{
			{Name: "nvme0", Temp: ip(48)},
			{Name: "nvme1", Temp: ip(52)},
		},
		NIC: []NICTemp{{Iface: "eth0", Label: "PHY", Temp: ip(70)}, {Iface: "eth0", Label: "MAC", Temp: ip(64)}, {Iface: "eth1", Temp: nil}},
		Disks: []Disk{
			{Name: "disk1", Device: "sda", Temp: ip(30)},
			{Name: "disk2", Device: "sdb", Temp: ip(40)},
			{Name: "disk3", Device: "sdc", Temp: ip(55), Standby: true},
			{Name: "disk4", Device: "sdd"},
		},
	}
}

func mustSpec(t *testing.T, s string) InputSpec {
	t.Helper()
	spec, err := ParseInputSpec(s)
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func newTestRegistry(t *testing.T, s Sensors) (*Registry, *fakeClock, *int, *[]string) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	reads := 0
	var logs []string
	r := newRegistry(clock.now, func() Sensors { reads++; return s }, func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) })
	return r, clock, &reads, &logs
}

func TestReadersFromSensors(t *testing.T) {
	r, _, _, _ := newTestRegistry(t, sampleSensors())
	ctx := context.Background()
	cases := []struct {
		spec string
		want float64
		ok   bool
	}{
		{"cpu", 61, true},
		{"nvme", 52, true},
		{"disk_max", 40, true}, // standby disk3 (55) and templess disk4 are skipped
		{"disk_avg", 35, true}, // (30+40)/2
		{"disk_max:disk1+disk2", 40, true},
		{"disk_avg:disk1+disk3", 30, true}, // the standby one is not counted
		{"disk_max:disk3", 0, false},       // only a standby drive: missing
		{"disk_max:disk2+nvme1", 52, true}, // an NVMe name in a drive list
		{"disk_avg:disk1+nvme0", 39, true},
		{"nic:eth0", 70, true},
		{"nic:eth1", 0, false},
		{"nic:nope", 0, false},
		{"max:cpu|disk_max", 61, true},
		{"max:disk_max:disk3|nic:eth0", 70, true}, // the unreadable part is left out
		{"max:disk_max:disk3|nic:eth1", 0, false},
		{"gpu", 0, false}, // no hwmon GPU, no nvidia-smi
	}
	nvidiaSMIPaths = []string{filepath.Join(t.TempDir(), "none")}
	for _, c := range cases {
		v, ok := r.Read(ctx, mustSpec(t, c.spec))
		if ok != c.ok || (ok && v != c.want) {
			t.Errorf("%s = %v, %v; want %v, %v", c.spec, v, ok, c.want, c.ok)
		}
	}
	if _, ok := r.Read(ctx, InputSpec{}); ok {
		t.Error("the auto spec has no reader in the registry")
	}
}

func TestPerTickCache(t *testing.T) {
	r, _, reads, _ := newTestRegistry(t, sampleSensors())
	ctx := context.Background()
	for range 5 {
		r.Read(ctx, mustSpec(t, "cpu"))
		r.Read(ctx, mustSpec(t, "disk_max"))
	}
	if *reads != 1 {
		t.Fatalf("sensors read %d times in one tick, want 1", *reads)
	}
	r.Tick(nil)
	r.Read(ctx, mustSpec(t, "cpu"))
	if *reads != 2 {
		t.Fatalf("a new tick must read again: %d", *reads)
	}
	// A snapshot handed over by the daemon is used as it is.
	snap := Sensors{CPU: ip(99)}
	r.Tick(&snap)
	if v, _ := r.Read(ctx, mustSpec(t, "cpu")); v != 99 || *reads != 2 {
		t.Fatalf("snapshot not used: %v, reads %d", v, *reads)
	}
	// The same spec spelled twice is one cache entry.
	r.Tick(&snap)
	snap.CPU = ip(50)
	r.Read(ctx, mustSpec(t, "cpu"))
	snap.CPU = ip(10)
	if v, _ := r.Read(ctx, mustSpec(t, "cpu")); v != 50 {
		t.Fatalf("second read in a tick must be cached, got %v", v)
	}
}

func TestAutoInputMatchesDecide(t *testing.T) {
	c := defaultConfig()
	s := sampleSensors()
	c.IgnoreDrives = map[string]bool{"disk2": true}
	v, ok := AutoInput("case", c, s)
	if !ok {
		t.Fatal("case auto must read")
	}
	d := Decide(c, s)
	if d.CaseTemp == nil || float64(*d.CaseTemp) != v {
		t.Fatalf("AutoInput %v differs from Decide %v", v, d.CaseTemp)
	}
	if v, ok := AutoInput("cpu", c, s); !ok || v != 61 {
		t.Fatalf("cpu auto = %v", v)
	}
	if _, ok := AutoInput("cpu", c, Sensors{}); ok {
		t.Fatal("no CPU reading must be missing")
	}
}

func fakeHwmon(t *testing.T, chips map[string]string) {
	t.Helper()
	dir := t.TempDir()
	n := 0
	for name, milli := range chips {
		h := filepath.Join(dir, fmt.Sprintf("hwmon%d", n))
		n++
		if err := os.MkdirAll(h, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(h, "name"), []byte(name+"\n"), 0o644)
		_ = os.WriteFile(filepath.Join(h, "temp1_input"), []byte(milli+"\n"), 0o644)
	}
	old := hwmonDir
	hwmonDir = dir
	t.Cleanup(func() { hwmonDir = old })
}

func TestGPUFromHwmon(t *testing.T) {
	for _, driver := range []string{"amdgpu", "nouveau"} {
		fakeHwmon(t, map[string]string{driver: "67000"})
		disksINI = filepath.Join(t.TempDir(), "none")
		s := ReadSensors()
		if s.GPU == nil || *s.GPU != 67 {
			t.Fatalf("%s: GPU = %v", driver, s.GPU)
		}
		r, _, _, _ := newTestRegistry(t, s)
		if v, ok := r.Read(context.Background(), mustSpec(t, "gpu")); !ok || v != 67 {
			t.Fatalf("%s: gpu input = %v, %v", driver, v, ok)
		}
	}
}

func fakeNvidia(t *testing.T, out string, exit int) (count string) {
	t.Helper()
	dir := t.TempDir()
	count = filepath.Join(dir, "count")
	path := filepath.Join(dir, "nvidia-smi")
	body := fmt.Sprintf("#!/bin/sh\necho x >> %s\nprintf '%s'\nexit %d\n", count, out, exit)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	old := nvidiaSMIPaths
	nvidiaSMIPaths = []string{path}
	t.Cleanup(func() { nvidiaSMIPaths = old })
	return count
}

func runs(t *testing.T, count string) int {
	t.Helper()
	b, _ := os.ReadFile(count)
	return strings.Count(string(b), "x")
}

func TestGPUFromNvidiaSMIWithTTL(t *testing.T) {
	count := fakeNvidia(t, "55\\n61\\n", 0)
	r, clock, _, _ := newTestRegistry(t, Sensors{})
	ctx := context.Background()
	for range 3 {
		r.Tick(nil)
		if v, ok := r.Read(ctx, mustSpec(t, "gpu")); !ok || v != 61 {
			t.Fatalf("gpu = %v, %v (the hottest GPU counts)", v, ok)
		}
		clock.advance(2 * time.Second)
	}
	if n := runs(t, count); n != 1 {
		t.Fatalf("nvidia-smi ran %d times inside its 10 s TTL, want 1", n)
	}
	clock.advance(10 * time.Second)
	r.Tick(nil)
	r.Read(ctx, mustSpec(t, "gpu"))
	if n := runs(t, count); n != 2 {
		t.Fatalf("after the TTL it must run again: %d", n)
	}
}

func TestNvidiaSMIFailures(t *testing.T) {
	for name, c := range map[string]struct {
		out  string
		exit int
	}{"exit": {"55\\n", 9}, "garbage": {"N/A\\n", 0}, "empty": {"", 0}, "hot": {"500\\n", 0}} {
		fakeNvidia(t, c.out, c.exit)
		r, _, _, _ := newTestRegistry(t, Sensors{})
		if v, ok := r.Read(context.Background(), mustSpec(t, "gpu")); ok {
			t.Errorf("%s: read %v, want missing", name, v)
		}
	}
	nvidiaSMIPaths = []string{filepath.Join(t.TempDir(), "absent")}
	r, _, _, _ := newTestRegistry(t, Sensors{})
	if _, ok := r.Read(context.Background(), mustSpec(t, "gpu")); ok {
		t.Error("no nvidia-smi must be missing")
	}
}

func TestListInputsOffersOnlyWhatIsPresent(t *testing.T) {
	oldNvidia, oldDir := nvidiaSMIPaths, sensorsDir
	t.Cleanup(func() { nvidiaSMIPaths, sensorsDir = oldNvidia, oldDir })
	nvidiaSMIPaths = []string{filepath.Join(t.TempDir(), "absent")}
	sensorsDir = filepath.Join(t.TempDir(), "none")
	has := func(rep InputsReport, spec string) bool {
		for _, o := range rep.Options {
			if o.Spec == spec {
				return true
			}
		}
		return false
	}
	r, _, _, _ := newTestRegistry(t, Sensors{})
	rep := ListInputs(context.Background(), r, defaultConfig(), Sensors{})
	for _, spec := range []string{"gpu", "cpu", "nvme", "disk_max", "nic:eth0"} {
		if has(rep, spec) {
			t.Errorf("%s offered on a NAS without it", spec)
		}
	}
	if !has(rep, "auto") {
		t.Error("auto is always offered")
	}

	s := sampleSensors()
	s.GPU = ip(50)
	rep = ListInputs(context.Background(), r, defaultConfig(), s)
	for _, spec := range []string{"gpu", "cpu", "nvme", "disk_max", "disk_avg", "nic:eth0", "nic:eth1"} {
		if !has(rep, spec) {
			t.Errorf("%s missing from %+v", spec, rep.Options)
		}
	}
	if len(rep.Disks) != 6 { // 4 drives, 2 NVMe
		t.Errorf("drive checklist: %+v", rep.Disks)
	}
	if g := rep.Groups["cpu"]; g.Spec != "auto" || !g.Usable || g.Reading == nil || *g.Reading != 61 {
		t.Errorf("cpu group state: %+v", g)
	}
}

func TestListInputsScriptsAreCheckedNotRun(t *testing.T) {
	dir := useScriptDir(t)
	marker := filepath.Join(t.TempDir(), "ran")
	writeScript(t, dir, "probe", fmt.Sprintf("#!/bin/sh\ntouch %s\necho 40\n", marker), 0o755)
	writeScript(t, dir, "loose", "echo 40\n", 0o644)
	nvidiaSMIPaths = []string{filepath.Join(t.TempDir(), "absent")}
	r, _, _, _ := newTestRegistry(t, Sensors{})
	rep := ListInputs(context.Background(), r, defaultConfig(), Sensors{})
	var probe, loose *InputOption
	for i := range rep.Options {
		switch rep.Options[i].Spec {
		case "script:probe":
			probe = &rep.Options[i]
		case "script:loose":
			loose = &rep.Options[i]
		}
	}
	if probe == nil || !probe.Usable || probe.Reading != nil {
		t.Fatalf("probe: %+v", probe)
	}
	if loose == nil || loose.Usable || !strings.Contains(loose.Detail, "no shebang") {
		t.Fatalf("loose: %+v", loose)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("listing must not run scripts")
	}
}

func TestTestInputRateLimitAndSpecErrors(t *testing.T) {
	dir := useScriptDir(t)
	writeScript(t, dir, "probe", "#!/bin/sh\necho 41.5\n", 0o755)
	inputTestDir = t.TempDir()
	r, clock, _, _ := newTestRegistry(t, sampleSensors())
	ctx := context.Background()

	res := TestInput(ctx, r, defaultConfig(), "script:probe")
	if !res.OK || !res.Usable || res.Reading == nil || *res.Reading != 41.5 {
		t.Fatalf("first test: %+v", res)
	}
	again := TestInput(ctx, r, defaultConfig(), "script:probe")
	if again.OK || !strings.Contains(again.Error, "a moment ago") {
		t.Fatalf("a second test inside 2 s must be refused: %+v", again)
	}
	clock.advance(3 * time.Second)
	if res := TestInput(ctx, r, defaultConfig(), "script:probe"); !res.OK || !res.Usable {
		t.Fatalf("after the gap: %+v", res)
	}
	if res := TestInput(ctx, r, defaultConfig(), "script:../x"); res.OK || res.Error == "" {
		t.Fatalf("a bad spec: %+v", res)
	}
	if res := TestInput(ctx, r, defaultConfig(), "cpu"); !res.OK || *res.Reading != 61 {
		t.Fatalf("cpu: %+v", res)
	}
	if res := TestInput(ctx, r, defaultConfig(), "script:missing"); !res.OK || res.Usable || !strings.Contains(res.Detail, "not found") {
		t.Fatalf("missing script: %+v", res)
	}
}

func TestKillGroupHelperIsAvailable(t *testing.T) {
	// Guards the sandbox tests against a platform without process groups.
	if err := syscall.Kill(-os.Getpid()-100000, 0); err == nil {
		t.Skip("unexpected group")
	}
}

func TestRegistryAsDaemonInputSourceHonoursIgnoredDrives(t *testing.T) {
	c := defaultConfig()
	c.IgnoreDrives = map[string]bool{"disk2": true, "nvme1": true}
	src := newInputSource(c, sampleSensors())
	ctx := context.Background()
	for spec, want := range map[string]float64{"disk_max": 30, "nvme": 48, "disk_avg": 30, "cpu": 61} {
		if v, ok := src.Read(ctx, mustSpec(t, spec)); !ok || v != want {
			t.Errorf("%s = %v, %v; want %v", spec, v, ok, want)
		}
	}
	// A new tick with other sensors does not see the old cache.
	s := sampleSensors()
	s.CPU = ip(77)
	if v, _ := newInputSource(c, s).Read(ctx, mustSpec(t, "cpu")); v != 77 {
		t.Fatalf("stale tick cache: %v", v)
	}
}

func fakeStorcli(t *testing.T, out string, exit int) (count string) {
	t.Helper()
	dir := t.TempDir()
	count = filepath.Join(dir, "count")
	path := filepath.Join(dir, "storcli64")
	body := fmt.Sprintf("#!/bin/sh\necho \"x $*\" >> %s\nprintf '%s'\nexit %d\n", count, out, exit)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	old, oldDir := storcliPaths, storcliWorkDir
	storcliPaths, storcliWorkDir = []string{path}, filepath.Join(dir, "work")
	t.Cleanup(func() { storcliPaths, storcliWorkDir = old, oldDir })
	return count
}

func fakeHBAHwmon(t *testing.T, chips map[string][]string) {
	t.Helper()
	dir := t.TempDir()
	n := 0
	for name, milli := range chips {
		h := filepath.Join(dir, fmt.Sprintf("hwmon%d", n))
		n++
		if err := os.MkdirAll(h, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(h, "name"), []byte(name+"\n"), 0o644)
		for i, m := range milli {
			_ = os.WriteFile(filepath.Join(h, fmt.Sprintf("temp%d_input", i+1)), []byte(m+"\n"), 0o644)
		}
	}
	old := hwmonDir
	hwmonDir = dir
	t.Cleanup(func() { hwmonDir = old })
}

func TestHBAFromHwmon(t *testing.T) {
	absentStorcli(t)
	// IOC 42, Board 38 on one card, a second card at 51; a GPU and an NVMe chip are not HBAs.
	fakeHBAHwmon(t, map[string][]string{"mpt3sas": {"42000", "38000"}, "amdgpu": {"90000"}, "nvme": {"80000"}})
	r, _, _, _ := newTestRegistry(t, Sensors{})
	if v, ok := r.Read(context.Background(), mustSpec(t, "hba")); !ok || v != 42 {
		t.Fatalf("hba = %v, %v", v, ok)
	}
	dir := hwmonDir
	h := filepath.Join(dir, "hwmon9")
	_ = os.MkdirAll(h, 0o755)
	_ = os.WriteFile(filepath.Join(h, "name"), []byte("mpt3sas\n"), 0o644)
	_ = os.WriteFile(filepath.Join(h, "temp1_input"), []byte("51000\n"), 0o644)
	r, _, _, _ = newTestRegistry(t, Sensors{})
	if v, ok := r.Read(context.Background(), mustSpec(t, "hba")); !ok || v != 51 {
		t.Fatalf("the hottest card counts: %v, %v", v, ok)
	}
}

func absentStorcli(t *testing.T) {
	t.Helper()
	old := storcliPaths
	storcliPaths = []string{filepath.Join(t.TempDir(), "absent")}
	t.Cleanup(func() { storcliPaths = old })
}

func TestHBAFromStorcliWithTTL(t *testing.T) {
	fakeHBAHwmon(t, map[string][]string{})
	count := fakeStorcli(t, "Controller = 0\\nROC temperature(Degree Celsius) = 55\\nController = 1\\nROC temperature(Degree Celsius) = 63\\n", 0)
	r, clock, _, _ := newTestRegistry(t, Sensors{})
	ctx := context.Background()
	for range 3 {
		r.Tick(nil)
		if v, ok := r.Read(ctx, mustSpec(t, "hba")); !ok || v != 63 {
			t.Fatalf("hba = %v, %v (the hottest controller counts)", v, ok)
		}
		clock.advance(2 * time.Second)
	}
	if n := runs(t, count); n != 1 {
		t.Fatalf("storcli ran %d times inside its 10 s TTL, want 1", n)
	}
	if b, _ := os.ReadFile(count); !strings.Contains(string(b), "/call show temperature") {
		t.Fatalf("storcli called with %q", b)
	}
	clock.advance(10 * time.Second)
	r.Tick(nil)
	r.Read(ctx, mustSpec(t, "hba"))
	if n := runs(t, count); n != 2 {
		t.Fatalf("after the TTL it must run again: %d", n)
	}
}

func TestStorcliRunsInWorkDirAndLeavesNoLog(t *testing.T) {
	fakeHBAHwmon(t, map[string][]string{})
	fakeStorcli(t, "ROC temperature(Degree Celsius) = 55\\n", 0)
	cwd := filepath.Join(t.TempDir(), "cwd")
	body := "#!/bin/sh\necho log > storcli.log\npwd > " + cwd + "\necho 'ROC temperature(Degree Celsius) = 55'\n"
	if err := os.WriteFile(storcliPaths[0], []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readStorcli(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(storcliWorkDir); strings.TrimSpace(string(got)) != want {
		t.Errorf("storcli ran in %q, want %q", strings.TrimSpace(string(got)), want)
	}
	if _, err := os.Stat(filepath.Join(storcliWorkDir, "storcli.log")); err == nil {
		t.Error("storcli.log left in the work dir")
	}
	if _, err := os.Stat("/storcli.log"); err == nil {
		t.Error("storcli.log written to /")
	}
}

func TestHBAFailures(t *testing.T) {
	fakeHBAHwmon(t, map[string][]string{"mpt3sas": {}}) // a chip without a reading is no reading
	for name, c := range map[string]struct {
		out  string
		exit int
	}{
		"exit":    {"ROC temperature(Degree Celsius) 55\\n", 1},
		"garbage": {"ROC temperature(Degree Celsius) N/A\\n", 0},
		"no line": {"Controller = 0\\n", 0},
		"empty":   {"", 0},
		"hot":     {"ROC temperature(Degree Celsius) 500\\n", 0},
	} {
		fakeStorcli(t, c.out, c.exit)
		r, _, _, _ := newTestRegistry(t, Sensors{})
		if v, ok := r.Read(context.Background(), mustSpec(t, "hba")); ok {
			t.Errorf("%s: read %v, want missing", name, v)
		}
	}
	absentStorcli(t)
	r, _, _, _ := newTestRegistry(t, Sensors{})
	if _, ok := r.Read(context.Background(), mustSpec(t, "hba")); ok {
		t.Error("no storcli and no hwmon must be missing")
	}
}

func TestHBAListedOnlyWhenPresent(t *testing.T) {
	oldNvidia, oldDir := nvidiaSMIPaths, sensorsDir
	t.Cleanup(func() { nvidiaSMIPaths, sensorsDir = oldNvidia, oldDir })
	nvidiaSMIPaths = []string{filepath.Join(t.TempDir(), "absent")}
	sensorsDir = filepath.Join(t.TempDir(), "none")
	fakeHBAHwmon(t, map[string][]string{})
	absentStorcli(t)
	list := func() InputsReport {
		r, _, _, _ := newTestRegistry(t, Sensors{})
		return ListInputs(context.Background(), r, defaultConfig(), Sensors{})
	}
	for _, o := range list().Options {
		if o.Spec == "hba" {
			t.Fatal("hba offered on a NAS without a controller")
		}
	}
	fakeStorcli(t, "ROC temperature(Degree Celsius) 57\\n", 0)
	var got *InputOption
	rep := list()
	for i := range rep.Options {
		if rep.Options[i].Spec == "hba" {
			got = &rep.Options[i]
		}
	}
	if got == nil || !got.Usable || got.Reading == nil || *got.Reading != 57 || got.Kind != "hba" {
		t.Fatalf("hba option: %+v", got)
	}
	res := TestInput(context.Background(), func() *Registry { r, _, _, _ := newTestRegistry(t, Sensors{}); return r }(), defaultConfig(), "hba")
	if !res.OK || !res.Usable || res.Reading == nil || *res.Reading != 57 {
		t.Fatalf("input test: %+v", res)
	}
}
