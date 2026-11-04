package fan

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
)

// Where the hardware is read from; tests point these at a fake tree.
var (
	sysDir      = envOr("UGREEN_SYS", "/sys")
	dmidecodeBn = envOr("UGREEN_DMIDECODE", "dmidecode")
	ethtoolBn   = envOr("UGREEN_ETHTOOL", "ethtool")
	mountsFile  = envOr("UGREEN_MOUNTS", "/proc/mounts")
	// tmpfs: what dmidecode and ethtool report does not change while the NAS runs, and every page
	// that asks for the hardware starts a new process. "" switches the cache off.
	cacheDir = envOr("UGREEN_HW_CACHE", "/var/run")
)

func cachePath(name string) string {
	if cacheDir == "" {
		return ""
	}
	return filepath.Join(cacheDir, "ugreen-nas-control.hw-"+name+".json")
}

func readCache(name string, v any) bool {
	p := cachePath(name)
	if p == "" {
		return false
	}
	b, err := os.ReadFile(p)
	return err == nil && json.Unmarshal(b, v) == nil
}

// writeCache is best effort: no cache file just means asking again next time.
func writeCache(name string, v any) {
	p := cachePath(name)
	if p == "" {
		return
	}
	if b, err := json.Marshal(v); err == nil {
		_ = fsx.WriteFile(p, b, 0o644)
	}
}

type layout struct {
	// The kernel's ata port of each bay, left to right (the same order as UGREEN LED
	// Control uses).
	bayPorts []int
	// The slot behind each PCIe root port: "m2-1", "m2-2", "system" (the internal M.2 of
	// the UGOS module) or "pcie" (a card in the PCIe slot). An NVMe behind any other
	// port counts as "pcie".
	slots map[string]string
	// The slot of an NVMe behind a root port that is not in slots: "pcie" unless the model says otherwise.
	otherSlot string
}

// layoutFor knows the DXP6800 Pro (ata3..ata6, then ata1, ata2), the DXP4800 GT, the DXP4800 / Plus / Pro
// (ata1..ata4) and the DXP8800 Plus (ata1..ata8), the order UGREEN LED Control uses. On the last three the root ports of the M.2 slots are not
// known, so an NVMe is just "m2". Any other model: no bays, every NVMe on the PCIe slot.
//
// DXP6800 Pro, from an Unraid box with four NVMe: the CPU's root port 00:06.0 is M.2 slot 1
// (x4, PCIe 4.0), 00:06.2 the PCIe slot (a card with one SSD, negotiated x1), and the
// chipset's 00:1c.0 the second M.2 slot (x2 negotiated) and 00:1c.2 the internal system
// slot (x1), the one of the UGOS module. Confirmed by the author for the SSD in the system slot.
func layoutFor(model string) layout {
	switch {
	case strings.HasPrefix(model, "DXP6800"):
		return layout{[]int{3, 4, 5, 6, 1, 2}, map[string]string{
			"0000:00:06.0": "m2-1", "0000:00:1c.0": "m2-2", "0000:00:1c.2": "system", "0000:00:06.2": "pcie",
		}, ""}
	case strings.HasPrefix(model, "DXP4800 GT"):
		return layout{[]int{1, 2, 3, 4}, map[string]string{"0000:00:01.4": "m2-1", "0000:00:01.6": "m2-2"}, ""}
	case model == "DXP4800" || model == "DXP4800 Plus" || model == "DXP4800 Pro":
		return layout{bayPorts: []int{1, 2, 3, 4}, otherSlot: "m2"}
	case model == "DXP2800":
		return layout{bayPorts: []int{1, 2}, otherSlot: "m2"}
	case model == "DXP8800 Plus", model == "DXP8800", model == "DXP8800 Pro":
		return layout{bayPorts: []int{1, 2, 3, 4, 5, 6, 7, 8}, otherSlot: "m2"}
	}
	return layout{}
}

type DIMM struct {
	Locator      string `json:"locator"`
	SizeMB       int    `json:"size_mb"`
	Type         string `json:"type"`
	SpeedMT      int    `json:"speed_mt"`
	ConfiguredMT int    `json:"configured_mt"`
	Manufacturer string `json:"manufacturer"`
	PartNumber   string `json:"part_number"`
}

type NVMe struct {
	ID        string `json:"id"`
	Model     string `json:"model"`
	SizeBytes int64  `json:"size_bytes"`
	Slot      string `json:"slot"` // "m2-1", "m2-2", "m2" (an M.2 slot of unknown place), "system" or "pcie"
	PCI       string `json:"pci"`
	Temp      *int   `json:"temp"`
	Role      string `json:"role,omitempty"` // Unraid's name for it: cache, a pool, disk1, ...
	Boot      bool   `json:"boot"`           // /boot (Unraid's flash) is on this SSD
}

type Bay struct {
	Bay       int    `json:"bay"`
	Present   bool   `json:"present"`
	Device    string `json:"device,omitempty"`
	Model     string `json:"model,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	HDD       bool   `json:"hdd"`
	Role      string `json:"role,omitempty"` // Unraid's name: parity, disk1, ...
	ID        string `json:"id,omitempty"`   // Unraid's disk id: model_serial, as on its Main page
	Temp      *int   `json:"temp"`
	Standby   bool   `json:"standby"`
}

type NetPort struct {
	Name      string `json:"name"`
	Link      bool   `json:"link"`
	SpeedMbit int    `json:"speed_mbit"`
	// The highest speed the port supports (ethtool), 0 when unknown: tells the 10G port from the 2.5G one.
	MaxMbit int `json:"max_mbit"`
}

type Hardware struct {
	Model  string    `json:"model"`
	Memory []DIMM    `json:"memory"`
	NVMe   []NVMe    `json:"nvme"`
	Bays   []Bay     `json:"bays"`
	Net    []NetPort `json:"net"`
}

// ReadHardware looks at what is built in. It only reads: sysfs, Unraid's disks.ini
// (never the disks themselves) and dmidecode.
func ReadHardware() Hardware {
	s := ReadSensors()
	model := hw.ProductName(dmiDir)
	l := layoutFor(model)
	if chip.IdxLayout(model) != "" {
		// iDX6011 / iDX6012 (Pro): the bays sit on two SATA controllers, so the ata numbers say nothing.
		l = layout{bayPorts: idxBayPorts(model), otherSlot: "m2"}
	}
	hw := Hardware{Model: model, Memory: readMemory(), NVMe: readNVMe(s, l), Net: readNet()}
	if len(l.bayPorts) > 0 {
		hw.Bays = readBays(s, l)
	}
	return hw
}

// idxBayPorts is the kernel's ata port of each bay of the iDX6011 / iDX6012, after UGREEN LED Control. Plain model:
// bays 1-4 are the ports of the ASMedia SATA controller (PCI vendor 0x1b21), bays 5-6 those of the Intel chipset
// (0x8086), each in port order. Pro (chip.IdxLayout): ASMedia ports 1-2 are bays 1-2, the Intel ports bays 3-4,
// ASMedia ports 3-4 bays 5-6. If the controllers don't have 4 + 2 ports: ata1 .. ata6.
func idxBayPorts(model string) []int {
	byVendor := map[string][]int{}
	paths, _ := filepath.Glob(filepath.Join(sysDir, "class", "ata_port", "ata*"))
	for _, vendor := range []string{"0x1b21", "0x8086"} {
		var mine []int
		for _, p := range paths {
			n, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(p), "ata"))
			resolved, err2 := filepath.EvalSymlinks(p)
			if err != nil || err2 != nil {
				continue
			}
			chain := pciChain(resolved)
			if len(chain) == 0 {
				continue
			}
			if readString(filepath.Join(sysDir, "bus", "pci", "devices", chain[len(chain)-1], "vendor")) == vendor {
				mine = append(mine, n)
			}
		}
		sort.Ints(mine)
		byVendor[vendor] = mine
	}
	asm, intel := byVendor["0x1b21"], byVendor["0x8086"]
	var ports []int
	if chip.IdxLayout(model) == "idx-pro" {
		if len(asm) == 4 && len(intel) == 2 {
			ports = []int{asm[0], asm[1], intel[0], intel[1], asm[2], asm[3]}
		}
	} else if len(asm)+len(intel) == 6 {
		ports = append(append(ports, asm...), intel...)
	}
	if ports == nil {
		return []int{1, 2, 3, 4, 5, 6}
	}
	return ports
}

var sizeRe = regexp.MustCompile(`^(\d+)\s*(MB|MiB|GB|GiB|TB|TiB)$`)

func parseDmidecode(out string) []DIMM {
	var dimms []DIMM
	var cur map[string]string

	flush := func() {
		if cur == nil {
			return
		}
		d := DIMM{
			Locator:      cur["Locator"],
			Type:         cur["Type"],
			Manufacturer: cur["Manufacturer"],
			PartNumber:   cur["Part Number"],
			SpeedMT:      mtPerSecond(cur["Speed"]),
			ConfiguredMT: mtPerSecond(cur["Configured Memory Speed"]),
		}
		if m := sizeRe.FindStringSubmatch(cur["Size"]); m != nil {
			n, _ := strconv.Atoi(m[1])
			switch m[2] {
			case "GB", "GiB":
				n *= 1024
			case "TB", "TiB":
				n *= 1024 * 1024
			}
			d.SizeMB = n
		}
		dimms = append(dimms, d)
	}

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Memory Device" {
			flush()
			cur = map[string]string{}
			continue
		}
		if key, value, ok := strings.Cut(trimmed, ":"); ok && cur != nil {
			if _, seen := cur[key]; !seen {
				cur[key] = strings.TrimSpace(value)
			}
		}
	}
	flush()

	return dimms
}

func mtPerSecond(s string) int {
	n, _ := strconv.Atoi(strings.Fields(s + " 0")[0])
	return n
}

func readMemory() []DIMM {
	var dimms []DIMM
	if readCache("memory", &dimms) && len(dimms) > 0 {
		return dimms
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, dmidecodeBn, "-t", "memory").Output()
	if err != nil {
		return nil
	}
	dimms = parseDmidecode(string(out))
	if len(dimms) > 0 {
		writeCache("memory", dimms)
	}
	return dimms
}

var pciRe = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-9a-f]$`)

func pciChain(path string) []string {
	var chain []string
	for _, part := range strings.Split(path, "/") {
		if pciRe.MatchString(part) {
			chain = append(chain, part)
		}
	}
	return chain
}

func sectorBytes(block string) int64 {
	n, _ := strconv.ParseInt(readString(filepath.Join(sysDir, "block", block, "size")), 10, 64)
	return n * 512
}

var (
	bootRe    = regexp.MustCompile(`^/dev/(nvme\d+)n\d+(?:p\d+)?$`)
	nvmeDevRe = regexp.MustCompile(`^(nvme\d+)n\d+`)
)

// bootNVMe names the NVMe controller /boot is mounted from ("" if /boot is on something else).
func bootNVMe() string {
	b, err := os.ReadFile(mountsFile)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == "/boot" {
			if m := bootRe.FindStringSubmatch(f[0]); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

// nvmeRoles maps an NVMe controller (nvme0) to its name in Unraid's disks.ini ("cache", "disk1",
// "flash", ...), taken from the section whose device is nvme0n1.
func nvmeRoles(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	roles := map[string]string{}
	var cur map[string]string
	flush := func() {
		if cur == nil {
			return
		}
		if m := nvmeDevRe.FindStringSubmatch(cur["device"]); m != nil && cur["name"] != "" {
			roles[m[1]] = cur["name"]
		}
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

	return roles
}

func readNVMe(s Sensors, l layout) []NVMe {
	temps := map[string]*int{}
	for _, t := range s.NVMe {
		temps[t.Name] = t.Temp
	}
	roles, boot := nvmeRoles(disksINI), bootNVMe()

	paths, _ := filepath.Glob(filepath.Join(sysDir, "class", "nvme", "nvme*"))
	sort.Slice(paths, func(i, j int) bool { return nvmeNumber(paths[i]) < nvmeNumber(paths[j]) })

	var list []NVMe
	for _, p := range paths {
		id := filepath.Base(p)
		resolved := readLink(filepath.Join(p, "device"))
		chain := pciChain(resolved)

		d := NVMe{ID: id, Model: readString(filepath.Join(p, "model")), Slot: "pcie", Temp: temps[id]}
		if l.otherSlot != "" {
			d.Slot = l.otherSlot
		}
		if len(chain) > 0 {
			d.PCI = chain[len(chain)-1]
			// Behind a bridge the NVMe is the last address, the root port the first.
			if slot, ok := l.slots[chain[0]]; ok {
				d.Slot = slot
			}
		}
		d.SizeBytes = sectorBytes(id + "n1")
		d.Role = roles[id]
		d.Boot = id == boot || d.Role == "flash"
		list = append(list, d)
	}
	return list
}

func nvmeNumber(path string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(path), "nvme"))
	return n
}

var ataRe = regexp.MustCompile(`/ata(\d+)/`)

func readBays(s Sensors, l layout) []Bay {
	byDevice := map[string]Disk{}
	for _, d := range s.Disks {
		byDevice[d.Device] = d
	}

	bays := make([]Bay, len(l.bayPorts))
	for i := range bays {
		bays[i].Bay = i + 1
	}

	devices, _ := filepath.Glob(filepath.Join(sysDir, "block", "sd*"))
	for _, dev := range devices {
		m := ataRe.FindStringSubmatch(readLink(filepath.Join(dev, "device")) + "/")
		if m == nil {
			continue
		}
		port, _ := strconv.Atoi(m[1])
		for i, p := range l.bayPorts {
			if p != port {
				continue
			}
			name := filepath.Base(dev)
			b := Bay{
				Bay:       i + 1,
				Present:   true,
				Device:    name,
				Model:     readString(filepath.Join(dev, "device", "model")),
				SizeBytes: sectorBytes(name),
				HDD:       readString(filepath.Join(dev, "queue", "rotational")) != "0",
			}
			if d, ok := byDevice[name]; ok {
				b.Role, b.ID, b.Temp, b.Standby, b.HDD = d.Name, d.ID, d.Temp, d.Standby, d.HDD
			}
			bays[i] = b
		}
	}
	return bays
}

func netPaths() []string {
	paths, _ := filepath.Glob(filepath.Join(sysDir, "class", "net", "e*"))
	sort.Strings(paths)

	var nics []string
	for _, p := range paths {
		// Bridges, bonds and VLANs have no device behind them.
		if _, err := os.Stat(filepath.Join(p, "device")); err == nil {
			nics = append(nics, p)
		}
	}
	return nics
}

func readNet() []NetPort {
	var ports []NetPort
	for _, p := range netPaths() {
		port := NetPort{Name: filepath.Base(p)}
		if n, ok := readInt(filepath.Join(p, "speed")); ok && n > 0 {
			port.Link, port.SpeedMbit = readString(filepath.Join(p, "carrier")) == "1", n
		}
		port.MaxMbit = maxLinkSpeed(port.Name)
		ports = append(ports, port)
	}
	return ports
}

type NetIO struct {
	Name string `json:"name"`
	Rx   uint64 `json:"rx"`
	Tx   uint64 `json:"tx"`
}

// ReadNetIO reads two sysfs counters per physical NIC. NICs without readable counters are left out.
func ReadNetIO() []NetIO {
	out := []NetIO{}
	for _, p := range netPaths() {
		rx, err1 := strconv.ParseUint(readString(filepath.Join(p, "statistics", "rx_bytes")), 10, 64)
		tx, err2 := strconv.ParseUint(readString(filepath.Join(p, "statistics", "tx_bytes")), 10, 64)
		if err1 == nil && err2 == nil {
			out = append(out, NetIO{Name: filepath.Base(p), Rx: rx, Tx: tx})
		}
	}
	return out
}

// toolTimeout bounds dmidecode and ethtool: both answer in milliseconds, a hung one must not hang the page.
const toolTimeout = 10 * time.Second

var linkModeRe = regexp.MustCompile(`(\d+)base`)

// missTTL is how long "ethtool said no for this port" is believed: a NIC that was not ready at boot gets asked again.
var missTTL = 10 * time.Minute

func maxLinkSpeed(name string) int {
	speeds := map[string]int{}
	readCache("maxspeed", &speeds)
	if n := speeds[name]; n > 0 {
		return n
	}
	// The failures live in their own file with the time of the miss (unix seconds); a stale -1 in "maxspeed" is ignored.
	misses := map[string]int64{}
	readCache("maxspeed_miss", &misses)
	if t, ok := misses[name]; ok && time.Since(time.Unix(t, 0)) < missTTL {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, ethtoolBn, name).Output()
	if err != nil {
		// Only an answer of ethtool itself is a miss: no binary, a timeout or a cancel says nothing about the port.
		if ctx.Err() == nil && !errors.Is(err, exec.ErrNotFound) && !errors.Is(err, fs.ErrNotExist) {
			misses[name] = time.Now().Unix()
			writeCache("maxspeed_miss", misses)
		}
		return 0
	}
	n := parseMaxLinkSpeed(string(out))
	if n > 0 {
		speeds[name] = n
		writeCache("maxspeed", speeds)
	}
	return n
}

func parseMaxLinkSpeed(out string) int {
	fastest, in := 0, false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "Supported link modes:"):
			in = true
		case strings.Contains(line, "Supported pause frame use:"):
			in = false
		}
		if !in {
			continue
		}
		for _, m := range linkModeRe.FindAllStringSubmatch(line, -1) {
			if n, _ := strconv.Atoi(m[1]); n > fastest {
				fastest = n
			}
		}
	}
	return fastest
}

// BayPorts is the kernel's ata port of each bay, left to right ("DXP6800 Pro": 3, 4, 5, 6, 1, 2). Nil for a model
// without a table.
func BayPorts(model string) []int {
	if chip.IdxLayout(model) != "" {
		return idxBayPorts(model)
	}
	return layoutFor(model).bayPorts
}
