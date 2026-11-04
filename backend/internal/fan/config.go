package fan

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Point struct {
	Temp int
	Pct  int
}

type Curve []Point

// Fan modes as in UGOS; "bios" leaves the fans as the BIOS set them.
var presets = map[string][2]string{
	"quiet":       {"50:15,65:25,75:45,85:80,92:100", "35:20,40:25,45:40,50:70,55:100"},
	"standard":    {"45:25,60:35,70:55,80:80,88:100", "30:25,38:30,44:50,50:80,55:100"},
	"performance": {"40:35,55:50,65:70,75:90,85:100", "30:35,36:45,42:65,48:90,52:100"},
	"full":        {"0:100", "0:100"},
}

// Config is the saved settings file: key="value" lines, as in the LED plugin.
type Config struct {
	Mode      string
	CPUCurve  Curve
	CaseCurve Curve
	MinPct    int
	// CaseFollowsCPU: the case fans also follow the CPU curve (the higher of the two wins).
	CaseFollowsCPU bool
	CPUCrit        int
	DiskCrit       int
	NVMeOffset     int
	StallNotify    bool
	// Drives (Unraid names such as "cache" or "disk2", NVMe as "nvme1") that neither drive the case fans nor
	// trigger the disk emergency rule, e.g. an external SSD that runs hot by itself.
	IgnoreDrives map[string]bool
	// FanControl false: this plugin leaves the fan chip alone (another plugin or the BIOS runs the fans).
	FanControl bool
	// Version is cfg_version: 1 when the file has none.
	Version int
	// CPU and Case: the group tuning (v2 keys); the defaults repeat the globals above. Channels holds the groups of
	// the fans that have fan.<id>.<key> overrides.
	CPU, Case Group
	Channels  map[string]Group
	Quiet     Quiet
	Mover     Job
	Parity    Job
	// Warnings name the settings that were present but not valid and were replaced by their default.
	Warnings []string
}

func defaultConfig() Config {
	cpu, _ := ParseCurve(presets["standard"][0])
	cs, _ := ParseCurve(presets["standard"][1])
	c := Config{
		Version:     1,
		Mode:        "standard",
		CPUCurve:    cpu,
		CaseCurve:   cs,
		MinPct:      25,
		CPUCrit:     90,
		DiskCrit:    55,
		NVMeOffset:  15,
		StallNotify: true,
		FanControl:  true,
	}
	c.deriveGroups()
	return c
}

func readValues(path string) map[string]string {
	values := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return values
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return values
}

// LoadConfig reads the settings; anything missing or broken keeps its default.
func LoadConfig(path string) Config {
	c := defaultConfig()
	v := readValues(path)

	warn := func(key string) {
		c.Warnings = append(c.Warnings, fmt.Sprintf("setting %s=%q is not valid, using the default", key, truncate(v[key], 40)))
	}
	if m := v["mode"]; m == "bios" || m == "custom" || presets[m] != [2]string{} {
		c.Mode = m
	} else if m != "" {
		warn("mode")
	}

	cpu, cs := v["cpu_curve"], v["case_curve"]
	if p, ok := presets[c.Mode]; ok {
		cpu, cs = p[0], p[1]
	}
	if curve, err := ParseCurve(cpu); err == nil {
		c.CPUCurve = curve
	} else if c.Mode == "custom" && cpu != "" {
		warn("cpu_curve")
	}
	if curve, err := ParseCurve(cs); err == nil {
		c.CaseCurve = curve
	} else if c.Mode == "custom" && cs != "" {
		warn("case_curve")
	}

	number := func(key string, def, lo, hi int) int {
		n, err := strconv.Atoi(v[key])
		if err != nil || n < lo || n > hi {
			if v[key] != "" {
				warn(key)
			}
			return def
		}
		return n
	}
	c.MinPct = number("min_pct", c.MinPct, 10, 60)
	c.CPUCrit = number("cpu_crit", c.CPUCrit, 70, 105)
	c.DiskCrit = number("disk_crit", c.DiskCrit, 40, 70)
	c.NVMeOffset = number("nvme_offset", c.NVMeOffset, 0, 40)
	c.StallNotify = v["stall_notify"] != "0"
	c.IgnoreDrives = parseDriveList(v["ignore_drives"])
	c.FanControl = v["fan_control"] != "0"
	c.CaseFollowsCPU = v["case_follows_cpu"] == "1"

	c.deriveGroups()
	if n, err := strconv.Atoi(v["cfg_version"]); err == nil && n >= cfgVersion {
		c.Version = cfgVersion
		c.applyV2(v, warn)
	}

	return c
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

var driveNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,40}$`)

// parseDriveList reads "disk2,cache,nvme1": names only, at most 32 of them; anything else is dropped.
func parseDriveList(s string) map[string]bool {
	list := map[string]bool{}
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if driveNameRe.MatchString(name) && len(list) < 32 {
			list[name] = true
		}
	}
	return list
}

// ParseCurve reads "temp:pct,temp:pct,...", e.g. "40:30,60:50,80:100".
func ParseCurve(s string) (Curve, error) {
	var c Curve
	for _, part := range strings.Split(s, ",") {
		t, p, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return nil, fmt.Errorf("curve point %q is not temp:pct", part)
		}
		temp, err1 := strconv.Atoi(t)
		pct, err2 := strconv.Atoi(p)
		if err1 != nil || err2 != nil || temp < 0 || temp > 120 || pct < 0 || pct > 100 {
			return nil, fmt.Errorf("curve point %q is out of range", part)
		}
		c = append(c, Point{Temp: temp, Pct: pct})
	}
	if len(c) == 0 {
		return nil, fmt.Errorf("empty curve")
	}
	sort.Slice(c, func(i, j int) bool { return c[i].Temp < c[j].Temp })
	return c, nil
}

func (c Curve) String() string {
	parts := make([]string, len(c))
	for i, p := range c {
		parts[i] = fmt.Sprintf("%d:%d", p.Temp, p.Pct)
	}
	return strings.Join(parts, ",")
}

func (c Curve) At(temp float64) int {
	if len(c) == 0 {
		return 100
	}
	if temp <= float64(c[0].Temp) {
		return c[0].Pct
	}
	for i := 1; i < len(c); i++ {
		a, b := c[i-1], c[i]
		if temp <= float64(b.Temp) {
			if b.Temp == a.Temp {
				return b.Pct
			}
			f := (temp - float64(a.Temp)) / float64(b.Temp-a.Temp)
			return a.Pct + int(f*float64(b.Pct-a.Pct)+0.5)
		}
	}
	return c[len(c)-1].Pct
}
