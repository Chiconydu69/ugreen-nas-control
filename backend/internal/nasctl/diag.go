package nasctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/mqtt"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/panel"
)

// Anon removes what identifies the NAS or its owner from free text: MAC and IP addresses, the host name, values
// that follow a serial/uuid/user/share label, paths below /mnt and, for log lines, serial-looking tokens.
type Anon struct{ Hostnames []string }

var (
	labelRe = regexp.MustCompile(`(?i)\b(serial(?:[ _-]?(?:number|no))?|s/n|sn|uuid|wwn|user(?:name)?|share|label)\b(\s*[=:]\s*)("[^"]*"|\S+)`)
	mntRe   = regexp.MustCompile(`/mnt/[^\s"',;]+`)
	tokenRe = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_-]{7,}`)
)

// serialLike: a token of 8 or more characters with at least 3 digits and 2 capital letters (WD-WCC4N1234567,
// S3Z9NB0K123456A). Dates, versions and lower-case words do not match.
func serialLike(tok string) bool {
	digits, upper := 0, 0
	for _, r := range tok {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r >= 'A' && r <= 'Z':
			upper++
		}
	}
	return digits >= 3 && upper >= 2
}

// Text is for every output: addresses, host name, labelled values, /mnt paths.
func (a Anon) Text(s string) string { return a.text(s, true) }

func (a Anon) text(s string, ip bool) string {
	for _, h := range a.Hostnames {
		if len(h) < 2 {
			continue
		}
		// Not inside a longer name: a host called "nas" must not cut "ugreen-nas-ctl".
		re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9-])` + regexp.QuoteMeta(h) + `([^A-Za-z0-9-]|$)`)
		for range 2 {
			s = re.ReplaceAllString(s, "${1}<host>${2}")
		}
	}
	s = macRe.ReplaceAllString(s, "<removed>")
	s = ipv6Re.ReplaceAllString(s, "<removed>")
	if ip {
		s = scrubIPv4(s)
	}
	s = labelRe.ReplaceAllString(s, "$1$2<removed>")
	s = mntRe.ReplaceAllString(s, "/mnt/<removed>")
	return s
}

// Meta is Text without the IPv4 pass, for the header: its version numbers (1.0.0.12) look like addresses, and the
// report values were scrubbed when they were read.
func (a Anon) Meta(s string) string {
	return a.text(s, false)
}

// Line is Text plus the serial-looking tokens; for log lines, where nothing names a model on purpose.
func (a Anon) Line(s string) string {
	s = a.Text(s)
	return tokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		if serialLike(tok) {
			return "<removed>"
		}
		return tok
	})
}

// Section is one block of the output; a failed section carries its error instead of (or next to) its text.
type Section struct {
	Title string
	Lang  string // fence language
	Body  string
	Err   string
}

// RenderDiag lays the header and the checks out as fenced blocks and every other section as a collapsed <details>
// block, so a paste stays readable.
func RenderDiag(a Anon, now time.Time, kernel string, r Report, checks []string, secs []Section) string {
	var b strings.Builder
	body := func(s Section) string {
		text := strings.TrimRight(s.Body, "\n")
		if s.Err != "" {
			if text != "" {
				text += "\n"
			}
			text += "ERROR: " + s.Err
		}
		return strings.TrimRight(a.Text(text), "\n")
	}
	b.WriteString("## ugreen-nas-ctl diag\n\n")
	fmt.Fprintf(&b, "```\n%s\n```\n\n", strings.TrimRight(a.Meta(diagHeader(now, kernel, r)), "\n"))
	if len(checks) == 0 {
		checks = []string{"no problem detected"}
	}
	fmt.Fprintf(&b, "Checks:\n\n```\n%s\n```\n\n", a.Text(strings.Join(checks, "\n")))
	secs = append([]Section{{Title: "Report summary", Body: reportSummary(r)}}, secs...)
	for _, s := range secs {
		fmt.Fprintf(&b, "<details><summary>%s</summary>\n\n```%s\n%s\n```\n\n</details>\n\n", s.Title, s.Lang, body(s))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func orNone(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func diagHeader(now time.Time, kernel string, r Report) string {
	m := r.Machine
	return fmt.Sprintf("time (UTC): %s\nplugin: %s\nunraid: %s\nkernel: %s\nmodel: %s %s (family %s)\nboard: %s %s\nbios: %s %s %s",
		now.UTC().Format("2006-01-02 15:04:05"), orNone(r.PluginVersion), orNone(r.UnraidVersion), orNone(kernel),
		m.SysVendor, orNone(m.Product), orNone(m.Family), m.BoardVendor, orNone(m.Board), m.BIOSVendor, m.BIOSVersion, m.BIOSDate)
}

func reportSummary(r Report) string {
	var l []string
	l = append(l, fmt.Sprintf("cpu: %s, %d threads", orNone(r.CPU), r.Threads))
	var mem []string
	for _, m := range r.Memory {
		mem = append(mem, fmt.Sprintf("%d MB %s %d MT/s", m.SizeMB, m.Type, m.SpeedMT))
	}
	l = append(l, "memory: "+orDash(strings.Join(mem, "; ")))
	if r.Fan.Found {
		l = append(l, fmt.Sprintf("fan chip: %s revision %v base %s ports %s", r.Fan.Chip, orDash(fmt.Sprint(orNil(r.Fan.Revision))), orDash(r.Fan.Base), orDash(r.Fan.Ports)))
	} else {
		l = append(l, "fan chip: not found: "+orDash(r.Fan.Error))
	}
	led := fmt.Sprintf("led: supported %t, blocker %s, probe %s", r.LED.Supported, orDash(r.LED.Blocker), r.LED.Probe)
	if r.LED.Bus != "" || r.LED.Address != "" {
		led += fmt.Sprintf(", bus %s address %s, %d leds", r.LED.Bus, r.LED.Address, r.LED.LEDCount)
	}
	if r.LED.Error != "" {
		led += ", error: " + r.LED.Error
	}
	l = append(l, led)
	l = append(l, "nvme slots: "+orDash(strings.Join(r.NVMe, ", ")))
	var nets []string
	for _, n := range r.Network {
		nets = append(nets, fmt.Sprintf("%s max %d Mbit", n.Name, n.MaxMbit))
	}
	l = append(l, "network: "+orDash(strings.Join(nets, ", ")))
	l = append(l, "rivals: "+orDash(strings.Join(r.Rivals, ", ")))
	return strings.Join(l, "\n")
}

func orNil(v any) any {
	if v == nil {
		return ""
	}
	return v
}

func orDash(s string) string {
	if s == "" || s == "<nil>" {
		return "-"
	}
	return s
}

var slotRe = regexp.MustCompile(`^(parity2?|disk\d{1,2}|cache|flash)$`)

// slotNames are Unraid's slot names per bay (parity, disk1, ...); a pool name is chosen by the owner, so it shows as "pool".
func slotNames(h fan.Hardware) []string {
	out := make([]string, len(h.Bays))
	for i, b := range h.Bays {
		switch {
		case !b.Present:
			out[i] = "-"
		case b.Role == "":
			out[i] = "unassigned"
		case slotRe.MatchString(b.Role):
			out[i] = b.Role
		default:
			out[i] = "pool"
		}
	}
	return out
}

// LEDDiag is what the LED section is made of; Collect fills it, the builder only formats.
type LEDDiag struct {
	Enabled, Supported, Untested, DaemonRunning bool
	Blocker, Warning                            string
	Bays                                        []BayReport
	Slots                                       []string
	Modprobe                                    string // what was done to i2c-dev, if anything
	Get, GetErr                                 string
	Scan, ScanErr                               string
	GT                                          bool
	CS201x, CS201xErr                           string
	Skipped                                     string   // why the hardware reads did not run
	Adapters                                    []string // names of the I2C adapters
	Status, StatusErr                           string   // ugreen-led-ctl status
	StatusSkipped                               string
}

func ledSection(d LEDDiag) Section {
	var l []string
	l = append(l, fmt.Sprintf("enabled: %t  supported: %t  untested model: %t  daemon running: %t", d.Enabled, d.Supported, d.Untested, d.DaemonRunning))
	l = append(l, fmt.Sprintf("blocker: %s  warning: %s", orDash(d.Blocker), orDash(d.Warning)))
	l = append(l, "i2c adapters: "+orDash(strings.Join(d.Adapters, "; ")))
	for i, b := range d.Bays {
		slot := "-"
		if i < len(d.Slots) {
			slot = d.Slots[i]
		}
		kind := "empty"
		if b.Present {
			kind = orDash(b.Kind)
		}
		l = append(l, fmt.Sprintf("bay %d: ata%d  %s  slot %s", b.Bay, b.Port, kind, slot))
	}
	if d.Skipped != "" {
		l = append(l, "hardware reads skipped: "+d.Skipped)
		return Section{Title: "LED", Body: strings.Join(l, "\n")}
	}
	if d.Modprobe != "" {
		l = append(l, d.Modprobe)
	}
	l = appendResult(l, "per-LED state (name mode brightness R G B on_ms off_ms)", d.Get, d.GetErr)
	l = appendResult(l, "i2c scan all", scanText(d.Scan), d.ScanErr)
	switch {
	case d.StatusSkipped != "":
		l = append(l, "ugreen-led-ctl status skipped: "+d.StatusSkipped)
	default:
		l = appendResult(l, "ugreen-led-ctl status", compactJSON(d.Status), d.StatusErr)
	}
	if d.GT {
		l = appendResult(l, "cs201x dump (read only)", compactJSON(d.CS201x), d.CS201xErr)
	}
	return Section{Title: "LED", Body: strings.Join(l, "\n")}
}

func appendResult(l []string, title, out, err string) []string {
	l = append(l, title+":")
	if err != "" {
		l = append(l, "  ERROR: "+err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			l = append(l, "  "+line)
		}
	}
	return l
}

// scanText: one line per bus, "/dev/i2c-2: 0x44 0x4a ..." (busy: addresses a kernel driver holds).
func scanText(s string) string {
	var m map[string]struct {
		Answer []string `json:"answer"`
		Busy   []string `json:"busy"`
		Error  string   `json:"error"`
	}
	if json.Unmarshal([]byte(s), &m) != nil {
		return s
	}
	buses := make([]string, 0, len(m))
	for b := range m {
		buses = append(buses, b)
	}
	sort.Slice(buses, func(i, j int) bool { return natural(buses[i]) < natural(buses[j]) })
	var out []string
	for _, b := range buses {
		v := m[b]
		line := b + ": " + orDash(strings.Join(v.Answer, " "))
		if len(v.Busy) > 0 {
			line += "  busy: " + strings.Join(v.Busy, " ")
		}
		if v.Error != "" {
			line += "  error: " + v.Error
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// compactJSON puts a JSON document on few lines: one per top-level key.
func compactJSON(s string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &m) != nil {
		return s
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		var buf bytes.Buffer
		if json.Compact(&buf, m[k]) != nil {
			buf.Write(m[k])
		}
		out = append(out, k+": "+buf.String())
	}
	return strings.Join(out, "\n")
}

// FanDiag is the answer of `ugreen-nas-fan status` and whether the daemon runs.
type FanDiag struct {
	Extra         []Result // read-only answers of the fan tool: probe-fans status/tachs, power-on-ac, cs201x, backlight; shown
	Aux           []Result // answers only the Features section uses (history, profile sources)
	Status        []byte
	Err           string
	DaemonRunning bool
}

// fanStatus is the part of `ugreen-nas-fan status` that diag shows.
type fanStatus struct {
	FanControl *bool  `json:"fan_control"`
	Chip       string `json:"chip"`
	ChipError  string `json:"chip_error"`
	Fans       []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
		RPM  int    `json:"rpm"`
		Pct  *int   `json:"pct"`
		Auto bool   `json:"auto"`
		Err  string `json:"error"`
	} `json:"fans"`
	Sensors struct {
		CPU          *int `json:"cpu"`
		Board        *int `json:"board"`
		NotConnected []struct {
			Source string `json:"source"`
			Name   string `json:"name"`
			Raw    int    `json:"raw"`
		} `json:"not_connected"`
	} `json:"sensors"`
	Daemon *struct {
		Mode       string         `json:"mode"`
		Failsafe   string         `json:"failsafe"`
		CPUTemp    *int           `json:"cpu_temp"`
		CaseTemp   *int           `json:"case_temp"`
		CaseFrom   string         `json:"case_from"`
		CaseDriver string         `json:"case_driver"`
		Targets    map[string]int `json:"targets"`
		Stalled    []string       `json:"stalled"`
		Conflict   []string       `json:"conflict"`
		Unapplied  string         `json:"unapplied"`
		Missing    []string       `json:"inputs_missing"`
		Quiet      bool           `json:"quiet_active"`
		Job        string         `json:"job"`
	} `json:"daemon"`
	Watchdog *struct {
		Armed    bool   `json:"armed"`
		Timeout  int    `json:"timeout"`
		Interval int    `json:"interval"`
		LastPet  int64  `json:"last_pet"`
		Time     int64  `json:"time"`
		State    string `json:"state"`
		Counter  *int   `json:"counter"`
	} `json:"watchdog"`
	Conflicts []struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Detail   string `json:"detail"`
	} `json:"conflicts"`
	FanCaps     json.RawMessage `json:"fan_caps"`
	Calibration struct {
		Table *struct {
			Fans map[string][][2]int `json:"fans"`
		} `json:"table"`
		Progress *struct {
			State string `json:"state"`
		} `json:"progress"`
	} `json:"calibration"`
}

func fanSection(d FanDiag) Section {
	l := []string{fmt.Sprintf("daemon running: %t", d.DaemonRunning)}
	var st fanStatus
	extras := func(l []string) []string {
		for _, r := range d.Extra {
			l = appendResult(l, r.Title, compactJSON(r.Out), r.Err)
		}
		return l
	}
	if d.Err != "" {
		return Section{Title: "Fans", Body: strings.Join(extras(l), "\n"), Err: d.Err}
	}
	if err := json.Unmarshal(d.Status, &st); err != nil {
		return Section{Title: "Fans", Body: strings.Join(extras(l), "\n"), Err: "status is not JSON: " + err.Error()}
	}
	on := "unknown"
	if st.FanControl != nil {
		on = fmt.Sprint(*st.FanControl)
	}
	l = append(l, fmt.Sprintf("fan control on: %s  chip: %s", on, orDash(st.Chip)))
	if st.ChipError != "" {
		l = append(l, "chip error: "+st.ChipError)
	}
	for _, f := range st.Fans {
		pct := "unknown"
		if f.Pct != nil {
			pct = fmt.Sprintf("%d%%", *f.Pct)
		}
		line := fmt.Sprintf("fan %s (%s): %s, %d rpm, auto %t", f.ID, f.Role, pct, f.RPM, f.Auto)
		if f.Err != "" {
			line += ", error: " + f.Err
		}
		l = append(l, line)
	}
	l = append(l, fmt.Sprintf("temps: cpu %s  board %s", tempText(st.Sensors.CPU), tempText(st.Sensors.Board)))
	for _, n := range st.Sensors.NotConnected {
		l = append(l, fmt.Sprintf("temp %s %s: not connected (raw %d C)", n.Source, n.Name, n.Raw))
	}
	if dm := st.Daemon; dm != nil {
		l = append(l, fmt.Sprintf("daemon state: mode %s  failsafe %s  job %s  quiet %t", orDash(dm.Mode), orDash(dm.Failsafe), orDash(dm.Job), dm.Quiet))
		l = append(l, fmt.Sprintf("source temps: cpu %s  case %s (from %s, driver %s)", tempText(dm.CPUTemp), tempText(dm.CaseTemp), orDash(dm.CaseFrom), orDash(dm.CaseDriver)))
		keys := make([]string, 0, len(dm.Targets))
		for k := range dm.Targets {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var t []string
		for _, k := range keys {
			t = append(t, fmt.Sprintf("%s %d%%", k, dm.Targets[k]))
		}
		l = append(l, "targets: "+orDash(strings.Join(t, ", ")))
		l = append(l, fmt.Sprintf("stalled: %s  inputs missing: %s  yielding to: %s  unapplied: %s",
			orDash(strings.Join(dm.Stalled, ",")), orDash(strings.Join(dm.Missing, ",")), orDash(strings.Join(dm.Conflict, ",")), orDash(dm.Unapplied)))
	} else {
		l = append(l, "daemon state: none (daemon not running or no state yet)")
	}
	if w := st.Watchdog; w != nil {
		l = append(l, fmt.Sprintf("watchdog: %s  armed %t  timeout %ds  interval %ds  last feed %s", orDash(w.State), w.Armed, w.Timeout, w.Interval, ageText(w.LastPet, w.Time)))
	}
	var cf []string
	for _, c := range st.Conflicts {
		s := fmt.Sprintf("%s:%s (%s)", c.Kind, c.ID, c.Severity)
		if c.Detail != "" {
			s += " " + c.Detail
		}
		cf = append(cf, s)
	}
	l = append(l, "conflicts: "+orDash(strings.Join(cf, "; ")))
	if len(st.FanCaps) > 0 {
		l = append(l, "fan caps: "+short(string(st.FanCaps), 400))
	}
	l = append(l, "calibration: "+calibrationText(st))
	return Section{Title: "Fans", Body: strings.Join(extras(l), "\n")}
}

// calibrationText: the state and, per fan, how many points the rpm table has and its lowest and highest rpm.
func calibrationText(st fanStatus) string {
	state := "never run"
	if p := st.Calibration.Progress; p != nil {
		state = p.State
	}
	if t := st.Calibration.Table; t != nil {
		ids := make([]string, 0, len(t.Fans))
		for id := range t.Fans {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if pts := t.Fans[id]; len(pts) > 0 {
				state += fmt.Sprintf("; %s %d points, %d rpm at %d%% to %d rpm at %d%%", id, len(pts), pts[0][1], pts[0][0], pts[len(pts)-1][1], pts[len(pts)-1][0])
			}
		}
	}
	return state
}

func tempText(t *int) string {
	if t == nil {
		return "-"
	}
	return fmt.Sprintf("%d C", *t)
}

func ageText(last, now int64) string {
	if last == 0 {
		return "never"
	}
	return fmt.Sprintf("%ds before the sample", now-last)
}

// EnvDiag is what the Environment section is made of.
type EnvDiag struct {
	Plugins     []string          // entries of /boot/config/plugins, folders with a trailing slash
	Versions    map[string]string // version of each .plg
	OwnFiles    []string          // names inside the plugin's own folder
	Modules     []string          // loaded modules
	Hwmon       []string          // hwmon device names
	DevI2C      []string
	PluginsBoot string
}

var (
	pluginInterest = regexp.MustCompile(`(?i)ugreen|led|fan|it87|idx|dxp|sensor|temp|autofan`)
	moduleInterest = regexp.MustCompile(`^(it87|it87_wdt|led_ugreen|i2c_i801|i2c_dev|i2c_smbus|i2c_designware.*|ugreen.*|ug_.*|leds_.*|coretemp|k10temp|nct6.*|jc42|drivetemp)$`)
)

func envSection(d EnvDiag) Section {
	var plugins []string
	for _, p := range d.Plugins {
		if pluginInterest.MatchString(p) && !strings.Contains(p, "-backup-") {
			if v := d.Versions[p]; v != "" {
				p += " " + v
			}
			plugins = append(plugins, p)
		}
	}
	l := []string{
		"other LED/fan plugins in /boot/config/plugins: " + orDash(strings.Join(plugins, " ")),
		"files in the plugin's own folder: " + orDash(strings.Join(d.OwnFiles, " ")),
	}
	var mods []string
	for _, m := range d.Modules {
		if moduleInterest.MatchString(m) {
			mods = append(mods, m)
		}
	}
	l = append(l, "loaded modules: "+orDash(strings.Join(mods, " ")))
	l = append(l, "i2c devices: "+orDash(strings.Join(d.DevI2C, " ")))
	l = append(l, "hwmon: "+orDash(strings.Join(d.Hwmon, " ")))
	return Section{Title: "Environment", Body: strings.Join(l, "\n")}
}

var logTagRe = regexp.MustCompile(`\bugreen-[a-z0-9-]+(\[\d+\])?:`)

// logLines picks the last n lines that carry one of this plugin's syslog tags.
func logLines(a Anon, text string, n int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if logTagRe.MatchString(line) {
			out = append(out, cut(a.Line(line), 300))
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func logSection(lines []string, err string) Section {
	if err == "" && len(lines) == 0 {
		return Section{Title: "Log", Body: "no ugreen-* lines in the syslog"}
	}
	return Section{Title: "Log (last " + fmt.Sprint(len(lines)) + " ugreen-* syslog lines)", Body: strings.Join(lines, "\n"), Err: err}
}

// runTimeout runs a tool with a deadline and returns its stdout; the error holds its stderr or why it failed.
func runTimeout(d time.Duration, path string, args ...string) (string, error) {
	if dl := diagDeadline(); !dl.IsZero() {
		left := time.Until(dl)
		if left <= 0 {
			return "", fmt.Errorf("skipped: the %s time budget of diag is used up", diagBudget)
		}
		d = min(d, left)
	}
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	var out, errb bytes.Buffer
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	ownGroup(cmd)
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return out.String(), fmt.Errorf("timed out after %s", d)
	}
	if err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("%s", firstLines(msg, 3))
	}
	return out.String(), nil
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " | ")
}

func tail(path string, limit int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err == nil && st.Size() > limit {
		_, _ = f.Seek(st.Size()-limit, 0)
	}
	var b bytes.Buffer
	_, _ = b.ReadFrom(f)
	return b.String()
}

func dirNames(dir string) []string {
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() {
			n += "/"
		}
		out = append(out, n)
	}
	return out
}

func kernelVersion(proc string) string {
	data, err := os.ReadFile(filepath.Join(proc, "sys/kernel/osrelease"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func moduleNames(path string) []string {
	data, _ := os.ReadFile(path)
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

func hwmonNames(dir string) []string {
	dirs, _ := filepath.Glob(filepath.Join(dir, "hwmon*"))
	var out []string
	for _, d := range dirs {
		if data, err := os.ReadFile(filepath.Join(d, "name")); err == nil {
			out = append(out, filepath.Base(d)+"="+strings.TrimSpace(string(data)))
		}
	}
	return out
}

func hostnames() []string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return nil
	}
	short, _, _ := strings.Cut(h, ".")
	if short != h {
		return []string{h, short}
	}
	return []string{h}
}

const (
	toolTimeout = 15 * time.Second
	scanTimeout = 30 * time.Second
	diagBudget  = 60 * time.Second // all tool calls of one diag run together
)

var deadlineNano atomic.Int64

// diagDeadline is the end of the budget; the zero time while no diag run set one.
func diagDeadline() time.Time {
	if n := deadlineNano.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Time{}
}

// sections collects the output blocks; a panic or error stays in its own block.
type sections struct {
	list   []Section
	failed []string // titles of the blocks that produced nothing but an error
}

func (s *sections) add(title string, fn func() Section) {
	sec := section(title, fn)
	if sec.Err != "" && sec.Body == "" {
		s.failed = append(s.failed, title)
	}
	s.list = append(s.list, sec)
}

// section runs one collector; a panic or error stays in its own section.
func section(title string, fn func() Section) (s Section) {
	defer func() {
		if r := recover(); r != nil {
			s = Section{Title: title, Err: fmt.Sprint("internal error: ", r)}
		}
	}()
	return fn()
}

// i2cDev loads i2c-dev when it is missing and says so. No hardware writes; this is the only system change diag
// makes (the tools it runs keep their own lock and cache files, as in normal use).
func (c *Ctl) i2cDev() string {
	if slices.Contains(moduleNames(c.Env.ProcModules), "i2c_dev") {
		return ""
	}
	if m, _ := filepath.Glob(filepath.Join(c.Env.Dev, "i2c-*")); len(m) > 0 {
		return ""
	}
	if _, err := runTimeout(toolTimeout, "modprobe", "-q", "i2c-dev"); err != nil {
		return "i2c-dev was not loaded and modprobe i2c-dev failed: " + err.Error()
	}
	return "loaded i2c-dev for the scan (modprobe i2c-dev, the only system change diag makes)"
}

// i2cAdapters are the names of the I2C adapters the kernel registered.
func (c *Ctl) i2cAdapters() []string {
	names, _ := filepath.Glob(filepath.Join(c.Env.SysI2C, "i2c-*", "name"))
	var out []string
	for _, f := range names {
		out = append(out, filepath.Base(filepath.Dir(f))+"="+readFirstLine(f))
	}
	sort.Slice(out, func(i, j int) bool { return natural(out[i]) < natural(out[j]) })
	return out
}

func (c *Ctl) ledDiag(r Report, h fan.Hardware) LEDDiag {
	model := c.ledModel()
	d := LEDDiag{Enabled: c.ledEnabled(), Supported: r.LED.Supported, Untested: ledUntested(model),
		DaemonRunning: c.running(c.Env.LEDPidFile), Blocker: c.ledBlocker(), Warning: c.ledWarning(),
		Bays: r.Bays, Slots: slotNames(h), GT: strings.HasPrefix(model, "DXP2800 GT"), Adapters: c.i2cAdapters()}
	if d.Blocker == "model" {
		d.Skipped = "not a UGREEN model"
		d.StatusSkipped = d.Skipped
		return d
	}
	d.Modprobe = c.i2cDev()
	d.Get, d.GetErr = errText(runTimeout(toolTimeout, c.Env.LEDI2C, "--plain", "get", "all"))
	d.Scan, d.ScanErr = errText(runTimeout(scanTimeout, c.Env.LEDI2C, "scan", "all"))
	if d.GT {
		d.CS201x, d.CS201xErr = errText(runTimeout(toolTimeout, c.Env.LEDI2C, "cs201x"))
	}
	// ugreen-led-ctl loads i2c_i801 when the adapter is missing; diag only loads i2c-dev, so it asks only when the
	// adapter is there (the GT has none).
	adapter := d.GT || r.LED.Probe == "ok"
	for _, a := range d.Adapters {
		if strings.Contains(a, "SMBus I801") || strings.Contains(a, "DesignWare") {
			adapter = true
		}
	}
	if !adapter {
		d.StatusSkipped = "no SMBus adapter registered (the tool would load i2c_i801, diag loads nothing but i2c-dev)"
	} else {
		d.Status, d.StatusErr = errText(runTimeout(scanTimeout, c.Env.LEDCtl, "status"))
	}
	return d
}

func errText(out string, err error) (string, string) {
	if err != nil {
		return out, err.Error()
	}
	return out, ""
}

func (c *Ctl) fanDiag(model string) FanDiag {
	d := FanDiag{DaemonRunning: c.running(c.Env.PidFile)}
	out, err := runTimeout(scanTimeout, c.Env.FanTool, "status")
	d.Status = []byte(out)
	if err != nil && out == "" {
		d.Err = err.Error()
	}
	run := func(tool, title, key string, args ...string) Result {
		out, err := runTimeout(scanTimeout, tool, args...)
		r := Result{Title: title, Out: out, Key: key}
		if err != nil {
			r.Err = err.Error()
		}
		return r
	}
	extra := func(title, key string, args ...string) {
		d.Extra = append(d.Extra, run(c.Env.FanTool, title, key, args...))
	}
	extra("probe-fans status (kind, map, proposal)", "probe", "probe-fans", "status")
	switch {
	case strings.HasPrefix(model, "DXP2800 GT"):
		extra("cs201x read (fan chip, read only)", "cs201x", "cs201x")
	case strings.HasPrefix(model, "iDX6011 Pro"):
		extra("backlight (read)", "backlight", "backlight")
	case fan.FanKind(model) == fan.KindProbe:
		extra("probe-fans tachs (every tach input, read only)", "tachs", "probe-fans", "tachs")
	}
	extra("power-on-ac (read)", "power-on-ac", "power-on-ac")
	d.Aux = append(d.Aux, run(c.Env.FanTool, "history", "history", "history", "1h"), run(c.Env.FanTool, "profile sources", "profiles", "profile", "sources"))
	if panel.ButtonsFor(model) != nil && c.Env.Panel != "" {
		d.Extra = append(d.Extra, run(c.Env.Panel, "front panel buttons (read only)", "panel-buttons", "panel", "buttons"))
	}
	return d
}

func (c *Ctl) diag() int {
	if runtime.GOOS != "linux" {
		c.errf("ERROR: diag reads an Unraid (Linux) box; this is %s.\n", runtime.GOOS)
		return 1
	}
	if os.Geteuid() != 0 {
		c.errf("ERROR: run diag as root on the Unraid box.\n")
		return 1
	}

	anon := Anon{Hostnames: hostnames()}
	now := time.Now()
	deadlineNano.Store(now.Add(diagBudget).UnixNano())
	defer deadlineNano.Store(0)

	var (
		in    Inputs
		rep   Report
		facts Facts
		secs  sections
		model string
	)
	// Everything that can panic runs under guard or section, so one broken read never costs the rest of the output.
	if msg := guard(func() {
		in = Inputs{Env: c.Env, Blocker: c.ledBlocker(), Rivals: c.rivalList()}
		in.Hardware = fan.ReadHardware()
		in.BayPorts = fan.BayPorts(in.Hardware.Model)
		if out, err := runTimeout(toolTimeout, c.Env.FanTool, "probe"); err == nil || out != "" {
			in.FanProbe = []byte(out)
		}
		if in.Blocker != "model" {
			in.LEDRan = true
			out, err := runTimeout(toolTimeout, c.Env.LEDI2C, "probe")
			in.LEDOK, in.LEDProbe = err == nil, []byte(out)
		}
		rep = Build(in)
	}); msg != "" {
		secs.add("Report", func() Section { return Section{Title: "Report", Err: msg} })
	}
	model = rep.Machine.Product

	facts.Report = rep
	if msg := guard(func() {
		facts.Env = EnvDiag{Plugins: dirNames(c.Env.PluginsBoot), Modules: moduleNames(c.Env.ProcModules)}
		facts.Env.Versions = plgVersions(c.Env.PluginsBoot, facts.Env.Plugins)
		facts.Env.OwnFiles = ownFiles(dirNames(filepath.Join(c.Env.PluginsBoot, "ugreen-nas-control")))
		facts.Env.Hwmon, facts.Env.DevI2C = hwmonNames(c.Env.Hwmon), i2cDevices(c.Env.Dev)
	}); msg != "" {
		secs.add("Environment facts", func() Section { return Section{Title: "Environment facts", Err: msg} })
	}

	secs.add("DMI", func() Section { return dmiSection(c.Env.DMIDir) })
	secs.add("LED", func() Section {
		facts.LED = c.ledDiag(rep, in.Hardware)
		return ledSection(facts.LED)
	})
	secs.add("Fans", func() Section {
		facts.Fan = c.fanDiag(model)
		facts.FanDaemonDown = !facts.Fan.DaemonRunning
		var st fanStatus
		if json.Unmarshal(facts.Fan.Status, &st) == nil {
			facts.FanSt = &st
		}
		return fanSection(facts.Fan)
	})
	secs.add("Other fan and LED tools", func() Section {
		facts.Rivals = c.rivalDiag(facts.FanSt)
		return rivalSection(facts.Rivals)
	})
	secs.add("Features", func() Section {
		return featuresSection(c.featureInputs(model, facts))
	})
	secs.add("Drives", func() Section {
		data, err := os.ReadFile(c.Env.DisksINI)
		return drivesSection(data, err, isFile(filepath.Join(c.Env.PluginsBoot, "unassigned.devices.plg")))
	})
	secs.add("Settings", func() Section {
		fanCfg, e1 := os.ReadFile(c.Env.FanCfg)
		ledCfg, e2 := os.ReadFile(c.Env.LEDCfg)
		return settingsSection(fanCfg, ledCfg, e1, e2, c.running(c.Env.PubPidFile))
	})
	var syslog string
	guard(func() { syslog = tail(c.Env.Syslog, 1<<20) })
	secs.add("Plugin state", func() Section {
		st := StateDiag{Version: rep.PluginVersion, OwnFiles: facts.Env.OwnFiles, CronPresent: isFile(c.Env.LEDCron)}
		if data, err := os.ReadFile(filepath.Join(c.Env.PluginsBoot, "ugreen-nas-control.plg")); err == nil {
			if m := entityRe("pluginURL").FindSubmatch(data); m != nil {
				st.PluginURL = scrub(string(m[1]))
			}
		}
		st.Daemons = []string{c.daemonLine("fan daemon", c.Env.PidFile), c.daemonLine("led daemon", c.Env.LEDPidFile), c.daemonLine("mqtt publisher", c.Env.PubPidFile)}
		for _, l := range filterLines(syslog, pluginLineRe, 10) {
			st.InstallLines = append(st.InstallLines, anon.Line(l))
		}
		return stateSection(st)
	})
	secs.add("Environment", func() Section { return envSection(facts.Env) })
	secs.add("hwmon", func() Section { return hwmonSection(c.Env.Hwmon) })
	secs.add("I/O ports", func() Section { return ioportsSection(c.Env.Proc) })
	secs.add("PCI", func() Section {
		out, err := runTimeout(toolTimeout, "lspci", "-nn")
		return lspciSection(anon, out, errString(err))
	})
	secs.add("dmesg", func() Section {
		out, err := runTimeout(toolTimeout, "dmesg")
		return dmesgSection(anon, out, errString(err))
	})
	secs.add("Log", func() Section {
		if syslog == "" {
			return logSection(nil, "cannot read the syslog")
		}
		return logSection(logLines(anon, syslog, 50), "")
	})
	facts.Failed = secs.failed

	var checks []string
	list := secs.list
	if msg := guard(func() {
		var reg *RegModel
		if models, err := loadRegistry(modelsJSON); err == nil {
			reg = matchModel(models, model)
		}
		ev := BuildEvidence(EvInputs{Reg: reg, Now: now, Report: rep, Fan: facts.Fan, FanSt: facts.FanSt, LED: facts.LED, Rivals: facts.Rivals})
		modelSec, modelChecks := modelSection(reg, ev)
		list = append(append([]Section{modelSec}, list...), evidenceSection(ev))
		checks = modelChecks
	}); msg != "" {
		list = append(list, Section{Title: "Model", Err: msg})
	}
	var out string
	if msg := guard(func() {
		checks = append(diagChecks(facts), checks...)
		out = RenderDiag(anon, now, kernelVersion(c.Env.Proc), rep, checks, list)
	}); msg != "" {
		c.errf("ERROR: diag failed while writing the output: %s\n", msg)
		return 1
	}
	c.out("%s", out)
	return 0
}

var pluginLineRe = regexp.MustCompile(`(?i)plugin: .*ugreen-nas-control|ugreen-nas-control.*plugin`)

// guard runs fn and returns the panic as text, "" when fn returned normally.
func guard(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint("internal error: ", r)
		}
	}()
	fn()
	return ""
}

var ownFileNames = []string{"fan.cfg", "leds.cfg", "fan_rpm.json", "led.enabled", "ugreen-nas-control.cron"}

// ownFiles keeps the names the plugin writes itself; a file somebody else put there shows nowhere.
func ownFiles(names []string) []string {
	return slices.DeleteFunc(names, func(n string) bool { return !slices.Contains(ownFileNames, n) })
}

func errString(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

func i2cDevices(dev string) []string {
	m, _ := filepath.Glob(filepath.Join(dev, "i2c-*"))
	for i := range m {
		m[i] = filepath.Base(m[i])
	}
	return m
}

// featureInputs gathers what the Features section needs besides the facts the other sections collected.
func (c *Ctl) featureInputs(model string, f Facts) FeatureInputs {
	in := FeatureInputs{Model: model, Fan: f.Fan, FanSt: f.FanSt, LED: f.LED, Rivals: f.Rivals, LEDEnabled: f.LED.Enabled,
		LEDImport: isFile(c.Env.LEDImportSrc), MQTTRunning: c.running(c.Env.PubPidFile),
		Dashboard: isFile(filepath.Join(c.Env.PluginsWeb, "ugreen-nas-control", "UGREENNASDashboard.page"))}
	in.FanCfg, in.MQTT = fan.LoadConfig(c.Env.FanCfg), mqtt.LoadConfig(c.Env.FanCfg)
	if data, err := os.ReadFile(c.Env.FanCfg); err == nil {
		in.FanCfgRaw, in.FanCfgPresent = cfgValues(data), true
	}
	if data, err := os.ReadFile(c.Env.LEDCfg); err == nil {
		in.LEDCfgRaw = cfgValues(data)
	}
	if f.LED.Status != "" {
		in.LEDSt = parseKV(f.LED.Status)
	}
	in.MQTTLog = tail(c.Env.PubLog, 4096)
	in.Scripts = scriptInterpreters(c.Env.SensorsDir)
	return in
}
