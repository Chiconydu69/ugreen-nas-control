package nasctl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

// Report is what a model report needs and nothing that identifies the NAS or its owner: no serial numbers, MAC or
// IP addresses, disk serials or names, host name. Every field is picked by name; nothing is passed through.
type Report struct {
	Schema        int         `json:"schema"`
	PluginVersion string      `json:"plugin_version"`
	UnraidVersion string      `json:"unraid_version"`
	Machine       Machine     `json:"machine"`
	CPU           string      `json:"cpu"`
	Threads       int         `json:"threads"`
	Fan           FanReport   `json:"fan"`
	LED           LEDReport   `json:"led"`
	Bays          []BayReport `json:"bays"`
	NVMe          []string    `json:"nvme_slots"`
	Network       []NetReport `json:"network"`
	Memory        []MemReport `json:"memory"`
	Rivals        []string    `json:"rivals"`
}

// Machine are the DMI strings that name the model (never the serial numbers, UUID or asset tags).
type Machine struct {
	SysVendor   string `json:"sys_vendor"`
	Product     string `json:"product_name"`
	Family      string `json:"product_family"`
	BoardVendor string `json:"board_vendor"`
	Board       string `json:"board_name"`
	BIOSVendor  string `json:"bios_vendor"`
	BIOSVersion string `json:"bios_version"`
	BIOSDate    string `json:"bios_date"`
}

// FanReport is the answer of `ugreen-nas-fan probe`.
type FanReport struct {
	Found    bool   `json:"found"`
	Chip     string `json:"chip"`
	Revision any    `json:"revision,omitempty"`
	Base     string `json:"base,omitempty"`
	Ports    string `json:"ports,omitempty"`
	Error    string `json:"error,omitempty"`
}

// LEDReport is the answer of `ugreen-led-i2c probe`: the bus and chip address, and whether the LED part is usable.
type LEDReport struct {
	Supported bool   `json:"supported"`
	Blocker   string `json:"blocker"`
	Probe     string `json:"probe"` // ok, failed, or not run
	Bus       string `json:"bus,omitempty"`
	Address   string `json:"address,omitempty"`
	LEDCount  int    `json:"led_count,omitempty"`
	Error     string `json:"error,omitempty"`
}

// BayReport maps a bay to its SATA port and says what sits in it, never which disk.
type BayReport struct {
	Bay     int    `json:"bay"`
	Port    int    `json:"ata_port,omitempty"`
	Present bool   `json:"present"`
	Kind    string `json:"kind,omitempty"` // hdd or ssd
}

type NetReport struct {
	Name    string `json:"name"`
	MaxMbit int    `json:"max_mbit"`
}

type MemReport struct {
	SizeMB  int    `json:"size_mb"`
	Type    string `json:"type"`
	SpeedMT int    `json:"speed_mt"`
}

// Inputs are the things a report is made of; Collect fills them from the running system.
type Inputs struct {
	Env      Env
	Hardware fan.Hardware
	BayPorts []int
	FanProbe []byte // stdout of the fan tool's probe
	LEDProbe []byte // stdout of the LED tool's probe
	LEDRan   bool
	LEDOK    bool
	Blocker  string
	Rivals   []conflict.Conflict
}

var (
	macRe  = regexp.MustCompile(`(?i)\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b`)
	ipv4Re = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	ipv6Re = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){3,7}[0-9a-f]{1,4}\b`)
)

// scrub is the last line of defence: a MAC or IP address in any text value is cut out.
func scrub(s string) string {
	s = macRe.ReplaceAllString(s, "<removed>")
	s = ipv6Re.ReplaceAllString(s, "<removed>")
	s = ipv4Re.ReplaceAllString(s, "<removed>")
	return strings.TrimSpace(s)
}

func readFirstLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return scrub(line)
}

var versionRe = regexp.MustCompile(`(?m)<!ENTITY\s+version\s+"([^"]+)"`)

// pluginVersion reads the version entity of the installed .plg (a "-next." version is the next channel).
func pluginVersion(boot string) string {
	if data, err := os.ReadFile(filepath.Join(boot, "ugreen-nas-control.plg")); err == nil {
		if m := versionRe.FindSubmatch(data); m != nil {
			return scrub(string(m[1]))
		}
	}
	return ""
}

func unraidVersion(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "version" {
			return scrub(strings.Trim(strings.TrimSpace(v), `"'`))
		}
	}
	return ""
}

func cpuModel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), ":"); ok && strings.TrimSpace(k) == "model name" {
			return scrub(v)
		}
	}
	return ""
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return scrub(s)
}

func Build(in Inputs) Report {
	dmi := func(name string) string { return readFirstLine(filepath.Join(in.Env.DMIDir, name)) }
	r := Report{
		Schema:        1,
		PluginVersion: pluginVersion(in.Env.PluginsBoot),
		UnraidVersion: unraidVersion(in.Env.UnraidFile),
		Machine: Machine{
			SysVendor: dmi("sys_vendor"), Product: dmi("product_name"), Family: dmi("product_family"),
			BoardVendor: dmi("board_vendor"), Board: dmi("board_name"),
			BIOSVendor: dmi("bios_vendor"), BIOSVersion: dmi("bios_version"), BIOSDate: dmi("bios_date"),
		},
		CPU:     cpuModel(filepath.Join(in.Env.Proc, "cpuinfo")),
		Threads: runtime.NumCPU(),
		Bays:    []BayReport{},
		NVMe:    []string{},
		Network: []NetReport{},
		Memory:  []MemReport{},
		Rivals:  []string{},
	}

	var probe map[string]any
	if json.Unmarshal(in.FanProbe, &probe) == nil {
		r.Fan.Found, _ = probe["ok"].(bool)
		r.Fan.Chip = str(probe, "chip")
		r.Fan.Revision = probe["revision"]
		if _, ok := r.Fan.Revision.(float64); !ok {
			r.Fan.Revision = nil
		}
		r.Fan.Base, r.Fan.Ports, r.Fan.Error = str(probe, "base"), str(probe, "ports"), str(probe, "error")
	}

	r.LED = LEDReport{Supported: in.Blocker != "model", Blocker: in.Blocker, Probe: "not run"}
	if in.LEDRan {
		r.LED.Probe = "failed"
		if in.LEDOK {
			r.LED.Probe = "ok"
		}
		var p map[string]any
		if json.Unmarshal(in.LEDProbe, &p) == nil {
			r.LED.Bus, r.LED.Address, r.LED.Error = str(p, "bus"), str(p, "address"), str(p, "error")
			if list, ok := p["leds"].([]any); ok {
				r.LED.LEDCount = len(list)
			}
		}
	}

	for i, b := range in.Hardware.Bays {
		bay := BayReport{Bay: b.Bay, Present: b.Present}
		if i < len(in.BayPorts) {
			bay.Port = in.BayPorts[i]
		}
		if b.Present {
			bay.Kind = "ssd"
			if b.HDD {
				bay.Kind = "hdd"
			}
		}
		r.Bays = append(r.Bays, bay)
	}
	for _, n := range in.Hardware.NVMe {
		r.NVMe = append(r.NVMe, scrub(n.Slot))
	}
	for _, n := range in.Hardware.Net {
		r.Network = append(r.Network, NetReport{Name: scrub(n.Name), MaxMbit: n.MaxMbit})
	}
	for _, m := range in.Hardware.Memory {
		if m.SizeMB > 0 {
			r.Memory = append(r.Memory, MemReport{SizeMB: m.SizeMB, Type: scrub(m.Type), SpeedMT: m.SpeedMT})
		}
	}
	for _, c := range in.Rivals {
		r.Rivals = append(r.Rivals, scrub(c.ID))
	}
	return r
}

func (c *Ctl) report() int {
	in := Inputs{Env: c.Env, Blocker: c.ledBlocker(), Rivals: c.rivalList()}
	in.Hardware = fan.ReadHardware()
	in.BayPorts = fan.BayPorts(in.Hardware.Model)

	var out bytes.Buffer
	_ = run(c.Env.FanTool, &out, nil, "probe")
	in.FanProbe = append([]byte(nil), out.Bytes()...)

	if in.Blocker != "model" {
		out.Reset()
		in.LEDRan = true
		in.LEDOK = run(c.Env.LEDI2C, &out, nil, "probe")
		in.LEDProbe = append([]byte(nil), out.Bytes()...)
	}

	enc := json.NewEncoder(c.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Build(in)); err != nil {
		c.errf("ERROR: %v\n", err)
		return 1
	}
	return 0
}
