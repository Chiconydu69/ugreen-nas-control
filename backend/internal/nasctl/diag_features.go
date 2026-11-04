package nasctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/mqtt"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/panel"
)

// The Features section has one line per feature of the plugin: "<id>: state and effective values", or
// "<id>: n/a for this model". Every feature the docs describe is in featureTable; TestEveryFeatureHasADiagLine
// (diag_test.go) lists the ids that must appear, so a new feature fails that test until it has a line here:
//
//  1. add a row to featureTable (id, the doc that describes it, the function that writes the line);
//  2. add the id to documentedFeatures in diag_test.go;
//  3. add the row to the feature table in docs/cli.md.
//
// A line shows effective values (the fan settings through fan.LoadConfig, the LED settings from `ugreen-led-ctl
// status`), never a user-chosen name, path or credential.

// FeatureInputs is everything the lines are made of; diag fills it, the lines only format.
type FeatureInputs struct {
	Model         string
	Fan           FanDiag
	FanSt         *fanStatus
	FanCfg        fan.Config // effective, as the daemon loads it
	FanCfgRaw     map[string]string
	FanCfgPresent bool
	MQTT          mqtt.Config // effective, as the publisher loads it; host, login and name never print
	LED           LEDDiag
	LEDSt         kv // `ugreen-led-ctl status`; nil when it did not answer
	LEDCfgRaw     map[string]string
	LEDEnabled    bool
	LEDImport     bool // the other LED plugin's leds.cfg is there to import
	Rivals        RivalDiag
	MQTTRunning   bool
	MQTTLog       string // tail of the publisher log
	Scripts       []string
	Dashboard     bool
}

type feature struct {
	ID   string
	Doc  string // the doc that describes it
	Line func(in FeatureInputs) string
}

// na is the text of a feature the model does not have.
const na = "n/a for this model"

var featureTable = []feature{
	{"led.control", "led.md", ledControl},
	{"led.daemon", "led.md", ledDaemon},
	{"led.live", "led.md", ledLive},
	{"led.status_mode", "led.md", ledStatusMode},
	{"led.disk_activity", "led.md", func(in FeatureInputs) string { return ledFlag(in, "activity_enabled", "disk activity") }},
	{"led.smart", "led.md", ledSmart},
	{"led.disk_errors", "led.md", ledDiskErrors},
	{"led.standby", "led.md", ledStandby},
	{"led.netdev", "led.md", ledNetdev},
	{"led.night", "led.md", ledNight},
	{"led.alerts", "led.md", ledAlerts},
	{"led.sync", "led.md", ledSync},
	{"led.shutdown", "led.md", ledShutdown},
	{"led.effects", "led.md", ledEffects},
	{"led.identify", "led.md", ledIdentify},
	{"led.import", "led.md", func(in FeatureInputs) string {
		return fmt.Sprintf("leds.cfg of the UGREEN LED Control plugin available to import: %t", in.LEDImport)
	}},
	{"fan.control", "fan.md", fanControl},
	{"fan.daemon", "fan.md", fanDaemon},
	{"fan.mode", "fan.md", fanMode},
	{"fan.curves", "fan.md", fanCurves},
	{"fan.inputs", "fan.md", fanInputs},
	{"fan.excluded_drives", "fan.md", fanExcluded},
	{"fan.limits", "fan.md", fanLimits},
	{"fan.failsafe", "fan.md", fanFailsafe},
	{"fan.quiet", "fan.md", fanQuiet},
	{"fan.jobs", "fan.md", fanJobs},
	{"fan.shaping", "fan.md", fanShaping},
	{"fan.calibration", "fan.md", fanCalibration},
	{"fan.history", "fan.md", fanHistory},
	{"fan.map", "fan.md", fanMap},
	{"fan.profiles", "fan.md", fanProfiles},
	{"fan.scripts", "fan.md", fanScripts},
	{"watchdog", "watchdog.md", watchdogLine},
	{"power_on_ac", "power-on-ac.md", powerOnAC},
	{"panel.buttons", "panel.md", panelButtons},
	{"panel.beeper", "panel.md", panelBeeper},
	{"lcd.backlight", "fan.md", lcdBacklight},
	{"mqtt", "mqtt.md", mqttLine},
	{"conflicts", "conflicts.md", conflictsLine},
	{"ui.dashboard", "README.md", func(in FeatureInputs) string { return fmt.Sprintf("dashboard tile installed: %t", in.Dashboard) }},
	{"ui.language_theme", "translations.md", uiLine},
}

func featuresSection(in FeatureInputs) Section {
	var l []string
	for _, f := range featureTable {
		text := ""
		if msg := guard(func() { text = f.Line(in) }); msg != "" {
			text = "ERROR: " + msg
		}
		l = append(l, f.ID+": "+text)
	}
	return Section{Title: "Features (state and effective settings)", Body: strings.Join(l, "\n")}
}

// kv is a JSON object whose values print as text.
type kv map[string]json.RawMessage

func parseKV(s string) kv {
	var m kv
	if json.Unmarshal([]byte(s), &m) != nil {
		return nil
	}
	return m
}

// s is the value of key as text; "-" when it is missing or empty.
func (m kv) s(key string) string {
	raw, ok := m[key]
	if !ok {
		return "-"
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return orDash(str)
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) != nil {
		return orDash(strings.TrimSpace(string(raw)))
	}
	return buf.String()
}

func (m kv) list(keys ...string) string {
	var out []string
	for _, k := range keys {
		out = append(out, k+" "+m.s(k))
	}
	return strings.Join(out, ", ")
}

func cfgPairs(m map[string]string, keys ...string) string {
	var out []string
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out = append(out, k+"="+v)
		}
	}
	return orDash(strings.Join(out, " "))
}

// ledNA is the n/a text when the model has no LED controller the plugin drives.
func ledNA(in FeatureInputs) (string, bool) {
	if in.LED.Blocker == "model" {
		return na + " (not a UGREEN model)", true
	}
	return "", false
}

// ledFrom: the effective values of the keys in `ugreen-led-ctl status`, else the configured ones.
func ledFrom(in FeatureInputs, statusKeys []string, cfgKeys ...string) string {
	if in.LEDSt != nil {
		return in.LEDSt.list(statusKeys...)
	}
	return "status unavailable, configured: " + cfgPairs(in.LEDCfgRaw, cfgKeys...)
}

func ledControl(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return fmt.Sprintf("enabled %t, supported %t, blocker %s, untested model %t, warning %s", in.LEDEnabled, in.LED.Supported, orDash(in.LED.Blocker), in.LED.Untested, orDash(in.LED.Warning))
}

func ledDaemon(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return fmt.Sprintf("running %t, %s", in.LED.DaemonRunning, ledFrom(in, []string{"mode", "mapping", "foreign_monitor"}, "mode"))
}

// ledLive: one entry per LED from `ugreen-led-i2c get all`: name mode brightness R,G,B.
func ledLive(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	if in.LED.Skipped != "" || in.LED.GetErr != "" {
		return "not read: " + orDash(in.LED.Skipped+in.LED.GetErr)
	}
	var leds []string
	for _, line := range strings.Split(in.LED.Get, "\n") {
		f := strings.Fields(line)
		if len(f) >= 6 {
			leds = append(leds, fmt.Sprintf("%s %s/%s/%s,%s,%s", f[0], f[1], f[2], f[3], f[4], f[5]))
		}
	}
	return fmt.Sprintf("%d LEDs read (name mode/brightness/R,G,B): %s", len(leds), orDash(strings.Join(leds, "; ")))
}

func ledStatusMode(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"mode", "disk_color_mode"}, "mode", "disk_color_mode")
}

func ledFlag(in FeatureInputs, key, what string) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return what + ": " + ledFrom(in, []string{key}, key)
}

func ledSmart(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"smart_enabled", "smart_color"}, "smart_enabled", "smart_color")
}

func ledDiskErrors(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"error_enabled", "disk_error_color"}, "error_enabled", "disk_error_color")
}

func ledStandby(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"standby_mode", "standby_level"}, "standby_mode", "standby_level")
}

func ledNetdev(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"netdev_status", "netdev_speed_enabled", "network_device", "netdev_speed", "netdev_speed_colors"},
		"netdev_status", "netdev_speed_enabled", "netdev_color")
}

func ledNight(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	cfg := cfgPairs(in.LEDCfgRaw, "night_enabled", "night_start", "night_end", "night_action", "night_brightness")
	phase := "unknown"
	if in.LEDSt != nil {
		phase = map[bool]string{true: "night (dimmed or off)", false: "day"}[in.LEDSt.s("night") == "true"]
	}
	return "schedule: " + cfg + "; current phase: " + phase
}

func ledAlerts(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"alert_enabled", "alert_level", "alert_color", "alert_count", "alert_active"}, "alert_enabled", "alert_level", "alert_color")
}

func ledSync(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"sync_enabled", "sync_color", "sync_action", "sync_percent"}, "sync_enabled", "sync_color")
}

func ledShutdown(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"shutdown_blink", "shutdown_effect", "shutdown_color"}, "shutdown_blink", "shutdown_effect", "shutdown_color")
}

func ledEffects(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"fx", "fx_leds", "fx_speed", "fx_reverse", "fx_idle"}, "fx_name", "fx_leds", "fx_speed", "fx_reverse", "fx_idle")
}

func ledIdentify(in FeatureInputs) string {
	if t, ok := ledNA(in); ok {
		return t
	}
	return ledFrom(in, []string{"identify"}, "identify")
}

// fanNA is the n/a text when the model has no fan control the plugin offers.
func fanNA(in FeatureInputs) (string, bool) {
	if _, ok := fan.CapsFor(in.Model); !ok {
		return na + " (no fan control)", true
	}
	return "", false
}

func fanControl(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	caps, _ := fan.CapsFor(in.Model)
	on := "unknown"
	if in.FanSt != nil && in.FanSt.FanControl != nil {
		on = fmt.Sprint(*in.FanSt.FanControl)
	}
	chip := "-"
	if in.FanSt != nil {
		chip = orDash(in.FanSt.Chip)
	}
	return fmt.Sprintf("on %s (setting %t), kind %s, %d channels, chip %s, min speed floor %d%%", on, in.FanCfg.FanControl, caps.Kind, caps.Channels, chip, caps.MinPctFloor)
}

func fanDaemon(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	if in.FanSt == nil || in.FanSt.Daemon == nil {
		return fmt.Sprintf("running %t, no state yet", in.Fan.DaemonRunning)
	}
	d := in.FanSt.Daemon
	return fmt.Sprintf("running %t, mode %s, job %s, yielding to %s", in.Fan.DaemonRunning, orDash(d.Mode), orDash(d.Job), orDash(strings.Join(d.Conflict, ",")))
}

func fanMode(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	if !in.FanCfgPresent {
		return "no fan.cfg, defaults: " + in.FanCfg.Mode
	}
	return fmt.Sprintf("%s (config version %d), case fans follow the CPU curve too: %t, warnings: %s", in.FanCfg.Mode, in.FanCfg.Version, in.FanCfg.CaseFollowsCPU, orDash(strings.Join(cfgWarnings(in.FanCfg), "; ")))
}

var warnKeyRe = regexp.MustCompile(`^setting ([^=]+)=`)

// cfgWarnings keeps the key of each warning; the rejected value may be a user-chosen name.
func cfgWarnings(c fan.Config) []string {
	var out []string
	for _, w := range c.Warnings {
		if m := warnKeyRe.FindStringSubmatch(w); m != nil {
			w = "setting " + m[1] + " is not valid, default used"
		}
		out = append(out, w)
	}
	return out
}

func fanCurves(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	return fmt.Sprintf("cpu %s; case %s", in.FanCfg.CPU.Curve, in.FanCfg.Case.Curve)
}

// inputKind: the kind of an input spec; the interface or script name stays out.
func inputKind(spec string) string { return inputKinds(spec) }

func fanInputs(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	c := in.FanCfg
	l := []string{"cpu " + inputKind(c.CPU.Input.String()), "case " + inputKind(c.Case.Input.String())}
	ids := make([]string, 0, len(c.Channels))
	for id := range c.Channels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		l = append(l, id+" "+inputKind(c.Channels[id].Input.String()))
	}
	missing := "-"
	if in.FanSt != nil && in.FanSt.Daemon != nil {
		missing = orDash(strings.Join(in.FanSt.Daemon.Missing, ","))
	}
	return strings.Join(l, ", ") + "; inputs missing now: " + missing
}

func fanExcluded(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	return fmt.Sprintf("%d drives excluded (names hidden)", len(in.FanCfg.IgnoreDrives))
}

func fanLimits(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	c := in.FanCfg
	return fmt.Sprintf("min speed %d%%, cpu critical %d C, disk critical %d C, nvme offset %d C, notify on stalled fan %t", c.MinPct, c.CPUCrit, c.DiskCrit, c.NVMeOffset, c.StallNotify)
}

func fanFailsafe(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	if in.FanSt == nil || in.FanSt.Daemon == nil {
		return "no daemon state"
	}
	d := in.FanSt.Daemon
	return fmt.Sprintf("active: %s, stalled fans: %s, unapplied: %s", orDash(d.Failsafe), orDash(strings.Join(d.Stalled, ",")), orDash(d.Unapplied))
}

func fanQuiet(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	q := in.FanCfg.Quiet
	active := "unknown"
	if in.FanSt != nil && in.FanSt.Daemon != nil {
		active = fmt.Sprint(in.FanSt.Daemon.Quiet)
	}
	return fmt.Sprintf("enabled %t, %s to %s, days %s, cap %d%%, scope %s, active now %s", q.Enabled, fan.Clock(q.From), fan.Clock(q.To), q.DaysString(), q.Cap, q.Scope, active)
}

func fanJobs(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	c := in.FanCfg
	return fmt.Sprintf("mover %s %d%%, parity check %s %d%%", c.Mover.Mode, c.Mover.Pct, c.Parity.Mode, c.Parity.Pct)
}

func fanShaping(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	g := func(n string, g fan.Group) string {
		return fmt.Sprintf("%s: dead zone %d%%, kick-start %d%% for %d ms, min run %d%%, ramp up %d down %d per tick, may stop %t, input missing -> %s",
			n, g.Deadband, g.StartPct, g.StartMS, g.MinRun, g.UpStep, g.DownStep, g.AllowOff, g.OnMissing)
	}
	return g("cpu", in.FanCfg.CPU) + "; " + g("case", in.FanCfg.Case) + fmt.Sprintf("; fans with their own settings: %d", len(in.FanCfg.Channels))
}

func fanCalibration(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	if in.FanSt == nil {
		return "no fan status"
	}
	return calibrationText(*in.FanSt)
}

// result finds a read-only tool answer by key, among the shown and the hidden ones.
func (d FanDiag) result(key string) (Result, bool) {
	for _, r := range append(append([]Result{}, d.Extra...), d.Aux...) {
		if r.Key == key {
			return r, true
		}
	}
	return Result{}, false
}

func fanHistory(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	r, ok := in.Fan.result("history")
	if !ok || r.Err != "" {
		return "not read: " + orDash(r.Err)
	}
	var h struct {
		OK      bool    `json:"ok"`
		Running bool    `json:"running"`
		T       []int64 `json:"t"`
	}
	if json.Unmarshal([]byte(r.Out), &h) != nil {
		return "answer is not JSON"
	}
	return fmt.Sprintf("daemon buffer answers %t, %d points in the last hour (10 s steps, RAM only)", h.Running, len(h.T))
}

func fanMap(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	caps, _ := fan.CapsFor(in.Model)
	if caps.Kind != fan.KindProbe {
		return fmt.Sprintf("n/a for this model (fixed fan map, kind %s)", caps.Kind)
	}
	r, ok := in.Fan.result("probe")
	if !ok || r.Err != "" {
		return "not read: " + orDash(r.Err)
	}
	var p struct {
		Map      json.RawMessage `json:"map"`
		MapError string          `json:"map_error"`
		Proposal json.RawMessage `json:"proposal"`
		Progress *struct {
			State string `json:"state"`
		} `json:"progress"`
		FanControl bool `json:"fan_control_possible"`
	}
	if json.Unmarshal([]byte(r.Out), &p) != nil {
		return "answer is not JSON"
	}
	state := "none"
	if p.Progress != nil {
		state = p.Progress.State
	}
	return fmt.Sprintf("map confirmed %t, proposal waiting %t, probe %s, fan control possible %t, map error %s", string(p.Map) != "null" && len(p.Map) > 0, string(p.Proposal) != "null" && len(p.Proposal) > 0, state, p.FanControl, orDash(p.MapError))
}

func fanProfiles(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	r, ok := in.Fan.result("profiles")
	if !ok || r.Err != "" {
		return "not read: " + orDash(r.Err)
	}
	var p struct {
		Sources []struct {
			ID      string `json:"id"`
			Running bool   `json:"running"`
		} `json:"sources"`
	}
	if json.Unmarshal([]byte(r.Out), &p) != nil {
		return "answer is not JSON"
	}
	var l []string
	for _, s := range p.Sources {
		l = append(l, fmt.Sprintf("%s (running %t)", s.ID, s.Running))
	}
	return "importable settings of other tools: " + orDash(strings.Join(l, ", "))
}

func fanScripts(in FeatureInputs) string {
	if t, ok := fanNA(in); ok {
		return t
	}
	if len(in.Scripts) == 0 {
		return "script inputs: not set (no files in sensors.d)"
	}
	return fmt.Sprintf("script inputs: set, %d files, interpreters: %s (names hidden)", len(in.Scripts), strings.Join(in.Scripts, ", "))
}

func watchdogLine(in FeatureInputs) string {
	caps, ok := fan.CapsFor(in.Model)
	if !ok || !caps.Watchdog {
		return na
	}
	w := (*struct {
		Armed    bool   `json:"armed"`
		Timeout  int    `json:"timeout"`
		Interval int    `json:"interval"`
		LastPet  int64  `json:"last_pet"`
		Time     int64  `json:"time"`
		State    string `json:"state"`
		Counter  *int   `json:"counter"`
	})(nil)
	if in.FanSt != nil {
		w = in.FanSt.Watchdog
	}
	if w == nil {
		return "no watchdog status"
	}
	marker := "-"
	if w.Counter != nil {
		marker = fmt.Sprint(*w.Counter)
	}
	return fmt.Sprintf("%s, armed %t, timeout %d s, feed interval %d s, last feed %s, counter %s", orDash(w.State), w.Armed, w.Timeout, w.Interval, ageText(w.LastPet, w.Time), marker)
}

func powerOnAC(in FeatureInputs) string {
	r, ok := in.Fan.result("power-on-ac")
	if !ok {
		return "not read"
	}
	if r.Err != "" {
		if strings.Contains(r.Err, "no power-on setting") {
			return na
		}
		return "not readable: " + r.Err
	}
	m := parseKV(r.Out)
	if m == nil {
		return "answer is not JSON"
	}
	return m.list("mode", "readable", "settable", "source")
}

func panelButtons(in FeatureInputs) string {
	if panel.ButtonsFor(in.Model) == nil {
		return na
	}
	r, ok := in.Fan.result("panel-buttons")
	if !ok {
		return "not read"
	}
	if r.Err != "" {
		return "not readable: " + r.Err
	}
	return "readable: " + short(strings.TrimPrefix(strings.TrimSpace(r.Out), "model:"), 200)
}

func panelBeeper(in FeatureInputs) string {
	b := panel.BeeperFor(in.Model)
	if b == nil {
		return na
	}
	return fmt.Sprintf("present, backend %v (never sounded by diag)", b.Kind)
}

func lcdBacklight(in FeatureInputs) string {
	if caps, ok := fan.CapsFor(in.Model); !ok || !caps.Backlight {
		return na
	}
	r, ok := in.Fan.result("backlight")
	if !ok {
		return "not read"
	}
	if r.Err != "" {
		return "not readable: " + r.Err
	}
	m := parseKV(r.Out)
	if m == nil {
		return "answer is not JSON"
	}
	return m.list("pct", "raw")
}

func mqttLine(in FeatureInputs) string {
	c := in.MQTT
	return fmt.Sprintf("enabled %t, publisher running %t, host %s, login %s, tls %t, port %d, interval %s, control %t, control updates %t, control containers %t, connection: %s",
		c.Enabled, in.MQTTRunning, setText(c.Host), setText(c.User), c.TLS, c.Port, c.Interval,
		c.Control, c.ControlUpdates, c.ControlContainers, mqttConnection(in))
}

func setText(v string) string {
	if v == "" {
		return "not set"
	}
	return "set (hidden)"
}

// mqttConnection classifies the last lines of the publisher log; the broker name in them never shows.
func mqttConnection(in FeatureInputs) string {
	if !in.MQTTRunning {
		return "publisher stopped"
	}
	lines := strings.Split(strings.TrimSpace(in.MQTTLog), "\n")
	last := strings.ToLower(lines[len(lines)-1])
	switch {
	case !strings.Contains(last, "retrying"):
		return "no connection error logged"
	case strings.Contains(last, "refused"):
		return "failing: connection refused"
	case strings.Contains(last, "timeout"), strings.Contains(last, "timed out"):
		return "failing: timeout"
	case strings.Contains(last, "no such host"), strings.Contains(last, "lookup"):
		return "failing: host name not found"
	case strings.Contains(last, "not authorized"), strings.Contains(last, "bad user"), strings.Contains(last, "credentials"):
		return "failing: login refused"
	}
	return "failing: see the Log section of the publisher"
}

func conflictsLine(in FeatureInputs) string {
	var cf []string
	if in.FanSt != nil {
		for _, c := range in.FanSt.Conflicts {
			cf = append(cf, fmt.Sprintf("%s:%s (%s)", c.Kind, c.ID, c.Severity))
		}
	}
	yield := "-"
	if in.FanSt != nil && in.FanSt.Daemon != nil {
		yield = orDash(strings.Join(in.FanSt.Daemon.Conflict, ","))
	}
	return fmt.Sprintf("detected: %s; fan daemon yields to: %s; LED takeover blocked by: %s; rival processes running: %d", orDash(strings.Join(cf, "; ")), yield, orDash(in.Rivals.LEDBlock), len(in.Rivals.Procs))
}

func uiLine(in FeatureInputs) string {
	v := func(k string) string {
		if x := in.FanCfgRaw[k]; x != "" {
			return x
		}
		return "default"
	}
	return fmt.Sprintf("language %s, theme %s", v("ui_lang"), v("ui_theme"))
}

// scriptInterpreters names the interpreter of each script in dir from its shebang; the file names stay out.
func scriptInterpreters(dir string) []string {
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		line := readFirstLine(filepath.Join(dir, e.Name()))
		kind := "no shebang"
		if strings.HasPrefix(line, "#!") {
			f := strings.Fields(line[2:])
			if len(f) > 0 {
				prog := filepath.Base(f[0])
				if prog == "env" && len(f) > 1 {
					prog = filepath.Base(f[1])
				}
				kind = prog
			}
		}
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}
