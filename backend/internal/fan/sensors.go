package fan

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

// Where the sensors live; tests point these at a fake tree.
var (
	hwmonDir = envOr("UGREEN_HWMON", "/sys/class/hwmon")
	disksINI = envOr("UGREEN_DISKS_INI", "/var/local/emhttp/disks.ini")
)

func envOr(key, def string) string {
	if v := testenv.Get(key); v != "" {
		return v
	}
	return def
}

type Temp struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	Temp  *int   `json:"temp"`
}

type Disk struct {
	Name    string `json:"name"`
	Device  string `json:"device"`
	ID      string `json:"id,omitempty"` // Unraid's disk id: model_serial
	Temp    *int   `json:"temp"`
	Standby bool   `json:"standby"`
	HDD     bool   `json:"hdd"`
}

// NICTemp is one temperature of a network chip (a NIC with a hwmon, e.g. atlantic: PHY and MAC per port).
type NICTemp struct {
	Iface string `json:"iface"`
	Label string `json:"label,omitempty"` // PHY, MAC
	Temp  *int   `json:"temp"`
}

// Sensors is everything the fan curves look at, plus temperatures that are only shown (GPU, NIC, NVMe
// sensors beyond the composite one); the curves never read those.
type Sensors struct {
	CPU       *int      `json:"cpu"`
	CPUCores  []Temp    `json:"cpu_cores"`
	NVMe      []Temp    `json:"nvme"`
	NVMeExtra []Temp    `json:"nvme_extra"` // Sensor 1, Sensor 2 ... of each drive (name = nvme0, ...)
	NIC       []NICTemp `json:"nic"`
	GPU       *int      `json:"gpu"`
	Disks     []Disk    `json:"disks"`
	Board     *int      `json:"board"`
}

func readInt(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return n, err == nil
}

func readString(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

func milli(path string) *int {
	n, ok := readInt(path)
	if !ok {
		return nil
	}
	t := (n + 500) / 1000
	return &t
}

// ReadSensors collects CPU (coretemp, or k10temp on AMD), NVMe (hwmon) and Unraid's disk temperatures.
// Disks are only read from disks.ini, so a spun-down disk is never woken up.
func ReadSensors() Sensors {
	s := Sensors{NVMeExtra: []Temp{}, NIC: []NICTemp{}}

	dirs, _ := filepath.Glob(filepath.Join(hwmonDir, "hwmon*"))
	sort.Slice(dirs, func(i, j int) bool { return hwmonNumber(dirs[i]) < hwmonNumber(dirs[j]) })

	for _, dir := range dirs {
		switch readString(filepath.Join(dir, "name")) {
		case "coretemp":
			inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
			sort.Slice(inputs, func(i, j int) bool { return tempNumber(inputs[i]) < tempNumber(inputs[j]) })
			for _, in := range inputs {
				label := readString(strings.TrimSuffix(in, "_input") + "_label")
				t := milli(in)
				if strings.HasPrefix(label, "Package") {
					if s.CPU == nil {
						s.CPU = t
					}
					continue
				}
				s.CPUCores = append(s.CPUCores, Temp{Name: label, Temp: t})
			}
		case "k10temp":
			// AMD: Tdie where the chip has it, else Tctl; Tccd* are the core dies.
			inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
			sort.Slice(inputs, func(i, j int) bool { return tempNumber(inputs[i]) < tempNumber(inputs[j]) })
			for _, in := range inputs {
				label := readString(strings.TrimSuffix(in, "_input") + "_label")
				t := milli(in)
				switch label {
				case "Tdie":
					if t != nil {
						s.CPU = t
					}
				case "Tctl":
					if s.CPU == nil {
						s.CPU = t
					}
				default:
					s.CPUCores = append(s.CPUCores, Temp{Name: label, Temp: t})
				}
			}
		case "nvme":
			name := filepath.Base(readLink(filepath.Join(dir, "device")))
			s.NVMe = append(s.NVMe, Temp{Name: name, Label: readString(filepath.Join(dir, "temp1_label")), Temp: milli(filepath.Join(dir, "temp1_input"))})
			// temp2 and up (Sensor 1, ...): shown only; their max/crit are not trustworthy.
			extra, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
			sort.Slice(extra, func(i, j int) bool { return tempNumber(extra[i]) < tempNumber(extra[j]) })
			for _, in := range extra {
				if tempNumber(in) > 1 {
					s.NVMeExtra = append(s.NVMeExtra, Temp{Name: name, Label: readString(strings.TrimSuffix(in, "_input") + "_label"), Temp: milli(in)})
				}
			}
		case "acpitz":
			if s.Board == nil {
				s.Board = milli(filepath.Join(dir, "temp1_input"))
			}
		case "amdgpu", "nouveau", "i915", "xe":
			if s.GPU == nil {
				s.GPU = milli(filepath.Join(dir, "temp1_input"))
			}
		default:
			if iface := nicOf(dir); iface != "" {
				inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
				sort.Slice(inputs, func(i, j int) bool { return tempNumber(inputs[i]) < tempNumber(inputs[j]) })
				for _, in := range inputs {
					label := strings.TrimSuffix(readString(strings.TrimSuffix(in, "_input")+"_label"), " Temperature")
					s.NIC = append(s.NIC, NICTemp{Iface: iface, Label: label, Temp: milli(in)})
				}
			}
		}
	}

	// hwmon numbers follow probe order; nvme0, nvme1, ... read better.
	sort.SliceStable(s.NVMe, func(i, j int) bool { return s.NVMe[i].Name < s.NVMe[j].Name })
	sort.SliceStable(s.NVMeExtra, func(i, j int) bool { return s.NVMeExtra[i].Name < s.NVMeExtra[j].Name })
	sort.SliceStable(s.NIC, func(i, j int) bool { return s.NIC[i].Iface < s.NIC[j].Iface })

	// Without a package sensor the hottest core counts.
	if s.CPU == nil {
		for _, c := range s.CPUCores {
			if c.Temp != nil && (s.CPU == nil || *c.Temp > *s.CPU) {
				t := *c.Temp
				s.CPU = &t
			}
		}
	}

	s.Disks = readDisks(disksINI)
	return s
}

// nicOf is the network interface a hwmon belongs to, "" when it isn't a NIC's: the hwmon's device has a
// net/<iface> child, or the chip is named after an interface in /sys/class/net (atlantic: eth0).
func nicOf(dir string) string {
	if nets, _ := filepath.Glob(filepath.Join(dir, "device", "net", "*")); len(nets) > 0 {
		return filepath.Base(nets[0])
	}
	name := readString(filepath.Join(dir, "name"))
	if name == "" || strings.ContainsAny(name, "/") {
		return ""
	}
	if _, err := os.Stat(filepath.Join(sysDir, "class", "net", name)); err == nil {
		return name
	}
	return ""
}

func readLink(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return target
}

func hwmonNumber(path string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "hwmon"))
	return n
}

func tempNumber(path string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "temp"), "_input"))
	return n
}

// readDisks parses Unraid's disks.ini: [section] lines and key="value" lines.
// NVMe devices are skipped; their own sensors are read above.
func readDisks(path string) []Disk {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var disks []Disk
	var cur map[string]string

	flush := func() {
		if cur == nil || cur["device"] == "" || strings.HasPrefix(cur["device"], "nvme") {
			return
		}
		d := Disk{
			Name:    cur["name"],
			Device:  cur["device"],
			ID:      cur["id"],
			Standby: cur["spundown"] == "1",
			HDD:     cur["rotational"] != "0",
		}
		if n, err := strconv.Atoi(cur["temp"]); err == nil {
			d.Temp = &n
		}
		disks = append(disks, d)
	}

	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			flush()
			cur = map[string]string{}
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok && cur != nil {
			cur[key] = strings.Trim(value, `"`)
		}
	}
	flush()

	return disks
}

// Hottest disk and NVMe; nil when none reports a temperature.
func (s Sensors) MaxDisk() *int { return s.MaxDiskExcept(nil) }

func (s Sensors) MaxDiskExcept(ignore map[string]bool) *int {
	var hottest *int
	for _, d := range s.Disks {
		if ignore[d.Name] {
			continue
		}
		if d.Temp != nil && (hottest == nil || *d.Temp > *hottest) {
			hottest = d.Temp
		}
	}
	return hottest
}

func (s Sensors) MaxNVMe() *int { return s.MaxNVMeExcept(nil) }

func (s Sensors) MaxNVMeExcept(ignore map[string]bool) *int {
	var hottest *int
	for _, n := range s.NVMe {
		if ignore[n.Name] {
			continue
		}
		if n.Temp != nil && (hottest == nil || *n.Temp > *hottest) {
			hottest = n.Temp
		}
	}
	return hottest
}
