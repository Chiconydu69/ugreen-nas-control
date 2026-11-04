package nasctl

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/ledctl"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/mqtt"
)

// Result is the answer of one read-only tool call.
type Result struct {
	Title, Out, Err string
	Key             string // what the Features section looks it up by
}

func short(s string, n int) string { return cut(strings.Join(strings.Fields(s), " "), n) }

// cut shortens s to n bytes at a rune boundary and marks the cut.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}

// dmiNames are the DMI files that name the model; serial numbers, UUID and asset tags are never read.
var dmiNames = []string{"sys_vendor", "product_name", "product_family", "product_version", "board_vendor", "board_name", "board_version",
	"bios_vendor", "bios_version", "bios_date", "bios_release", "chassis_vendor", "chassis_type", "chassis_version"}

func dmiSection(dir string) Section {
	var l []string
	for _, n := range dmiNames {
		if v := readFirstLine(filepath.Join(dir, n)); v != "" {
			l = append(l, n+": "+v)
		}
	}
	if len(l) == 0 {
		return Section{Title: "DMI", Err: "no DMI data under " + filepath.Base(dir)}
	}
	return Section{Title: "DMI (no serial numbers, UUID or asset tags)", Body: strings.Join(l, "\n")}
}

var hwmonFileRe = regexp.MustCompile(`^(fan\d+_(input|label)|pwm\d+(_enable|_mode)?|temp\d+_(input|label))$`)

// hwmonSection lists every hwmon device with its fan, pwm and temperature entries and their current values.
func hwmonSection(dir string) Section {
	devs, _ := filepath.Glob(filepath.Join(dir, "hwmon*"))
	sort.Slice(devs, func(i, j int) bool { return natural(devs[i]) < natural(devs[j]) })
	var l []string
	for _, d := range devs {
		name := readFirstLine(filepath.Join(d, "name"))
		files, _ := filepath.Glob(filepath.Join(d, "*"))
		var vals []string
		for _, f := range files {
			b := filepath.Base(f)
			if !hwmonFileRe.MatchString(b) {
				continue
			}
			if v := readFirstLine(f); v != "" {
				vals = append(vals, b+"="+v)
			}
		}
		sort.Slice(vals, func(i, j int) bool { return natural(vals[i]) < natural(vals[j]) })
		if len(vals) > 40 {
			vals = vals[:40]
		}
		l = append(l, fmt.Sprintf("%s %s: %s", filepath.Base(d), orDash(name), orDash(strings.Join(vals, " "))))
	}
	if len(l) == 0 {
		return Section{Title: "hwmon", Err: "no hwmon devices"}
	}
	return Section{Title: "hwmon (fan, pwm and temperature entries)", Body: strings.Join(l, "\n")}
}

var numRe = regexp.MustCompile(`\d+`)

// natural makes hwmon2 sort before hwmon10 and fan2 before fan10.
func natural(s string) string {
	return numRe.ReplaceAllStringFunc(s, func(n string) string { return fmt.Sprintf("%08s", n) })
}

var ioportsRe = regexp.MustCompile(`(?i)^\s*(002e|004e|0a[0-3][0-9a-f]|0290|0062|0066)\b|it8|ite|nct|super`)

func ioportsSection(proc string) Section {
	data, err := os.ReadFile(filepath.Join(proc, "ioports"))
	if err != nil {
		return Section{Title: "I/O ports", Err: err.Error()}
	}
	return Section{Title: "I/O ports (Super-I/O and EC ranges)", Body: orDashLines(filterLines(string(data), ioportsRe, 40), "no Super-I/O or EC range (002e, 004e, 0a00-0a3f, 0290, 0062, 0066) is claimed by a driver")}
}

func orDashLines(lines []string, none ...string) string {
	if len(lines) == 0 {
		if len(none) > 0 {
			return none[0]
		}
		return "-"
	}
	return strings.Join(lines, "\n")
}

// filterLines keeps the last n lines that match re.
func filterLines(text string, re *regexp.Regexp, n int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if re.MatchString(line) {
			out = append(out, strings.TrimRight(line, " \t\r"))
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

var (
	dmesgRe = regexp.MustCompile(`(?i)it87|it86|\bite\b|nct67|super.?i/o|i2c|led_ugreen|ugreen`)
	lspciRe = regexp.MustCompile(`(?i)smbus|i2c|sata|nvm|non-volatile|\bsas\b|raid|ethernet|network|isa bridge`)
)

func dmesgSection(a Anon, out string, err string) Section {
	var lines []string
	for _, l := range filterLines(out, dmesgRe, 40) {
		lines = append(lines, cut(a.Line(l), 300))
	}
	return Section{Title: "dmesg (chip, I2C and UGREEN lines)", Body: orDashLines(lines), Err: err}
}

func lspciSection(a Anon, out, err string) Section {
	var lines []string
	for _, l := range filterLines(out, lspciRe, 40) {
		lines = append(lines, a.Text(l))
	}
	return Section{Title: "PCI (SMBus, storage, network; vendor:device IDs)", Body: orDashLines(lines), Err: err}
}

// Drive is one line of the drives summary, from Unraid's disks.ini.
type Drive struct{ Role, Type, Transport, Rotational, Spundown, Temp, Status string }

var iniSectionRe = regexp.MustCompile(`^\["?([^"\]]*)"?\]$`)

// parseDrives keeps role, type, transport, rotational, spundown, temp and status per slot. No id, device, serial,
// size or label; a pool name is chosen by the owner, so it shows as pool1, pool2.
func parseDrives(data []byte) []Drive {
	var out []Drive
	var cur map[string]string
	pool := 0
	flush := func() {
		if cur == nil {
			return
		}
		name, typ := cur["name"], cur["type"]
		role := name
		switch {
		case typ == "Flash":
			role = "flash"
		case typ == "Cache" && name != "cache":
			pool++
			role = fmt.Sprintf("pool%d", pool)
		case !slotRe.MatchString(name):
			role = "slot"
		}
		out = append(out, Drive{role, typ, cur["transport"], cur["rotational"], cur["spundown"], cur["temp"], cur["status"]})
		cur = nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if m := iniSectionRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = map[string]string{}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && cur != nil {
			cur[k] = strings.Trim(v, `"`)
		}
	}
	flush()
	return out
}

func drivesSection(data []byte, readErr error, unassigned bool) Section {
	if readErr != nil {
		return Section{Title: "Drives", Err: readErr.Error()}
	}
	var l []string
	for _, d := range parseDrives(data) {
		if strings.HasPrefix(d.Status, "DISK_NP") || d.Status == "" {
			l = append(l, d.Role+": empty")
			continue
		}
		kind := "ssd"
		if d.Rotational == "1" {
			kind = "hdd"
		}
		l = append(l, fmt.Sprintf("%s: %s %s %s, spundown %s, temp %s, %s", d.Role, d.Type, d.Transport, kind, orDash(d.Spundown), orDash(d.Temp), d.Status))
	}
	l = append(l, fmt.Sprintf("Unassigned Devices plugin installed: %t", unassigned))
	return Section{Title: "Drives (no serials, ids, models or labels)", Body: strings.Join(l, "\n")}
}

// privateKeyRe: a key whose name says it holds a credential or an address; its value is never printed, whatever the
// key tables say.
var privateKeyRe = regexp.MustCompile(`(?i)pass|token|secret|user|host`)

// keyClass says for one settings key whether it is known (a key the code reads, from the parsers' own tables) and
// whether its value is private.
type keyClass func(key string) (known, private bool)

// fanCfgKey: the keys of fan.cfg are fan.KnownSetting (what the fan daemon reads), mqtt.Keys and fan.UISettings.
// ignore_drives names the owner's drives and stays private.
func fanCfgKey(k string) (known, private bool) {
	switch {
	case k == "ignore_drives":
		return true, true
	case slices.Contains(fan.UISettings, k):
		return true, false
	}
	if known, private := mqtt.KnownKey(k); known {
		return true, private
	}
	return fan.KnownSetting(k), false
}

// ledCfgKey: the keys of leds.cfg are ledctl.KnownKey (what the LED daemon reads) and fan.UISettings.
func ledCfgKey(k string) (known, private bool) {
	if slices.Contains(fan.UISettings, k) {
		return true, false
	}
	return ledctl.KnownKey(k)
}

// cfgValues is the last value of each key of a key="value" file.
func cfgValues(data []byte) map[string]string {
	last := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := cfgLineRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			last[m[1]] = m[2]
		}
	}
	return last
}

var cfgLineRe = regexp.MustCompile(`^([A-Za-z0-9_.]+)="(.*)"$`)

// redactCfg lists the key="value" lines of a settings file, sorted; the last line of a key wins. A known key prints
// its value. A private key prints only whether it is set, an unknown key prints "<hidden>". A fan input prints its
// kind only (script and NIC inputs carry a name).
func redactCfg(data []byte, limit int, class keyClass) []string {
	last := cfgValues(data)
	order := make([]string, 0, len(last))
	for k := range last {
		order = append(order, k)
	}
	sort.Strings(order)
	var out []string
	for _, k := range order {
		v := last[k]
		known, private := class(k)
		switch {
		case !known:
			out = append(out, k+": <hidden>")
			continue
		case private || privateKeyRe.MatchString(k):
			set := "(set)"
			if v == "" {
				set = "(empty)"
			}
			out = append(out, k+": <hidden> "+set)
			continue
		case k == "input" || strings.HasSuffix(k, "_input") || strings.HasSuffix(k, ".input"):
			v = inputKinds(v)
		}
		out = append(out, k+"="+short(v, 200))
	}
	if len(out) > limit {
		out = append(out[:limit], "...")
	}
	return out
}

// inputKinds keeps the kinds of an input spec: "nic:eth0" -> "nic", "max:cpu|script:x" -> "max:cpu|script".
func inputKinds(spec string) string {
	kind, rest, ok := strings.Cut(spec, ":")
	if !ok {
		return kind
	}
	if kind == "max" {
		parts := strings.Split(rest, "|")
		for i, p := range parts {
			parts[i], _, _ = strings.Cut(p, ":")
		}
		return "max:" + strings.Join(parts, "|")
	}
	return kind
}

func settingsSection(fanCfg, ledCfg []byte, fanErr, ledErr error, mqttRunning bool) Section {
	var l []string
	add := func(title string, data []byte, err error, class keyClass) {
		l = append(l, title+":")
		if err != nil {
			l = append(l, "  not readable: "+err.Error())
			return
		}
		for _, line := range redactCfg(data, 400, class) {
			l = append(l, "  "+line)
		}
	}
	add("fan.cfg", fanCfg, fanErr, fanCfgKey)
	add("leds.cfg", ledCfg, ledErr, ledCfgKey)
	l = append(l, fmt.Sprintf("MQTT publisher running: %t", mqttRunning))
	return Section{Title: "Settings (every known key; credentials, drive names and unknown keys hidden)", Body: strings.Join(l, "\n")}
}

// procUptime is how long a process has run: its start time in clock ticks (100 per second) against the uptime.
func procUptime(proc string, pid int) (time.Duration, bool) {
	stat, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, false
	}
	s := string(stat)
	rest := s[strings.LastIndex(s, ")")+1:]
	f := strings.Fields(rest)
	if len(f) < 20 {
		return 0, false
	}
	start, err := strconv.ParseFloat(f[19], 64)
	if err != nil {
		return 0, false
	}
	up, err := os.ReadFile(filepath.Join(proc, "uptime"))
	if err != nil {
		return 0, false
	}
	now, err := strconv.ParseFloat(strings.Fields(string(up) + " ")[0], 64)
	if err != nil || now < start/100 {
		return 0, false
	}
	return time.Duration((now - start/100) * float64(time.Second)).Round(time.Second), true
}

// daemonLine says whether a daemon runs, with its pid and uptime.
func (c *Ctl) daemonLine(name, pidFile string) string {
	if !c.running(pidFile) {
		return name + ": stopped"
	}
	pidText := c.pidText(pidFile)
	line := fmt.Sprintf("%s: running pid %s", name, pidText)
	if pid, err := strconv.Atoi(pidText); err == nil {
		if d, ok := procUptime(c.Env.Proc, pid); ok {
			line += ", up " + d.String()
		}
	}
	return line
}

var entityRe = func(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)<!ENTITY\s+` + name + `\s+"([^"]+)"`)
}

// pluginChannel: a "-next." version is the next channel.
func pluginChannel(version string) string {
	if strings.Contains(version, "-next.") {
		return "next"
	}
	if version == "" {
		return "unknown"
	}
	return "stable"
}

// StateDiag is what the plugin state section is made of.
type StateDiag struct {
	Version, PluginURL string
	OwnFiles           []string
	CronPresent        bool
	Daemons            []string
	InstallLines       []string
}

func stateSection(s StateDiag) Section {
	l := []string{
		fmt.Sprintf("installed version: %s  channel: %s", orDash(s.Version), pluginChannel(s.Version)),
		"pluginURL: " + orDash(s.PluginURL),
		"files in /boot/config/plugins/ugreen-nas-control: " + orDash(strings.Join(s.OwnFiles, " ")),
		fmt.Sprintf("cron file present: %t", s.CronPresent),
	}
	l = append(l, s.Daemons...)
	l = append(l, "plugin lines in the syslog:")
	for _, x := range s.InstallLines {
		l = append(l, "  "+x)
	}
	if len(s.InstallLines) == 0 {
		l = append(l, "  -")
	}
	return Section{Title: "Plugin state", Body: strings.Join(l, "\n")}
}

// plgVersions reads the version each interesting .plg declares.
func plgVersions(dir string, names []string) map[string]string {
	out := map[string]string{}
	ver := entityRe("version")
	for _, n := range names {
		if !strings.HasSuffix(n, ".plg") || !pluginInterest.MatchString(n) {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
			if m := ver.FindSubmatch(data); m != nil {
				out[n] = scrub(string(m[1]))
			}
		}
	}
	return out
}

// leftoverFolders: a plugin folder of a known LED/fan tool without its .plg is what an uninstall left behind.
var leftoverRe = regexp.MustCompile(`(?i)ugreen|fanctrl|autofan|it87`)

func leftoverFolders(names []string) []string {
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	var out []string
	for _, n := range names {
		base := strings.TrimSuffix(n, "/")
		if !strings.HasSuffix(n, "/") || base == "ugreen-nas-control" || !leftoverRe.MatchString(base) {
			continue
		}
		if !have[base+".plg"] {
			out = append(out, n)
		}
	}
	return out
}

// Facts are what the checks summary is made of.
type Facts struct {
	Report        Report
	LED           LEDDiag
	Fan           FanDiag
	FanSt         *fanStatus
	Env           EnvDiag
	Failed        []string // titles of the sections that failed
	FanDaemonDown bool
	Rivals        RivalDiag
}

// diagChecks is one line per detected problem, so a maintainer sees the issue at a glance.
func diagChecks(f Facts) []string {
	out := rivalChecks(f.Rivals)
	r, led := f.Report, f.LED
	switch {
	case led.Blocker == "model":
		out = append(out, "LED: not a UGREEN model")
	case led.Blocker != "":
		out = append(out, "LED blocked: "+led.Blocker)
	case !led.Enabled:
		out = append(out, "LED control is off")
	}
	if led.Warning != "" {
		out = append(out, "LED: UGREEN LED Control is installed too ("+led.Warning+")")
	}
	if led.Untested && led.Blocker != "model" {
		out = append(out, "model untested: the LED bay order is a guess")
	}
	if strings.Contains(led.Modprobe, "failed") {
		out = append(out, "i2c-dev could not be loaded")
	} else if led.Modprobe != "" {
		out = append(out, "i2c-dev was not loaded (diag loaded it for the scan)")
	}
	if led.Blocker == "" && r.LED.Probe == "failed" {
		out = append(out, "LED controller probe failed: "+orDash(r.LED.Error))
	}
	if led.Blocker == "" && r.LED.Probe == "failed" && !led.GT && led.Skipped == "" && led.ScanErr == "" && led.Scan != "" && !strings.Contains(strings.ToLower(led.Scan), "0x3a") {
		out = append(out, "no I2C device answers at 0x3a")
	}
	if led.ScanErr != "" && led.Skipped == "" {
		out = append(out, "I2C scan failed: "+led.ScanErr)
	}
	if !r.Fan.Found {
		out = append(out, "fan chip not found: "+orDash(r.Fan.Error))
	}
	if st := f.FanSt; st != nil {
		if st.FanControl != nil && !*st.FanControl {
			out = append(out, "fan control is off in the settings")
		}
		if st.ChipError != "" {
			out = append(out, "fan chip error: "+st.ChipError)
		}
		for _, c := range st.Conflicts {
			if c.Severity == "block" && !slices.ContainsFunc(f.Rivals.Procs, func(p RivalProc) bool { return p.Program == c.ID }) {
				out = append(out, "rival running: "+c.Kind+" "+c.ID)
			}
		}
		if w := st.Watchdog; w != nil && (w.State == "action_needed" || w.State == "not_counting" || w.State == "not_effective") {
			out = append(out, "watchdog: "+w.State)
		}
		if dm := st.Daemon; dm != nil && dm.Failsafe != "" {
			out = append(out, "fan failsafe active: "+dm.Failsafe)
		}
	}
	if f.FanDaemonDown && f.FanSt != nil && f.FanSt.FanControl != nil && *f.FanSt.FanControl {
		out = append(out, "fan daemon not running")
	}
	for _, m := range f.Env.Modules {
		switch m {
		case "it87", "led_ugreen":
			out = append(out, m+" loaded: may fight over the "+map[string]string{"it87": "fan chip", "led_ugreen": "LED controller"}[m])
		case "it87_wdt":
			out = append(out, "it87_wdt loaded: it owns the watchdog timer")
		}
	}
	for _, d := range leftoverFolders(f.Env.Plugins) {
		out = append(out, "leftover plugin folder: "+d)
	}
	for _, t := range f.Failed {
		out = append(out, "section failed: "+t)
	}
	return out
}
