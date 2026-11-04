package fan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func intp(n int) *int { return &n }

func TestCurve(t *testing.T) {
	c, err := ParseCurve("60:50, 40:30,80:100")
	if err != nil {
		t.Fatal(err)
	}
	if c.String() != "40:30,60:50,80:100" {
		t.Fatalf("sorted = %s", c)
	}
	cases := map[float64]int{-1: 30, 20: 30, 40: 30, 50: 40, 60: 50, 70: 75, 80: 100, 95: 100}
	for temp, want := range cases {
		if got := c.At(temp); got != want {
			t.Errorf("At(%v) = %d, want %d", temp, got, want)
		}
	}

	for _, bad := range []string{"", "40", "40:101", "abc:20", "130:50"} {
		if _, err := ParseCurve(bad); err == nil {
			t.Errorf("ParseCurve(%q) should fail", bad)
		}
	}

	for name, p := range presets {
		if _, err := ParseCurve(p[0]); err != nil {
			t.Errorf("preset %s cpu: %v", name, err)
		}
		if _, err := ParseCurve(p[1]); err != nil {
			t.Errorf("preset %s case: %v", name, err)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fan.cfg")

	c := LoadConfig(path)
	if c.Mode != "standard" || c.MinPct != 25 || c.CPUCurve.String() != presets["standard"][0] {
		t.Fatalf("defaults = %+v", c)
	}

	os.WriteFile(path, []byte("mode=\"custom\"\ncpu_curve=\"40:30,90:100\"\ncase_curve=\"broken\"\nmin_pct=\"5\"\ncpu_crit=\"95\"\nstall_notify=\"0\"\n"), 0o644)
	c = LoadConfig(path)
	if c.Mode != "custom" || c.CPUCurve.String() != "40:30,90:100" || c.CaseCurve.String() != presets["standard"][1] {
		t.Fatalf("custom = %+v", c)
	}
	if c.MinPct != 25 || c.CPUCrit != 95 || c.StallNotify {
		t.Fatalf("numbers = %+v", c)
	}

	// A preset ignores the saved custom curves.
	os.WriteFile(path, []byte("mode=\"quiet\"\ncpu_curve=\"40:30,90:100\"\n"), 0o644)
	if c = LoadConfig(path); c.CPUCurve.String() != presets["quiet"][0] {
		t.Fatalf("quiet = %s", c.CPUCurve)
	}

	os.WriteFile(path, []byte("mode=\"turbo\"\n"), 0o644)
	if c = LoadConfig(path); c.Mode != "standard" {
		t.Fatalf("unknown mode = %s", c.Mode)
	}
}

func TestDecide(t *testing.T) {
	c := defaultConfig()

	// Idle: CPU 45 °C, disks 35 °C.
	s := Sensors{CPU: intp(45), Disks: []Disk{{Temp: intp(35)}, {Temp: nil, Standby: true}}, NVMe: []Temp{{Temp: intp(40)}}}
	d := Decide(c, s)
	if d.Failsafe != "" || d.CPU != 25 || d.CaseFrom != "disk" || *d.CaseTemp != 35 || d.Case != 28 {
		t.Fatalf("idle = %+v", d)
	}

	// Hot NVMe, minus the offset, beats the disks.
	s.NVMe = []Temp{{Temp: intp(65)}}
	d = Decide(c, s)
	if d.CaseFrom != "nvme" || *d.CaseTemp != 50 || d.Case != 80 {
		t.Fatalf("nvme = %+v", d)
	}

	// Everything asleep: lowest point, never below the minimum.
	d = Decide(c, Sensors{CPU: intp(40)})
	if d.CaseTemp != nil || d.Case != 25 || d.CPU != 25 {
		t.Fatalf("asleep = %+v", d)
	}

	for name, s := range map[string]Sensors{
		"cpu_unreadable": {},
		"cpu_hot":        {CPU: intp(91)},
		"disk_hot":       {CPU: intp(50), Disks: []Disk{{Temp: intp(56)}}},
	} {
		d = Decide(c, s)
		if d.Failsafe != name || d.CPU != 100 || d.Case != 100 {
			t.Errorf("%s = %+v", name, d)
		}
	}
}

func TestSmooth(t *testing.T) {
	cases := [][3]int{{-1, 40, 40}, {30, 60, 60}, {60, 30, 58}, {31, 30, 30}, {50, 50, 50}}
	for _, c := range cases {
		if got := Smooth(c[0], c[1]); got != c[2] {
			t.Errorf("Smooth(%d, %d) = %d, want %d", c[0], c[1], got, c[2])
		}
	}
}

func TestSensors(t *testing.T) {
	dir := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(dir, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}

	write("hwmon/hwmon0/name", "nvme\n")
	write("hwmon/hwmon0/temp1_input", "41850\n")
	write("hwmon/hwmon0/temp1_label", "Composite\n")
	os.MkdirAll(filepath.Join(dir, "devices/nvme0"), 0o755)
	os.Symlink(filepath.Join(dir, "devices/nvme0"), filepath.Join(dir, "hwmon/hwmon0/device"))
	write("hwmon/hwmon4/name", "acpitz\n")
	write("hwmon/hwmon4/temp1_input", "27800\n")
	write("hwmon/hwmon7/name", "coretemp\n")
	write("hwmon/hwmon7/temp1_input", "52000\n")
	write("hwmon/hwmon7/temp1_label", "Package id 0\n")
	write("hwmon/hwmon7/temp2_input", "49000\n")
	write("hwmon/hwmon7/temp2_label", "Core 0\n")
	write("hwmon/hwmon7/temp10_input", "51000\n")
	write("hwmon/hwmon7/temp10_label", "Core 8\n")
	write("disks.ini", `["parity"]
name="parity"
device="sdb"
temp="38"
spundown="0"
rotational="1"
["disk1"]
name="disk1"
device="sdc"
temp="*"
spundown="1"
rotational="1"
["cache"]
name="cache"
device="nvme0n1"
temp="41"
rotational="0"
["flash"]
name="flash"
device="sda"
temp="*"
rotational="1"
`)

	hwmonDir = filepath.Join(dir, "hwmon")
	disksINI = filepath.Join(dir, "disks.ini")
	s := ReadSensors()

	if s.CPU == nil || *s.CPU != 52 || len(s.CPUCores) != 2 || s.CPUCores[1].Name != "Core 8" {
		t.Fatalf("cpu = %v %+v", s.CPU, s.CPUCores)
	}
	if len(s.NVMe) != 1 || s.NVMe[0].Name != "nvme0" || *s.NVMe[0].Temp != 42 {
		t.Fatalf("nvme = %+v", s.NVMe)
	}
	if s.Board == nil || *s.Board != 28 {
		t.Fatalf("board = %v", s.Board)
	}
	if len(s.Disks) != 3 || !s.Disks[1].Standby || s.Disks[1].Temp != nil || *s.MaxDisk() != 38 {
		t.Fatalf("disks = %+v", s.Disks)
	}
}

// Display-only temperatures: the extra NVMe sensors, the iGPU and the PHY / MAC of the LAN ports.
func TestSensorsDisplayOnly(t *testing.T) {
	dir := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(dir, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}

	// nvme with two more sensors whose max is nonsense
	write("hwmon/hwmon1/name", "nvme\n")
	write("hwmon/hwmon1/temp1_input", "41000\n")
	write("hwmon/hwmon1/temp1_label", "Composite\n")
	write("hwmon/hwmon1/temp2_input", "64850\n")
	write("hwmon/hwmon1/temp2_label", "Sensor 1\n")
	write("hwmon/hwmon1/temp2_max", "65261850\n")
	write("hwmon/hwmon1/temp3_input", "42000\n")
	write("hwmon/hwmon1/temp3_label", "Sensor 2\n")
	os.MkdirAll(filepath.Join(dir, "devices/nvme1"), 0o755)
	os.Symlink(filepath.Join(dir, "devices/nvme1"), filepath.Join(dir, "hwmon/hwmon1/device"))
	// iGPU
	write("hwmon/hwmon2/name", "amdgpu\n")
	write("hwmon/hwmon2/temp1_input", "51000\n")
	write("hwmon/hwmon2/temp1_label", "edge\n")
	// atlantic: named after the interface
	write("hwmon/hwmon3/name", "eth0\n")
	write("hwmon/hwmon3/temp1_input", "64000\n")
	write("hwmon/hwmon3/temp1_label", "PHY Temperature\n")
	write("hwmon/hwmon3/temp2_input", "66000\n")
	write("hwmon/hwmon3/temp2_label", "MAC Temperature\n")
	os.MkdirAll(filepath.Join(dir, "sys/class/net/eth0"), 0o755)
	// another driver: the hwmon's device has the interface as a child
	write("hwmon/hwmon4/name", "igc\n")
	write("hwmon/hwmon4/temp1_input", "55000\n")
	os.MkdirAll(filepath.Join(dir, "sys/devices/pci/net/enp2s0"), 0o755)
	os.Symlink(filepath.Join(dir, "sys/devices/pci"), filepath.Join(dir, "hwmon/hwmon4/device"))
	// not a NIC
	write("hwmon/hwmon5/name", "something\n")
	write("hwmon/hwmon5/temp1_input", "30000\n")

	defer func(h, sy, d string) { hwmonDir, sysDir, disksINI = h, sy, d }(hwmonDir, sysDir, disksINI)
	hwmonDir, sysDir = filepath.Join(dir, "hwmon"), filepath.Join(dir, "sys")
	disksINI = filepath.Join(dir, "none.ini")
	s := ReadSensors()

	if s.GPU == nil || *s.GPU != 51 {
		t.Fatalf("gpu = %v", s.GPU)
	}
	if len(s.NVMe) != 1 || *s.NVMe[0].Temp != 41 {
		t.Fatalf("nvme = %+v", s.NVMe)
	}
	if len(s.NVMeExtra) != 2 || s.NVMeExtra[0].Name != "nvme1" || s.NVMeExtra[0].Label != "Sensor 1" || *s.NVMeExtra[0].Temp != 65 || *s.NVMeExtra[1].Temp != 42 {
		t.Fatalf("nvme extra = %+v", s.NVMeExtra)
	}
	if len(s.NIC) != 3 {
		t.Fatalf("nic = %+v", s.NIC)
	}
	if n := s.NIC[0]; n.Iface != "enp2s0" || n.Label != "" || *n.Temp != 55 {
		t.Fatalf("nic[0] = %+v", n)
	}
	if n := s.NIC[1]; n.Iface != "eth0" || n.Label != "PHY" || *n.Temp != 64 {
		t.Fatalf("nic[1] = %+v", n)
	}
	if n := s.NIC[2]; n.Label != "MAC" || *n.Temp != 66 {
		t.Fatalf("nic[2] = %+v", n)
	}

	// Never null for the lists, and the curves still see only the composite sensor.
	hwmonDir = filepath.Join(dir, "empty")
	b, _ := json.Marshal(ReadSensors())
	if !strings.Contains(string(b), `"nic":[]`) || !strings.Contains(string(b), `"nvme_extra":[]`) {
		t.Fatalf("json = %s", b)
	}
}

func TestIgnoredDrivesDoNotDriveTheCaseFans(t *testing.T) {
	hot, warm, cpu := 70, 35, 40
	s := Sensors{CPU: &cpu, Disks: []Disk{
		{Name: "cache", Device: "sdb", Temp: &hot},
		{Name: "disk1", Device: "sdc", Temp: &warm},
	}}
	c := defaultConfig()

	if d := Decide(c, s); d.Failsafe != "disk_hot" {
		t.Fatalf("without the list the hot drive trips the emergency rule: %+v", d)
	}

	c.IgnoreDrives = parseDriveList("cache, nvme9")
	d := Decide(c, s)
	if d.Failsafe != "" || d.CaseTemp == nil || *d.CaseTemp != 35 || d.CaseFrom != "disk" {
		t.Errorf("the ignored drive must not count: %+v", d)
	}

	// An ignored NVMe neither drives the fans nor counts as the hottest.
	h := 62
	s = Sensors{CPU: &cpu, NVMe: []Temp{{Name: "nvme1", Temp: &h}}, Disks: []Disk{{Name: "disk1", Temp: &warm}}}
	c.IgnoreDrives = parseDriveList("nvme1")
	if d := Decide(c, s); d.CaseFrom != "disk" || *d.CaseTemp != 35 {
		t.Errorf("ignored nvme: %+v", d)
	}
}

func TestParseDriveList(t *testing.T) {
	l := parseDriveList(" disk2,cache ,,bad name,nvme1;x,../etc ")
	if len(l) != 2 || !l["disk2"] || !l["cache"] {
		t.Errorf("%v", l)
	}
	if len(parseDriveList("")) != 0 {
		t.Error("empty list")
	}
}

func TestFanControlSwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fan.cfg")

	if !LoadConfig(path).FanControl {
		t.Error("fan control is on by default")
	}
	os.WriteFile(path, []byte("fan_control=\"0\"\nmode=\"quiet\"\n"), 0o644)
	if c := LoadConfig(path); c.FanControl || c.Mode != "quiet" {
		t.Errorf("off: %+v", c)
	}
	os.WriteFile(path, []byte("fan_control=\"1\"\n"), 0o644)
	if !LoadConfig(path).FanControl {
		t.Error("on")
	}
}

func TestDecideCaseFollowsCPU(t *testing.T) {
	c := defaultConfig()
	// CPU 70 °C -> cpu curve 55 %; disks 35 °C -> case curve 28 %.
	s := Sensors{CPU: intp(70), Disks: []Disk{{Temp: intp(35)}}}

	d := Decide(c, s)
	if d.CPU != 55 || d.Case != 28 || d.CaseDriver != "case" {
		t.Fatalf("off = %+v", d)
	}

	c.CaseFollowsCPU = true
	d = Decide(c, s)
	if d.Case != 55 || d.CaseDriver != "cpu" {
		t.Fatalf("on, cpu curve higher = %+v", d)
	}

	// Hot disks: the case curve wins.
	s.Disks = []Disk{{Temp: intp(50)}}
	d = Decide(c, s)
	if d.Case != 80 || d.CaseDriver != "case" {
		t.Fatalf("on, case curve higher = %+v", d)
	}

	// Case below min_pct, CPU curve below too: min_pct still applies.
	c.MinPct = 40
	d = Decide(c, Sensors{CPU: intp(40), Disks: []Disk{{Temp: intp(30)}}})
	if d.Case != 40 || d.CPU != 40 {
		t.Fatalf("min = %+v", d)
	}

	// Emergency still wins.
	d = Decide(c, Sensors{CPU: intp(95)})
	if d.Failsafe != "cpu_hot" || d.Case != 100 {
		t.Fatalf("failsafe = %+v", d)
	}
}

func TestCaseFollowsCPUConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fan.cfg")
	if LoadConfig(path).CaseFollowsCPU {
		t.Fatal("default must be off")
	}
	os.WriteFile(path, []byte("case_follows_cpu=\"1\"\n"), 0o644)
	if !LoadConfig(path).CaseFollowsCPU {
		t.Fatal("1 must turn it on")
	}
}

func TestNVMeEmergency(t *testing.T) {
	cpu, warm := 40, 35
	c := defaultConfig() // disk_crit 55, nvme_offset 15: the NVMe trips at 70
	at := func(n int) Sensors {
		return Sensors{CPU: &cpu, NVMe: []Temp{{Name: "nvme0", Temp: &n}}, Disks: []Disk{{Name: "disk1", Temp: &warm}}}
	}

	if d := Decide(c, at(69)); d.Failsafe != "" {
		t.Errorf("69 C minus the offset is below the limit: %+v", d)
	}
	d := Decide(c, at(70))
	if d.Failsafe != "nvme_hot" || d.CPU != 100 || d.Case != 100 {
		t.Errorf("70 C must trip the NVMe emergency: %+v", d)
	}

	// The offset moves the trip point, the disk limit too.
	c.NVMeOffset = 0
	if d := Decide(c, at(55)); d.Failsafe != "nvme_hot" {
		t.Errorf("offset 0: %+v", d)
	}

	// An ignored NVMe never trips it.
	c = defaultConfig()
	c.IgnoreDrives = parseDriveList("nvme0")
	if d := Decide(c, at(90)); d.Failsafe != "" {
		t.Errorf("ignored NVMe: %+v", d)
	}

	// CPU and disk reasons keep their priority.
	c = defaultConfig()
	hot := 95
	s := at(90)
	s.CPU = &hot
	if d := Decide(c, s); d.Failsafe != "cpu_hot" {
		t.Errorf("cpu first: %+v", d)
	}
}

// A setting that is present but invalid is replaced by its default and reported.
func TestLoadConfigWarnsAboutInvalidValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fan.cfg")
	os.WriteFile(path, []byte("mode=\"turbo\"\nmin_pct=\"5\"\ncpu_crit=\"abc\"\ndisk_crit=\"55\"\nignore_drives=\"disk1,--x;rm\"\n"), 0o644)
	c := LoadConfig(path)
	if c.Mode != "standard" || c.MinPct != 25 || c.CPUCrit != 90 || c.DiskCrit != 55 {
		t.Errorf("defaults not applied: %+v", c)
	}
	if len(c.Warnings) != 3 {
		t.Errorf("warnings: %q", c.Warnings)
	}
	if !c.IgnoreDrives["disk1"] || len(c.IgnoreDrives) != 1 {
		t.Errorf("drive list: %v", c.IgnoreDrives)
	}

	os.WriteFile(path, []byte("mode=\"custom\"\ncpu_curve=\"50:200,60:50\"\ncase_curve=\"40:30,60:50\"\n"), 0o644)
	c = LoadConfig(path)
	if len(c.Warnings) != 1 || c.CaseCurve.String() != "40:30,60:50" {
		t.Errorf("curves: %q, case %s", c.Warnings, c.CaseCurve)
	}

	os.WriteFile(path, []byte("mode=\"quiet\"\n"), 0o644)
	if c = LoadConfig(path); len(c.Warnings) != 0 {
		t.Errorf("a valid file warns: %q", c.Warnings)
	}
}
