package mqtt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

// The publisher: sends the status of this NAS to an MQTT broker for displays and Home Assistant, and, only
// if switched on, carries out a few fixed commands (restart, shut down, update a plugin or a container). It only reads
// the hardware the way `status` does and never writes a fan register.
//
// Topics, below ugreen-nas/<name>/: state, notifications and updates (JSON, retained), availability
// (online/offline, the last will), cmd (commands in) and cmd_result (answers out).

// Where Unraid keeps what is read; tests point these at a fake tree (the variables only work in test builds).
var (
	notifyDir    = envOr("UGREEN_NOTIFY_DIR", "/tmp/notifications/unread")
	pluginsBoot  = envOr("UGREEN_PLUGINS_BOOT", "/boot/config/plugins")
	pluginsFresh = envOr("UGREEN_PLUGINS_FRESH", "/tmp/plugins")
	dockerStatus = envOr("UGREEN_DOCKER_STATUS", "/var/lib/docker/unraid-update-status.json")
	unraidCheck  = envOr("UGREEN_UNRAID_CHECK", "/tmp/unraidcheck/result.json")
	sysDir       = envOr("UGREEN_SYS", "/sys")
	procNetDev   = envOr("UGREEN_PROC_NET_DEV", "/proc/net/dev")
	powerdownBin = envOr("UGREEN_POWERDOWN", "/usr/local/sbin/powerdown")
	pluginBin    = envOr("UGREEN_PLUGIN_BIN", "/usr/local/sbin/plugin")
	dockerBin    = envOr("UGREEN_DOCKER_BIN", "/usr/bin/docker")
	// Unraid's own script for pulling a new image and recreating a container, the same one its Docker page uses.
	updateContainerBin = envOr("UGREEN_UPDATE_CONTAINER", "/usr/local/emhttp/plugins/dynamix.docker.manager/scripts/update_container")
)

func envOr(key, def string) string {
	if v := testenv.Get(key); v != "" {
		return v
	}
	return def
}

func logf(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }

type PubFan struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	RPM   int    `json:"rpm"`
	Pct   *int   `json:"pct"`
	Auto  bool   `json:"auto"`
	Error string `json:"error,omitempty"`
}

type PubNVMe struct {
	Name string `json:"name"`
	Slot string `json:"slot,omitempty"`
	Temp *int   `json:"temp"`
}

type PubNet struct {
	Name      string `json:"name"`
	Link      bool   `json:"link"`
	SpeedMbit int    `json:"speed_mbit"`
	MaxMbit   int    `json:"max_mbit"`
	RxBps     int64  `json:"rx_bps"`
	TxBps     int64  `json:"tx_bps"`
	IP        string `json:"ip,omitempty"`
}

type Alarm struct {
	Level string `json:"level"`
	Code  string `json:"code"` // cpu_hot, cpu_unreadable, disk_hot, nvme_hot, fan_stalled: for a display to translate
	Bay   int    `json:"bay,omitempty"`
	Text  string `json:"text"`
	Value *int   `json:"value,omitempty"`
	Limit int    `json:"limit,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

type PubState struct {
	Time       int64       `json:"time"`
	Model      string      `json:"model"`
	Unraid     string      `json:"unraid"`
	Uptime     int         `json:"uptime"`
	Mode       string      `json:"mode"`
	FanControl bool        `json:"fan_control"`
	CPU        *int        `json:"cpu"`
	Board      *int        `json:"board"`
	CaseTemp   *int        `json:"case_temp"`
	Fans       []PubFan    `json:"fans"`
	Bays       []fan.Bay   `json:"bays"`
	NVMe       []PubNVMe   `json:"nvme"`
	Net        []PubNet    `json:"net"`
	Storage    *PubStorage `json:"storage"`
	Parity     *PubParity  `json:"parity"`
	Alarms     []Alarm     `json:"alarms"`
}

type Notification struct {
	Level string `json:"level"`
	Title string `json:"title"`
	Text  string `json:"text"`
	Time  string `json:"time"`
}

type Update struct {
	Kind string `json:"kind"` // plugin, container, system
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// readNotifications reads Unraid's unread notifications (files of key="value" lines), newest first.
func readNotifications(dir string, now time.Time) []Notification {
	files, _ := filepath.Glob(filepath.Join(dir, "*.notify"))
	type item struct {
		ts int64
		n  Notification
	}
	var items []item
	for _, f := range files {
		v := fan.ReadValues(f)
		ts, _ := strconv.ParseInt(v["timestamp"], 10, 64)
		level := v["importance"]
		if level == "" {
			level = "normal"
		}
		t := time.Unix(ts, 0)
		label := t.Format("15:04")
		if t.YearDay() != now.YearDay() || t.Year() != now.Year() {
			label = t.Format("02.01. 15:04")
		}
		title := v["subject"]
		if title == "" {
			title = v["event"]
		}
		items = append(items, item{ts, Notification{Level: level, Title: title, Text: v["description"], Time: label}})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ts > items[j].ts })
	out := []Notification{}
	for i, it := range items {
		if i == 20 {
			break
		}
		out = append(out, it.n)
	}
	return out
}

var versionRe = regexp.MustCompile(`<!ENTITY\s+version\s+"([^"]+)"`)

func plgVersion(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if m := versionRe.FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

// versionLess compares versions such as 7.3.1 or 2026.10.26a part by part, numbers as numbers.
func versionLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		nx, ex := strconv.Atoi(x)
		ny, ey := strconv.Atoi(y)
		if ex == nil && ey == nil {
			if nx != ny {
				return nx < ny
			}
			continue
		}
		if x != y {
			return x < y
		}
	}
	return false
}

// readOSUpdate reads the result of Unraid's own check for a new Unraid OS (isNewer), which it keeps in
// /tmp/unraidcheck/result.json.
func readOSUpdate(path string) (Update, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Update{}, false
	}
	var r struct {
		Version string `json:"version"`
		IsNewer bool   `json:"isNewer"`
		Params  struct {
			Current string `json:"current_version"`
		} `json:"params"`
	}
	if json.Unmarshal(b, &r) != nil || !r.IsNewer || r.Version == "" {
		return Update{}, false
	}
	return Update{Kind: "system", Name: "Unraid OS", From: r.Params.Current, To: r.Version}, true
}

// readUpdates finds the updates Unraid already knows of: a plugin whose freshly downloaded copy (in /tmp/plugins,
// put there by Unraid's own check) has a higher version than the installed one, and the containers Unraid marked
// as outdated. It checks nothing itself and downloads nothing.
func readUpdates(boot, fresh, dockerFile, osFile string) []Update {
	out := []Update{}
	osKnown := false
	if u, ok := readOSUpdate(osFile); ok {
		out, osKnown = append(out, u), true
	}
	files, _ := filepath.Glob(filepath.Join(boot, "*.plg"))
	sort.Strings(files)
	for _, f := range files {
		base := filepath.Base(f)
		local, remote := plgVersion(f), plgVersion(filepath.Join(fresh, base))
		if local == "" || remote == "" || !versionLess(local, remote) {
			continue
		}
		name := strings.TrimSuffix(base, ".plg")
		kind := "plugin"
		if name == "unRAIDServer" {
			if osKnown {
				continue
			}
			kind, name = "system", "Unraid OS"
		}
		out = append(out, Update{Kind: kind, Name: name, File: base, From: local, To: remote})
	}
	if b, err := os.ReadFile(dockerFile); err == nil {
		var st map[string]struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(b, &st) == nil {
			names := make([]string, 0, len(st))
			for n := range st {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, n := range names {
				// "true": up to date, "false": a newer image exists.
				if st[n].Status == "false" {
					out = append(out, Update{Kind: "container", Name: strings.TrimPrefix(n, "library/"), To: "new image"})
				}
			}
		}
	}
	return out
}

type netCounters struct{ rx, tx uint64 }

func readNetCounters(path string) map[string]netCounters {
	out := map[string]netCounters{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out[strings.TrimSpace(name)] = netCounters{rx, tx}
	}
	return out
}

func hottestBay(bays []fan.Bay) (fan.Bay, bool) {
	var best fan.Bay
	found := false
	for _, b := range bays {
		if b.Present && b.HDD && !b.Standby && b.Temp != nil && (!found || *b.Temp > *best.Temp) {
			best, found = b, true
		}
	}
	return best, found
}

// buildAlarms says what is wrong: what the fan daemon's fail-safe reports, plus the limits checked here too,
// so the display shows an alarm even if the daemon does not run.
func buildAlarms(cpu *int, bays []fan.Bay, nvme []fan.NVMe, st *fan.State, cfg fan.Config) []Alarm {
	out := []Alarm{}
	seen := map[string]bool{}
	add := func(a Alarm) {
		if !seen[a.Text] {
			seen[a.Text] = true
			out = append(out, a)
		}
	}
	hint := ""
	if st != nil && st.Failsafe != "" {
		hint = "fans at full speed"
	}
	if cpu != nil && *cpu >= cfg.CPUCrit {
		add(Alarm{Level: "crit", Code: "cpu_hot", Text: "CPU too hot", Value: cpu, Limit: cfg.CPUCrit, Hint: hint})
	}
	if b, ok := hottestBay(bays); ok && *b.Temp >= cfg.DiskCrit {
		add(Alarm{Level: "crit", Code: "disk_hot", Bay: b.Bay, Text: fmt.Sprintf("Drive %d too hot", b.Bay), Value: b.Temp, Limit: cfg.DiskCrit, Hint: hint})
	}
	for _, n := range nvme {
		if n.Temp != nil && *n.Temp >= cfg.DiskCrit+cfg.NVMeOffset {
			add(Alarm{Level: "crit", Code: "nvme_hot", Text: "NVMe too hot", Value: n.Temp, Limit: cfg.DiskCrit + cfg.NVMeOffset, Hint: hint})
		}
	}
	if st != nil {
		switch st.Failsafe {
		case "cpu_unreadable":
			add(Alarm{Level: "crit", Code: "cpu_unreadable", Text: "CPU temperature unreadable", Hint: hint})
		case "cpu_hot":
			add(Alarm{Level: "crit", Code: "cpu_hot", Text: "CPU too hot", Value: st.CPUTemp, Limit: cfg.CPUCrit, Hint: hint})
		case "disk_hot":
			add(Alarm{Level: "crit", Code: "disk_hot", Text: "Drive too hot", Limit: cfg.DiskCrit, Hint: hint})
		case "nvme_hot":
			add(Alarm{Level: "crit", Code: "nvme_hot", Text: "NVMe too hot", Hint: hint})
		}
		for _, f := range st.Stalled {
			add(Alarm{Level: "warning", Code: "fan_stalled", Text: "Fan stalled: " + f})
		}
	}
	return out
}

// collector reads what a state is made of; the funcs are the fan package's, replaced in tests.
type collector struct {
	live     func() fan.Live
	hardware func() fan.Hardware
	settings func() fan.Config
	daemon   func(time.Time) *fan.State

	hw      fan.Hardware
	hwAt    time.Time
	prevNet map[string]netCounters
	prevAt  time.Time
	rates   map[string][2]int64
}

func newCollector() *collector {
	return &collector{live: fan.ReadLive, hardware: fan.ReadHardware, settings: fan.Settings, daemon: fan.ReadDaemonState}
}

func (c *collector) netRates(now time.Time) {
	cur := readNetCounters(procNetDev)
	if c.prevNet != nil && now.After(c.prevAt) {
		dt := now.Sub(c.prevAt).Seconds()
		c.rates = map[string][2]int64{}
		for n, v := range cur {
			if p, ok := c.prevNet[n]; ok && v.rx >= p.rx && v.tx >= p.tx {
				c.rates[n] = [2]int64{int64(float64(v.rx-p.rx) / dt), int64(float64(v.tx-p.tx) / dt)}
			}
		}
	}
	c.prevNet, c.prevAt = cur, now
}

func (c *collector) state(now time.Time) PubState {
	live := c.live()
	sensors, info, fanOn := live.Sensors, live.Info, live.FanControl
	cfg := c.settings()

	if now.Sub(c.hwAt) > time.Minute {
		c.hw, c.hwAt = c.hardware(), now
	}
	c.netRates(now)
	st := c.daemon(now)

	s := PubState{
		Time: now.Unix(), Model: c.hw.Model, Unraid: info.Unraid, Uptime: info.Uptime, Mode: cfg.Mode, FanControl: fanOn,
		CPU: sensors.CPU, Board: sensors.Board, Fans: []PubFan{}, Bays: c.hw.Bays, NVMe: []PubNVMe{}, Net: []PubNet{},
	}
	if s.Bays == nil {
		s.Bays = []fan.Bay{}
	}
	s.Storage, s.Parity = readStorage(disksINI), readParity(varINI)
	if st != nil {
		s.CaseTemp = st.CaseTemp
		s.Mode = st.Mode
	}
	for _, f := range live.Fans {
		s.Fans = append(s.Fans, PubFan{ID: f.ID, Role: f.Role, RPM: f.RPM, Pct: f.Pct, Auto: f.Auto, Error: f.Err})
	}
	for i, n := range c.hw.NVMe {
		name := n.Role
		if name == "" {
			name = fmt.Sprintf("NVMe %d", i+1)
		}
		s.NVMe = append(s.NVMe, PubNVMe{Name: name, Slot: n.Slot, Temp: n.Temp})
	}
	for _, p := range c.hw.Net {
		r := c.rates[p.Name]
		s.Net = append(s.Net, PubNet{Name: p.Name, Link: p.Link, SpeedMbit: p.SpeedMbit, MaxMbit: p.MaxMbit, RxBps: r[0], TxBps: r[1], IP: ifaceIPv4(p.Name)})
	}
	s.Alarms = buildAlarms(sensors.CPU, c.hw.Bays, c.hw.NVMe, st, cfg)
	return s
}
