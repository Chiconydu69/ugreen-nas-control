package fan

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/profile"
)

// The glue between package profile (pure parsers) and the fan settings: the rules that clamp a profile into what
// LoadConfig accepts, the NAS lookups the importers need, and the `profile` command (api.php fan_profile_*).
//
// Nothing here writes the settings file: `profile apply` prints the plan (keys and values); api.php checks every
// value again with fan_v2.php and writes under its flock.

var diskByIDDir = envOr("UGREEN_DISK_BY_ID", "/dev/disk/by-id")

// v1 settings a profile carries (fan_control, ui_lang and ui_theme are never part of it).
var profileV1Ints = map[string][2]int{
	"min_pct": {10, 60}, "cpu_crit": {70, 105}, "disk_crit": {40, 70}, "nvme_offset": {0, 40},
}

var profileV1Bools = []string{"case_follows_cpu", "stall_notify"}

var profileV2Ints = map[string][2]int{
	"deadband": {0, 10}, "start_pct": {0, 100}, "start_ms": {0, 10000}, "min_run": {10, 100},
	"up_step": {1, 100}, "down_step": {1, 100},
	"quiet_cap": {10, 100}, "job_mover_pct": {1, 100}, "job_parity_pct": {1, 100},
}

var profileFieldRe = regexp.MustCompile(`^(?:(?:cpu|case)_|fan\.[a-z0-9]+\.)([a-z_]+)$`)

// isV1Setting says whether the key belongs to the v1 file format (a profile with only these leaves a v1 file v1).
func isV1Setting(key string) bool {
	if _, ok := profileV1Ints[key]; ok {
		return true
	}
	switch key {
	case "mode", "cpu_curve", "case_curve", "ignore_drives", "case_follows_cpu", "stall_notify":
		return true
	}
	return false
}

// ProfileRules clamps profile settings into the ranges LoadConfig accepts.
type ProfileRules struct{}

func (ProfileRules) Normalize(key, value string) (string, string, error) {
	value = strings.TrimSpace(value)
	if len(value) > 512 {
		return "", "", fmt.Errorf("the value is too long")
	}
	if isV1Setting(key) {
		return normalizeV1(key, value)
	}
	return normalizeV2(key, value)
}

func clampNote(n, lo, hi int) (int, string) {
	switch {
	case n < lo:
		return lo, fmt.Sprintf("%d is below the minimum, set to %d", n, lo)
	case n > hi:
		return hi, fmt.Sprintf("%d is above the maximum, set to %d", n, hi)
	}
	return n, ""
}

func normalizeV1(key, value string) (string, string, error) {
	switch key {
	case "mode":
		if value == "bios" || value == "custom" || presets[value] != [2]string{} {
			return value, "", nil
		}
		return "", "", fmt.Errorf("%q is not a fan mode", value)
	case "cpu_curve", "case_curve":
		return normalizeCurve(value)
	case "ignore_drives":
		var names []string
		for n := range parseDriveList(value) {
			names = append(names, n)
		}
		sort.Strings(names)
		return strings.Join(names, ","), "", nil
	}
	if r, ok := profileV1Ints[key]; ok {
		return normalizeInt(value, r[0], r[1])
	}
	for _, b := range profileV1Bools {
		if key == b {
			if value == "0" || value == "1" {
				return value, "", nil
			}
			return "", "", fmt.Errorf("%q is not 0 or 1", value)
		}
	}
	return "", "", fmt.Errorf("not a setting a profile carries")
}

func normalizeInt(value string, lo, hi int) (string, string, error) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return "", "", fmt.Errorf("%q is not a number", value)
	}
	n, note := clampNote(n, lo, hi)
	return strconv.Itoa(n), note, nil
}

// normalizeCurve gives the text fan_v2.php accepts: 2 to 8 points, temperatures rising without repeats.
func normalizeCurve(value string) (string, string, error) {
	c, err := ParseCurve(value)
	if err != nil {
		return "", "", err
	}
	var out Curve
	note := ""
	for _, p := range c {
		if n := len(out); n > 0 && out[n-1].Temp == p.Temp {
			out[n-1] = p
			note = "two points at one temperature, the later one is used"
			continue
		}
		out = append(out, p)
	}
	if len(out) == 1 {
		p := out[0]
		if p.Temp+10 <= 120 {
			out = append(out, Point{p.Temp + 10, p.Pct})
		} else {
			out = Curve{{p.Temp - 10, p.Pct}, p}
		}
		note = "a single point was widened to a flat line"
	}
	if len(out) > 8 {
		return "", "", fmt.Errorf("a curve has at most 8 points")
	}
	return out.String(), note, nil
}

func normalizeV2(key, value string) (string, string, error) {
	field := key
	if m := profileFieldRe.FindStringSubmatch(key); m != nil {
		field = m[1]
	}
	if field == "curve" && strings.HasPrefix(key, "fan.") {
		clean, note, err := normalizeCurve(value)
		if err != nil {
			return "", "", err
		}
		return clean, note, ValidateSetting(key, clean)
	}
	if r, ok := profileV2Ints[field]; ok {
		clean, note, err := normalizeInt(value, r[0], r[1])
		if err != nil {
			return "", "", err
		}
		return clean, note, ValidateSetting(key, clean)
	}
	switch field {
	case "input":
		in, err := ParseInputSpec(value)
		if err != nil {
			return "", "", err
		}
		if err := ValidateSetting(key, in.String()); err != nil {
			return "", "", err
		}
		return in.String(), "", nil
	case "quiet_days":
		days, err := parseDays(value)
		if err != nil {
			return "", "", err
		}
		return Quiet{Days: days}.DaysString(), "", nil
	}
	if err := ValidateSetting(key, value); err != nil {
		return "", "", err
	}
	return value, "", nil
}

// EffectiveSettings is the profile view of a config: every v1 setting, and the v2 settings (defaults included).
func EffectiveSettings(c Config) map[string]string {
	out := map[string]string{
		"mode":             c.Mode,
		"cpu_curve":        c.CPUCurve.String(),
		"case_curve":       c.CaseCurve.String(),
		"min_pct":          strconv.Itoa(c.MinPct),
		"cpu_crit":         strconv.Itoa(c.CPUCrit),
		"disk_crit":        strconv.Itoa(c.DiskCrit),
		"nvme_offset":      strconv.Itoa(c.NVMeOffset),
		"case_follows_cpu": b01(c.CaseFollowsCPU),
		"stall_notify":     b01(c.StallNotify),
	}
	var ignore []string
	for n := range c.IgnoreDrives {
		ignore = append(ignore, n)
	}
	sort.Strings(ignore)
	out["ignore_drives"] = strings.Join(ignore, ",")

	group := func(prefix string, g Group, curve bool) {
		out[prefix+"input"] = g.Input.String()
		out[prefix+"deadband"] = strconv.Itoa(g.Deadband)
		out[prefix+"start_pct"] = strconv.Itoa(g.StartPct)
		out[prefix+"start_ms"] = strconv.Itoa(g.StartMS)
		out[prefix+"min_run"] = strconv.Itoa(g.MinRun)
		out[prefix+"up_step"] = strconv.Itoa(g.UpStep)
		out[prefix+"down_step"] = strconv.Itoa(g.DownStep)
		out[prefix+"on_missing"] = g.OnMissing
		if curve {
			out[prefix+"curve"] = g.Curve.String()
		}
	}
	group("cpu_", c.CPU, false)
	group("case_", c.Case, false)
	for id, g := range c.Channels {
		group("fan."+id+".", g, true)
	}
	q := c.Quiet
	out["quiet_enabled"], out["quiet_from"], out["quiet_to"] = b01(q.Enabled), Clock(q.From), Clock(q.To)
	out["quiet_days"], out["quiet_cap"], out["quiet_scope"] = q.DaysString(), strconv.Itoa(q.Cap), q.Scope
	out["job_mover"], out["job_mover_pct"] = c.Mover.Mode, strconv.Itoa(c.Mover.Pct)
	out["job_parity"], out["job_parity_pct"] = c.Parity.Mode, strconv.Itoa(c.Parity.Pct)
	return out
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// ExportProfile is the profile of the saved settings: a v1 file exports its v1 settings, a v2 file all of them.
func ExportProfile(c Config, model string, now time.Time) profile.Profile {
	p := profile.Profile{Schema: profile.Schema, Kind: profile.Kind, Model: model, Created: now.UTC().Format(time.RFC3339), Fan: map[string]string{}}
	for k, v := range EffectiveSettings(c) {
		if c.Version >= 2 || isV1Setting(k) {
			p.Fan[k] = v
		}
	}
	return p
}

var (
	nvmeDevPartRe = regexp.MustCompile(`^(nvme[0-9]+)(?:n[0-9]+)?(?:p[0-9]+)?$`)
	diskIDRe      = regexp.MustCompile(`^[A-Za-z0-9_.:+-]{1,200}$`)
)

// profileEnv is a variable so that tests can stand in for the NAS.
var profileEnv = nasEnv

// nasEnv resolves drive names against the disks Unraid reports.
func nasEnv() profile.Env {
	s := ReadSensors()
	byDevice := func(dev string) (string, bool) {
		dev = strings.TrimPrefix(dev, "/dev/")
		if m := nvmeDevPartRe.FindStringSubmatch(dev); m != nil {
			for _, n := range s.NVMe {
				if n.Name == m[1] {
					return n.Name, true
				}
			}
			return "", false
		}
		dev = strings.TrimRight(dev, "0123456789")
		for _, d := range s.Disks {
			if d.Device == dev && d.Name != "" {
				return d.Name, true
			}
		}
		return "", false
	}
	return profile.Env{
		ResolveDevice: byDevice,
		ResolveDisk: func(id string) (string, bool) {
			// Unraid's id is the by-id name without its bus prefix (ata-, scsi-, nvme-).
			if _, rest, ok := strings.Cut(id, "-"); ok {
				for _, d := range s.Disks {
					if d.ID != "" && d.ID == rest && d.Name != "" {
						return d.Name, true
					}
				}
			}
			if !diskIDRe.MatchString(id) {
				return "", false
			}
			if target, err := filepath.EvalSymlinks(filepath.Join(diskByIDDir, id)); err == nil {
				return byDevice(filepath.Base(target))
			}
			return "", false
		},
	}
}

type SourceInfo struct {
	profile.Source
	Running bool `json:"running"`
}

// ProfileSources lists the rival configs on this NAS and whether that tool is running now.
func ProfileSources() []SourceInfo {
	boot := conflict.Default().PluginsBoot
	running := map[string]bool{}
	for _, c := range detectConflicts() {
		if c.Kind == conflict.KindDaemon && c.Severity == conflict.SeverityBlock {
			running[c.Name] = true
		}
	}
	out := []SourceInfo{}
	for _, s := range profile.Discover(boot) {
		out = append(out, SourceInfo{Source: s, Running: running[s.Name] || (s.ID == profile.SourceITKuny && running["UGREEN fan control (IT-Kuny)"])})
	}
	return out
}

type DiffRow struct {
	Key  string `json:"key"`
	From string `json:"from"`
	To   string `json:"to"`
}

type ProfilePreview struct {
	OK          bool              `json:"ok"`
	Source      string            `json:"source"`
	Title       string            `json:"title"`
	Model       string            `json:"model,omitempty"`
	Token       string            `json:"token"`
	Profile     map[string]string `json:"profile"`
	Diff        []DiffRow         `json:"diff"`
	Unchanged   int               `json:"unchanged"`
	Warnings    []string          `json:"warnings"`
	NotImported []profile.Skipped `json:"not_imported"`
	Provenance  map[string]string `json:"provenance"`
	// V2: the profile has v2 settings; saving them turns a v1 file into v2 (and keeps fan.cfg.bak).
	V2 bool `json:"v2"`
}

// ProfileApplyPlan is the answer of `profile apply`: the settings api.php writes.
type ProfileApplyPlan struct {
	OK     bool              `json:"ok"`
	Values map[string]string `json:"values"`
	V2     bool              `json:"v2"`
}

func previewOf(res *profile.Result, c Config, model string) ProfilePreview {
	prev := ProfilePreview{
		OK: true, Source: res.Source, Title: res.Title, Model: res.Profile.Model, Token: res.Profile.Token(),
		Profile: res.Profile.Fan, Diff: []DiffRow{}, Warnings: res.Warnings, NotImported: res.NotImported, Provenance: res.Provenance,
	}
	if res.Profile.Model != "" && model != "" && res.Profile.Model != model {
		prev.Warnings = append(prev.Warnings, fmt.Sprintf("the profile is from a %s, this NAS is a %s", res.Profile.Model, model))
	}
	cur := EffectiveSettings(c)
	keys := make([]string, 0, len(res.Profile.Fan))
	for k := range res.Profile.Fan {
		keys = append(keys, k)
		if !isV1Setting(k) {
			prev.V2 = true
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if cur[k] == res.Profile.Fan[k] {
			prev.Unchanged++
			continue
		}
		prev.Diff = append(prev.Diff, DiffRow{k, cur[k], res.Profile.Fan[k]})
	}
	if prev.Warnings == nil {
		prev.Warnings = []string{}
	}
	return prev
}

// profileInput is what a preview or apply reads: a discovered source, or uploaded text.
type profileInput struct {
	source, path, name string
	content            []byte
}

func (in profileInput) parse() (*profile.Result, error) {
	rules, env := ProfileRules{}, profileEnv()
	if in.source != "" {
		if in.source == profile.SourceOwn {
			return nil, fmt.Errorf("unknown source")
		}
		return profile.LoadSource(in.source, conflict.Default().PluginsBoot, in.path, env, rules)
	}
	return profile.ParseUpload(in.name, in.content, env, rules)
}

// parseProfileArgs reads --source ID, --path P, --name N and --stdin.
func parseProfileArgs(args []string, stdin io.Reader) (profileInput, error) {
	var in profileInput
	for i := 0; i < len(args); i++ {
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", args[i])
			}
			i++
			return args[i], nil
		}
		var err error
		switch args[i] {
		case "--source":
			in.source, err = next()
		case "--path":
			in.path, err = next()
		case "--name":
			in.name, err = next()
		case "--stdin":
			in.content, err = io.ReadAll(io.LimitReader(stdin, profile.MaxBytes+1))
			if err == nil && len(in.content) > profile.MaxBytes {
				err = fmt.Errorf("the file is larger than %d KB", profile.MaxBytes>>10)
			}
		case "--token":
			_, err = next() // read by apply
		default:
			err = fmt.Errorf("unknown option %s", args[i])
		}
		if err != nil {
			return in, err
		}
	}
	if in.source == "" && in.content == nil {
		return in, fmt.Errorf("give --source or --stdin")
	}
	return in, nil
}

func argValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// profileCommand runs `profile export|sources|preview|apply`; it returns the value to print or an error.
func profileCommand(args []string, stdin io.Reader) (any, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("profile needs export, sources, preview or apply")
	}
	cfg := LoadConfig(configPath)
	switch args[0] {
	case "export":
		return ExportProfile(cfg, productName(), time.Now()), nil
	case "sources":
		return map[string]any{"ok": true, "sources": ProfileSources()}, nil
	case "preview", "apply":
		in, err := parseProfileArgs(args[1:], stdin)
		if err != nil {
			return nil, err
		}
		res, err := in.parse()
		if err != nil {
			return nil, err
		}
		prev := previewOf(res, cfg, productName())
		if args[0] == "preview" {
			return prev, nil
		}
		if token := argValue(args, "--token"); token == "" || token != prev.Token {
			return nil, fmt.Errorf("the settings changed since the preview; preview again")
		}
		if len(res.Profile.Fan) == 0 {
			return nil, fmt.Errorf("nothing to apply")
		}
		return ProfileApplyPlan{OK: true, Values: res.Profile.Fan, V2: prev.V2}, nil
	}
	return nil, fmt.Errorf("unknown profile command %q", args[0])
}

func inputsCommand(args []string) (any, error) {
	cfg := LoadConfig(configPath)
	r := NewRegistry(nil)
	ctx := context.Background()
	if len(args) >= 1 && args[0] == "input-test" {
		if len(args) != 2 {
			return nil, fmt.Errorf("input-test needs a spec")
		}
		return TestInput(ctx, r, cfg, args[1]), nil
	}
	return ListInputs(ctx, r, cfg, ReadSensors()), nil
}

// runInputsProfile is called by Main for the input and profile commands; handled=false for any other command.
func runInputsProfile(args []string) (handled bool) {
	if len(args) == 0 {
		return false
	}
	var out any
	var err error
	switch args[0] {
	case "inputs", "input-test":
		out, err = inputsCommand(args)
	case "profile":
		out, err = profileCommand(args[1:], os.Stdin)
	default:
		return false
	}
	if err != nil {
		printJSON(map[string]any{"ok": false, "error": err.Error()})
		os.Exit(1)
	}
	printJSON(out)
	return true
}
