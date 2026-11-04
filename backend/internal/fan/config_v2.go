package fan

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The v2 settings (cfg_version="2"): per group and per channel tuning, input choice, quiet hours and job rules.
// A file without cfg_version is v1: the keys below are ignored, so it decides exactly as it always did, and it is not
// rewritten before the user saves from the v2 page (api.php then keeps it as fan.cfg.bak).
//
// Pipeline order (docs/fan.md): inputs, curve, shape, job floor/boost, quiet cap, emergency override.

const cfgVersion = 2

// Group is the tuning of one fan group ("cpu" or "case"). Every default is today's global behaviour.
type Group struct {
	// Input: where the temperature comes from. The zero value (auto) is today's: the CPU for "cpu", the hottest
	// non-ignored disk or NVMe minus nvme_offset for "case".
	Input InputSpec
	Curve Curve
	// Deadband: the target only moves when it differs by at least this many % (0 = every change).
	Deadband int
	// StartPct and StartMS: kick-start from a stop or from below MinRun (0 = none).
	StartPct, StartMS int
	// MinRun: the lowest speed while running, never below the global min_pct.
	MinRun int
	// UpStep and DownStep: the most the speed changes per tick (100 = at once; down defaults to stepDown).
	UpStep, DownStep int
	// AllowOff: a curve target of 0 may stop the fan (allow_off, default off). The global minimum, a job floor and an
	// emergency still win; the shaping code keeps a fan running otherwise.
	AllowOff bool
	// OnMissing: what an unreadable input does: "fallback" (the next in the chain, ending in the curve's lowest point;
	// an unreadable CPU still triggers cpu_unreadable), "curve_low" or "hold" (keep the last target).
	OnMissing string
}

// Quiet: from Days at From until To (minutes after midnight, To may be earlier: across midnight) the targets of
// Scope are capped at Cap %, but never below MinRun and never during an emergency.
type Quiet struct {
	Enabled  bool
	From, To int
	// Days is Monday=0 to Sunday=6 of the day the window starts.
	Days  [7]bool
	Cap   int
	Scope string // "case" or "all"
}

// Job: a background job (mover, parity check/rebuild) that raises the case fans: "off", "floor" (at least Pct %) or
// "boost" (Pct points on top).
type Job struct {
	Mode string
	Pct  int
}

var (
	groupFields = []string{"input", "deadband", "start_pct", "start_ms", "min_run", "up_step", "down_step", "on_missing", "allow_off"}
	dayNames    = [7]string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	hhmmRe      = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)
	scriptRe    = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
	ifaceRe     = regexp.MustCompile(`^[A-Za-z0-9_.@-]{1,15}$`)
	channelRe   = regexp.MustCompile(`^fan\.([a-z0-9]+)\.([a-z_]+)$`)
)

// deriveGroups fills the groups from the v1 globals, so a config without v2 keys changes nothing.
func (c *Config) deriveGroups() {
	base := func(curve Curve) Group {
		return Group{Curve: curve, MinRun: c.MinPct, UpStep: 100, DownStep: stepDown, OnMissing: "fallback"}
	}
	c.CPU, c.Case = base(c.CPUCurve), base(c.CaseCurve)
	c.Quiet = Quiet{From: 23 * 60, To: 7 * 60, Days: [7]bool{true, true, true, true, true, true, true}, Cap: 40, Scope: "case"}
	c.Mover = Job{Mode: "off", Pct: 40}
	c.Parity = Job{Mode: "off", Pct: 40}
	c.Channels = nil
}

func (c Config) Group(role string) Group {
	if role == "cpu" {
		return c.CPU
	}
	return c.Case
}

// ForChannel is the tuning of one fan: its role's group with the fan.<id>.<key> overrides applied.
func (c Config) ForChannel(id string) Group {
	if g, ok := c.Channels[id]; ok {
		return g
	}
	return c.Group(roles[id])
}

func (c *Config) applyV2(v map[string]string, warn func(string)) {
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	preset := c.Mode != "custom"
	for _, k := range keys {
		if err := c.setV2(k, v[k], preset); err != nil && err != errUnknownKey {
			warn(k)
		}
	}
	if c.Quiet.From == c.Quiet.To && c.Quiet.Enabled {
		c.Quiet.Enabled = false
		c.Warnings = append(c.Warnings, "quiet_from and quiet_to are the same time, quiet hours are off")
	}
}

var errUnknownKey = fmt.Errorf("not a v2 setting")

// ValidateSetting checks one v2 key and value the way LoadConfig does; api.php mirrors it (web/plugin/include/fan_v2.php).
// Keys of v1 and unknown keys report an error.
func ValidateSetting(key, value string) error {
	var c Config
	c.MinPct = 10
	c.CPUCurve, c.CaseCurve = Curve{{0, 0}}, Curve{{0, 0}}
	c.deriveGroups()
	if err := c.setV2(key, value, false); err != nil {
		if err == errUnknownKey {
			return fmt.Errorf("%q is not a v2 fan setting", key)
		}
		return err
	}
	return nil
}

// setV2 applies one key. preset: the mode brings its own curves, so curve keys are accepted and ignored.
func (c *Config) setV2(key, val string, preset bool) error {
	switch key {
	case "quiet_enabled":
		return setBool(&c.Quiet.Enabled, val)
	case "quiet_from":
		return setClock(&c.Quiet.From, val)
	case "quiet_to":
		return setClock(&c.Quiet.To, val)
	case "quiet_days":
		days, err := parseDays(val)
		if err == nil {
			c.Quiet.Days = days
		}
		return err
	case "quiet_cap":
		return setInt(&c.Quiet.Cap, val, 10, 100)
	case "quiet_scope":
		return setEnum(&c.Quiet.Scope, val, "case", "all")
	case "job_mover":
		return setEnum(&c.Mover.Mode, val, "off", "floor", "boost")
	case "job_parity":
		return setEnum(&c.Parity.Mode, val, "off", "floor", "boost")
	case "job_mover_pct":
		return setInt(&c.Mover.Pct, val, 1, 100)
	case "job_parity_pct":
		return setInt(&c.Parity.Pct, val, 1, 100)
	}
	if m := channelRe.FindStringSubmatch(key); m != nil {
		if _, ok := roles[m[1]]; !ok || !isGroupField(m[2]) && m[2] != "curve" {
			return errUnknownKey
		}
		g, ok := c.Channels[m[1]]
		if !ok {
			g = c.Group(roles[m[1]])
		}
		err := setGroupField(&g, m[2], val, c.MinPct, preset)
		if err == nil {
			if c.Channels == nil {
				c.Channels = map[string]Group{}
			}
			c.Channels[m[1]] = g
		}
		return err
	}
	role, field, ok := strings.Cut(key, "_")
	if !ok || (role != "cpu" && role != "case") || !isGroupField(field) {
		return errUnknownKey
	}
	g := &c.CPU
	if role == "case" {
		g = &c.Case
	}
	return setGroupField(g, field, val, c.MinPct, preset)
}

func isGroupField(f string) bool {
	for _, g := range groupFields {
		if f == g {
			return true
		}
	}
	return false
}

func setGroupField(g *Group, field, val string, minPct int, preset bool) error {
	switch field {
	case "input":
		in, err := ParseInputSpec(val)
		if err == nil {
			g.Input = in
		}
		return err
	case "curve":
		if strings.ContainsAny(val, " \t\r\n") {
			return fmt.Errorf("curve %q has whitespace", val)
		}
		curve, err := ParseCurve(val)
		if err == nil && !preset {
			g.Curve = curve
		}
		return err
	case "deadband":
		return setInt(&g.Deadband, val, 0, 10)
	case "start_pct":
		return setInt(&g.StartPct, val, 0, 100)
	case "start_ms":
		return setInt(&g.StartMS, val, 0, 10000)
	case "min_run":
		err := setInt(&g.MinRun, val, 10, 100)
		g.MinRun = max(g.MinRun, minPct) // the global minimum is the floor
		return err
	case "up_step":
		return setInt(&g.UpStep, val, 1, 100)
	case "down_step":
		return setInt(&g.DownStep, val, 1, 100)
	case "on_missing":
		return setEnum(&g.OnMissing, val, "fallback", "curve_low", "hold")
	case "allow_off":
		return setBool(&g.AllowOff, val)
	}
	return errUnknownKey
}

func setInt(dst *int, val string, lo, hi int) error {
	n, err := strconv.Atoi(val)
	if err != nil || n < lo || n > hi {
		return fmt.Errorf("%q is not a number from %d to %d", val, lo, hi)
	}
	*dst = n
	return nil
}

func setBool(dst *bool, val string) error {
	if val != "0" && val != "1" {
		return fmt.Errorf("%q is not 0 or 1", val)
	}
	*dst = val == "1"
	return nil
}

func setEnum(dst *string, val string, allowed ...string) error {
	for _, a := range allowed {
		if val == a {
			*dst = val
			return nil
		}
	}
	return fmt.Errorf("%q is not one of %s", val, strings.Join(allowed, ", "))
}

func setClock(dst *int, val string) error {
	m := hhmmRe.FindStringSubmatch(val)
	if m == nil {
		return fmt.Errorf("%q is not HH:MM", val)
	}
	h, _ := strconv.Atoi(m[1])
	mins, _ := strconv.Atoi(m[2])
	*dst = h*60 + mins
	return nil
}

func parseDays(s string) ([7]bool, error) {
	var days [7]bool
	for _, name := range strings.Split(s, ",") {
		found := false
		for i, d := range dayNames {
			if name == d {
				days[i], found = true, true
			}
		}
		if !found {
			return days, fmt.Errorf("%q is not a list of mon..sun", s)
		}
	}
	return days, nil
}

func (q Quiet) DaysString() string {
	var list []string
	for i, on := range q.Days {
		if on {
			list = append(list, dayNames[i])
		}
	}
	return strings.Join(list, ",")
}

func Clock(minutes int) string { return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60) }

// InputSpec names a temperature source for a group. Kinds: auto (empty), cpu, disk_max, disk_avg, nvme, gpu, hba, nic,
// max (of the readable parts) and script. Only the syntax lives here; the readers belong to the inputs code.
type InputSpec struct {
	Kind string
	// Disks: Unraid drive names for disk_max and disk_avg (empty = all).
	Disks []string
	// Name: the interface of nic, the script file name of script.
	Name string
	// Parts: the sources of max.
	Parts []InputSpec
}

// ParseInputSpec reads "cpu", "disk_max", "disk_avg:disk1+disk2", "nvme", "gpu", "hba", "nic:eth0", "script:name" or
// "max:a|b|c" (2 to 8 parts, none of them max). Empty and "auto" are the default input.
func ParseInputSpec(s string) (InputSpec, error) {
	return parseInput(s, true)
}

func parseInput(s string, top bool) (InputSpec, error) {
	if len(s) > 256 {
		return InputSpec{}, fmt.Errorf("input spec is too long")
	}
	kind, arg, hasArg := strings.Cut(s, ":")
	switch kind {
	case "", "auto":
		if !hasArg && top {
			return InputSpec{}, nil
		}
	case "cpu", "nvme", "gpu", "hba":
		if !hasArg {
			return InputSpec{Kind: kind}, nil
		}
	case "disk_max", "disk_avg":
		in := InputSpec{Kind: kind}
		if !hasArg {
			return in, nil
		}
		seen := map[string]bool{}
		for _, name := range strings.Split(arg, "+") {
			if !driveNameRe.MatchString(name) || len(in.Disks) >= 32 {
				return InputSpec{}, fmt.Errorf("input %q: bad drive list", s)
			}
			if !seen[name] {
				seen[name] = true
				in.Disks = append(in.Disks, name)
			}
		}
		return in, nil
	case "nic":
		if hasArg && ifaceRe.MatchString(arg) {
			return InputSpec{Kind: kind, Name: arg}, nil
		}
	case "script":
		if hasArg && scriptRe.MatchString(arg) {
			return InputSpec{Kind: kind, Name: arg}, nil
		}
	case "max":
		if !top || !hasArg {
			break
		}
		in := InputSpec{Kind: kind}
		for _, part := range strings.Split(arg, "|") {
			p, err := parseInput(part, false)
			if err != nil || p.Kind == "max" || p.Kind == "" {
				return InputSpec{}, fmt.Errorf("input %q: bad part %q", s, part)
			}
			in.Parts = append(in.Parts, p)
		}
		if len(in.Parts) < 2 || len(in.Parts) > 8 {
			return InputSpec{}, fmt.Errorf("input %q: max needs 2 to 8 parts", s)
		}
		return in, nil
	}
	return InputSpec{}, fmt.Errorf("input %q is not a valid input spec", s)
}

// String is the canonical spec text; ParseInputSpec(spec.String()) gives the spec back.
func (i InputSpec) String() string {
	switch i.Kind {
	case "":
		return "auto"
	case "disk_max", "disk_avg":
		if len(i.Disks) > 0 {
			return i.Kind + ":" + strings.Join(i.Disks, "+")
		}
	case "nic", "script":
		return i.Kind + ":" + i.Name
	case "max":
		parts := make([]string, len(i.Parts))
		for n, p := range i.Parts {
			parts[n] = p.String()
		}
		return "max:" + strings.Join(parts, "|")
	}
	return i.Kind
}

// ConfigView is the effective v2 configuration as the web page reads it (contract.ts FanConfigV2).
type ConfigView struct {
	Version  int                  `json:"version"`
	CPU      GroupView            `json:"cpu"`
	Case     GroupView            `json:"case"`
	Channels map[string]GroupView `json:"channels"`
	Quiet    QuietView            `json:"quiet"`
	Mover    JobView              `json:"mover"`
	Parity   JobView              `json:"parity"`
}

type GroupView struct {
	Input     string `json:"input"`
	Curve     string `json:"curve"`
	Deadband  int    `json:"deadband"`
	StartPct  int    `json:"start_pct"`
	StartMS   int    `json:"start_ms"`
	MinRun    int    `json:"min_run"`
	UpStep    int    `json:"up_step"`
	DownStep  int    `json:"down_step"`
	OnMissing string `json:"on_missing"`
	AllowOff  bool   `json:"allow_off"`
}

type QuietView struct {
	Enabled bool   `json:"enabled"`
	From    string `json:"from"`
	To      string `json:"to"`
	Days    string `json:"days"`
	Cap     int    `json:"cap"`
	Scope   string `json:"scope"`
}

type JobView struct {
	Mode string `json:"mode"`
	Pct  int    `json:"pct"`
}

func (g Group) view() GroupView {
	return GroupView{g.Input.String(), g.Curve.String(), g.Deadband, g.StartPct, g.StartMS, g.MinRun, g.UpStep, g.DownStep, g.OnMissing, g.AllowOff}
}

func (c Config) View() ConfigView {
	v := ConfigView{
		Version:  c.Version,
		CPU:      c.CPU.view(),
		Case:     c.Case.view(),
		Channels: map[string]GroupView{},
		Quiet:    QuietView{c.Quiet.Enabled, Clock(c.Quiet.From), Clock(c.Quiet.To), c.Quiet.DaysString(), c.Quiet.Cap, c.Quiet.Scope},
		Mover:    JobView(c.Mover),
		Parity:   JobView(c.Parity),
	}
	for id, g := range c.Channels {
		v.Channels[id] = g.view()
	}
	return v
}
