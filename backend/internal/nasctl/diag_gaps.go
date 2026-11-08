package nasctl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/panel"
)

// Everything in this file only reads: sysfs, procfs, files of the plugin and the pad registers through /dev/mem
// opened read-only. No packet, no bus traffic, no write.

// ---- LED network state ----

// NetIface is one network device the LED verdict depends on.
type NetIface struct{ Name, Oper, Carrier, Slaves string }

// NetDiag is why the network LED has its colour: the LED turns red when the device is not "up" or the gateway has
// no live neighbour entry (backend/internal/led/ledctl/net.go). Diag sends nothing; the verdict is the one the LED daemon
// stored.
type NetDiag struct {
	Dev, Verdict, DefaultVia string
	Ifaces                   []NetIface
}

var netNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]*$`)

// skipNet: virtual ports of containers and VMs are not part of the uplink.
var skipNet = regexp.MustCompile(`^(veth|vnet|tap|docker|virbr|shim-)`)

// defaultRouteDev is the device of the first default route in /proc/net/route ("" if there is none); the gateway
// address is never read out.
func (c *Ctl) defaultRouteDev() string {
	data, _ := os.ReadFile(filepath.Join(c.Env.Proc, "net", "route"))
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 8 && f[1] == "00000000" && f[7] == "00000000" && !skipNet.MatchString(f[0]) {
			return f[0]
		}
	}
	return ""
}

// netMembers lists the ports of a bond or bridge and the lower devices of a VLAN.
func (c *Ctl) netMembers(dev string) []string {
	dir := filepath.Join(c.Env.SysNet, dev)
	var out []string
	out = append(out, strings.Fields(readFirstLine(filepath.Join(dir, "bonding", "slaves")))...)
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if m, ok := strings.CutPrefix(e.Name(), "lower_"); ok {
			out = append(out, m)
		}
	}
	ports, _ := os.ReadDir(filepath.Join(dir, "brif"))
	for _, e := range ports {
		out = append(out, e.Name())
	}
	return out
}

func (c *Ctl) netDiag(ledStatus string) NetDiag {
	var st struct {
		Device string `json:"network_device"`
		OK     *bool  `json:"network_ok"`
		Reason string `json:"network_reason"`
	}
	haveStatus := json.Unmarshal([]byte(ledStatus), &st) == nil
	d := NetDiag{Dev: st.Device, DefaultVia: c.defaultRouteDev()}
	switch {
	case !haveStatus:
		d.Verdict = "unknown (ugreen-led-ctl status did not run)"
	case st.OK == nil:
		d.Verdict = "none yet (the LED daemon is not running or has not probed)"
	case *st.OK:
		d.Verdict = "ok"
		if st.Reason != "" {
			d.Verdict = st.Reason
		}
	default:
		d.Verdict = "not ok: " + orDash(st.Reason)
	}
	if d.Dev == "" {
		d.Dev = d.DefaultVia
	}
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if netNameRe.MatchString(n) && !skipNet.MatchString(n) && !seen[n] && len(names) < 16 {
			seen[n] = true
			names = append(names, n)
		}
	}
	add(d.Dev)
	for _, m := range c.netMembers(d.Dev) {
		add(m)
	}
	for _, n := range names {
		dir := filepath.Join(c.Env.SysNet, n)
		d.Ifaces = append(d.Ifaces, NetIface{Name: n, Oper: readFirstLine(filepath.Join(dir, "operstate")),
			Carrier: readFirstLine(filepath.Join(dir, "carrier")), Slaves: readFirstLine(filepath.Join(dir, "bonding", "slaves"))})
	}
	return d
}

func (d NetDiag) lines() []string {
	l := []string{"network (what colours the network LED; read only, nothing is sent):",
		"  LED network device: " + orDash(d.Dev), "  LED daemon verdict: " + d.Verdict}
	for _, i := range d.Ifaces {
		s := fmt.Sprintf("  %s: operstate %s  carrier %s", i.Name, orDash(i.Oper), orDash(i.Carrier))
		if i.Slaves != "" {
			s += "  slaves " + i.Slaves
		}
		l = append(l, s)
	}
	if d.DefaultVia != "" {
		return append(l, "  default route: yes via "+d.DefaultVia)
	}
	return append(l, "  default route: no")
}

// ---- front panel pads ----

// PadsDiag is the raw pad registers of the model's power and reset button.
type PadsDiag struct {
	Model string
	Words []panel.PadWord
	Err   string
}

func (c *Ctl) padsDiag(model string) (d PadsDiag) {
	d.Model = model
	spec := panel.ButtonsFor(model)
	if spec == nil {
		return d
	}
	open := c.OpenMem
	if open == nil {
		open = panel.OpenMem
	}
	m, err := open(false)
	if err != nil {
		d.Err = "open /dev/mem: " + err.Error()
		return d
	}
	defer func() { _ = m.Close() }()
	// A fault on a mapped page would kill the process; turn it into a panic that guard catches.
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	d.Words = spec.ReadWords(m)
	return d
}

func padsSection(d PadsDiag) Section {
	if d.Model != "" && len(d.Words) == 0 && d.Err == "" {
		return Section{Title: "Front panel pads", Body: "no button map for this model"}
	}
	if d.Err != "" {
		return Section{Title: "Front panel pads", Err: d.Err}
	}
	l := []string{"raw 32-bit pad registers (read only; 0xFFFFFFFF means not readable):"}
	for _, w := range d.Words {
		switch {
		case w.Err != nil:
			l = append(l, fmt.Sprintf("  %s: %v", w.Name, w.Err))
		case w.Value == 0xFFFFFFFF:
			l = append(l, fmt.Sprintf("  %s: 0x%08X = 0x%08X  (not readable)", w.Name, w.Addr, w.Value))
		default:
			l = append(l, fmt.Sprintf("  %s: 0x%08X = 0x%08X", w.Name, w.Addr, w.Value))
		}
	}
	return Section{Title: "Front panel pads", Body: strings.Join(l, "\n")}
}

// ---- plugin version and cron ----

var versionDirRe = regexp.MustCompile(`^[0-9]+\.[0-9]+(\.[0-9]+)?(-next\.[0-9]+)?$`)

// installStaleAfter: the install follows the download of the .plg within minutes.
const installStaleAfter = 15 * time.Minute

// versionVerdict compares the flash .plg with what is installed. The files carry no version of their own, so it
// looks at the archive folders the installer keeps on the flash (one per installed version, older ones removed at
// the end of an install) and at the age of the installed tree against the .plg.
func versionVerdict(plg string, cached []string, treeAt, plgAt time.Time) string {
	var why []string
	switch {
	case plg == "":
	case len(cached) == 0:
		why = append(why, "no cached archive on the flash")
	case !slices.Contains(cached, plg) || len(cached) > 1:
		why = append(why, "cached archive(s) "+strings.Join(cached, ", ")+" do not match the .plg "+plg)
	}
	if !treeAt.IsZero() && !plgAt.IsZero() && plgAt.Sub(treeAt) > installStaleAfter {
		why = append(why, "the .plg is newer than the installed files")
	}
	if len(why) == 0 {
		return "ok"
	}
	return "MISMATCH: " + strings.Join(why, "; ") + " (update pending a reboot or reinstall)"
}

func mtime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// fillState reads the version, cron and install facts of the plugin.
func (c *Ctl) fillState(st *StateDiag) {
	boot := filepath.Join(c.Env.PluginsBoot, "ugreen-nas-control")
	for _, n := range dirNames(boot) {
		if v := strings.TrimSuffix(n, "/"); strings.HasSuffix(n, "/") && versionDirRe.MatchString(v) {
			st.Cached = append(st.Cached, v)
		}
	}
	st.TreeAt = mtime(filepath.Join(c.Env.PluginsWeb, "ugreen-nas-control", "UGREENNASControl.page"))
	st.PlgAt = mtime(filepath.Join(c.Env.PluginsBoot, "ugreen-nas-control.plg"))
	if data, err := os.ReadFile(c.Env.LEDCron); err == nil {
		st.CronLines = strings.Split(strings.TrimSpace(string(data)), "\n")
	}
	files, _ := filepath.Glob(filepath.Join(c.Env.CronD, "*ugreen*"))
	for _, f := range files {
		st.CronD = append(st.CronD, filepath.Base(f)+" "+stamp(mtime(f)))
	}
}

// pluginURLKind says whose build the .plg came from without naming a user: a fork's pluginURL carries a GitHub user
// name in the path or in the Pages host.
func pluginURLKind(raw string) string {
	const stable = "https://raw.githubusercontent.com/ugreen-nas-control/ugreen-nas-control/main/plugin/ugreen-nas-control.plg"
	const next = "https://ugreen-nas-control.github.io/ugreen-nas-control/next/ugreen-nas-control.plg"
	switch strings.TrimSpace(raw) {
	case "":
		return ""
	case stable:
		return "official"
	case next:
		return "official next"
	}
	host := strings.TrimSpace(raw)
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	host, _, _ = strings.Cut(host, "/")
	host, _, _ = strings.Cut(host, ":")
	if h, ok := strings.CutSuffix(host, ".github.io"); ok && h != "" {
		host = "github.io"
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	return "other (host only: " + scrub(host) + ")"
}

// ---- syslog ----

// lineTime reads the stamp of a syslog line: "Nov  4 10:00:00 ..." (no year) or an ISO 8601 stamp.
func lineTime(line string, now time.Time) (time.Time, bool) {
	if len(line) >= 15 {
		if t, err := time.ParseInLocation("Jan _2 15:04:05", line[:15], now.Location()); err == nil {
			t = t.AddDate(now.Year(), 0, 0)
			if t.After(now.Add(24 * time.Hour)) {
				t = t.AddDate(-1, 0, 0)
			}
			return t, true
		}
	}
	first, _, _ := strings.Cut(line, " ")
	if t, err := time.Parse(time.RFC3339, first); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// recentLines picks the last n lines that match re and are at most age old (a line without a readable stamp stays).
func recentLines(text string, re *regexp.Regexp, now time.Time, age time.Duration, n int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if !re.MatchString(line) {
			continue
		}
		if t, ok := lineTime(line, now); ok && now.Sub(t) > age {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t\r"))
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// ---- NVMe ----

var pciBDFRe = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)

// NVMeDev is where one NVMe controller sits on the PCI tree; no model, no serial.
type NVMeDev struct {
	ID    string
	Chain []string // BDFs, root port first
	Slot  string   // what the model's slot table made of it
}

func (c *Ctl) nvmeDiag(h fan.Hardware) []NVMeDev {
	paths, _ := filepath.Glob(filepath.Join(c.Env.SysNVMe, "nvme*"))
	sort.Slice(paths, func(i, j int) bool { return natural(paths[i]) < natural(paths[j]) })
	var out []NVMeDev
	for _, p := range paths {
		d := NVMeDev{ID: filepath.Base(p)}
		if target, err := filepath.EvalSymlinks(filepath.Join(p, "device")); err == nil {
			for _, part := range strings.Split(target, "/") {
				if pciBDFRe.MatchString(part) {
					d.Chain = append(d.Chain, part)
				}
			}
		}
		for _, n := range h.NVMe {
			if n.ID == d.ID {
				d.Slot = n.Slot
			}
		}
		out = append(out, d)
	}
	return out
}

func nvmeSection(devs []NVMeDev) Section {
	if len(devs) == 0 {
		return Section{Title: "NVMe", Body: "no NVMe controller in /sys/class/nvme"}
	}
	l := []string{"PCI chain per NVMe controller, root port first (no model, no serial):"}
	for _, d := range devs {
		s := fmt.Sprintf("  %s: %s  slot %s", d.ID, orDash(strings.Join(d.Chain, " > ")), orDash(d.Slot))
		if d.Slot == "pcie" && len(d.Chain) > 0 {
			s += fmt.Sprintf("  (root port %s is in no slot of this model's table)", d.Chain[0])
		}
		l = append(l, s)
	}
	return Section{Title: "NVMe", Body: strings.Join(l, "\n")}
}

// anonymize runs the free text the tools printed through the same filter as the log lines.
func (d *LEDDiag) anonymize(a Anon) {
	d.Status, d.Get = a.Line(d.Status), a.Line(d.Get)
	for i := range d.Adapters {
		d.Adapters[i] = a.Line(d.Adapters[i])
	}
}

func (a Anon) lines(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = a.Line(s)
	}
	return out
}

// pluginLines are the plugin install, start and stop lines of the last 24 hours, at most 40, through the log filter.
func pluginLines(a Anon, syslog string, now time.Time) []string {
	var out []string
	for _, l := range recentLines(syslog, pluginLineRe, now, 24*time.Hour, 40) {
		out = append(out, cut(a.Line(l), 300))
	}
	return out
}
