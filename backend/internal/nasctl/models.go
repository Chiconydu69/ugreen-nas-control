package nasctl

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/panel"
)

// models.json is the model registry (web/models/*.ts) as docs/tools/gen-docs.ts renders it; `just docs` rewrites
// it and `just docs-check` fails when it is stale. The backend keeps no second copy of what a model is.
//
//go:embed models.json
var modelsJSON []byte

type RegCheck struct {
	Status  string `json:"status"`
	By      string `json:"by"`
	Version string `json:"version"`
	Date    string `json:"date"`
	Ref     string `json:"ref"`
	Note    string `json:"note"`
}

type RegModel struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DMIPattern string `json:"dmiPattern"`
	FixtureDMI string `json:"fixtureDmi"`
	Bays       *int   `json:"bays"`
	M2         *struct {
		Slots    []string `json:"slots"`
		PCIeSlot bool     `json:"pcieSlot"`
	} `json:"m2"`
	LAN *struct {
		MaxMbit []*int `json:"maxMbit"`
	} `json:"lan"`
	LED      string `json:"led"`
	CaseView string `json:"caseView"`
	Fan      *struct {
		Chip     string            `json:"chip"`
		Fans     map[string]string `json:"fans"`
		Kind     string            `json:"kind"`
		Watchdog *RegCheck         `json:"watchdog"`
	} `json:"fan"`
	Overall  string              `json:"overall"`
	Verified map[string]RegCheck `json:"verified"`
	Note     string              `json:"note"`
}

type registryFile struct {
	Schema int        `json:"schema"`
	Models []RegModel `json:"models"`
}

func loadRegistry(data []byte) ([]RegModel, error) {
	var f registryFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Schema != 1 {
		return nil, fmt.Errorf("registry schema %d", f.Schema)
	}
	return f.Models, nil
}

// matchModel is the first registry model whose DMI pattern matches the product name (the registry is in matching order).
func matchModel(models []RegModel, product string) *RegModel {
	for i := range models {
		if re, err := regexp.Compile(models[i].DMIPattern); err == nil && re.MatchString(product) {
			return &models[i]
		}
	}
	return nil
}

// Evidence is the machine-readable result (schema diag/1), built from the same data as the Markdown.
type Evidence struct {
	Schema        string            `json:"schema"`
	PluginVersion string            `json:"plugin_version"`
	Channel       string            `json:"channel"`
	UnraidVersion string            `json:"unraid_version"`
	TimeUTC       string            `json:"time_utc"`
	Model         EvModel           `json:"model"`
	Areas         map[string]EvArea `json:"areas"`
	Rivals        EvRivals          `json:"rivals"`
}

type EvModel struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Known bool    `json:"known"`
	DMI   Machine `json:"dmi"`
}

type EvArea struct {
	RegistryStatus string    `json:"registry_status"`
	Checks         []EvCheck `json:"checks"`
}

// EvCheck compares what the registry expects with what was detected; OK is null where nothing can be compared.
type EvCheck struct {
	ID       string `json:"id"`
	Expected any    `json:"expected"`
	Detected any    `json:"detected"`
	OK       *bool  `json:"ok"`
}

type EvRivals struct {
	Running   []string `json:"running"`
	Installed []string `json:"installed"`
	Modules   []string `json:"modules"`
	ManualPWM []string `json:"manual_pwm"`
	Yielding  []string `json:"yielding_to"`
}

func tri(ok bool) *bool { return &ok }

var chipKeyRe = regexp.MustCompile(`(?i)it\d{4}|cs\d{3}`)

func chipKey(s string) string { return strings.ToLower(chipKeyRe.FindString(s)) }

// EvInputs is what the evidence is made of.
type EvInputs struct {
	Reg    *RegModel
	Now    time.Time
	Report Report
	Fan    FanDiag
	FanSt  *fanStatus
	LED    LEDDiag
	Rivals RivalDiag
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func BuildEvidence(in EvInputs) Evidence {
	r, reg := in.Report, in.Reg
	ev := Evidence{
		Schema: "diag/1", PluginVersion: r.PluginVersion, Channel: pluginChannel(r.PluginVersion), UnraidVersion: r.UnraidVersion,
		TimeUTC: in.Now.UTC().Format("2006-01-02T15:04:05Z"),
		Model:   EvModel{ID: "unknown", Name: r.Machine.Product, DMI: r.Machine},
		Areas:   map[string]EvArea{},
	}
	if reg != nil {
		ev.Model.ID, ev.Model.Name, ev.Model.Known = reg.ID, reg.Name, true
	}
	status := func(area string) string {
		if reg == nil {
			return "unknown"
		}
		if c, ok := reg.Verified[area]; ok {
			return c.Status
		}
		return "unknown"
	}
	area := func(name, st string, checks ...EvCheck) { ev.Areas[name] = EvArea{RegistryStatus: st, Checks: checks} }

	// leds
	var ledExp any
	if reg != nil {
		ledExp = reg.LED
	}
	ledCheck := EvCheck{ID: "led_controller", Detected: r.LED.Probe}
	if reg != nil {
		ledCheck.Expected = "ok"
		switch {
		case reg.LED == "none":
			ledCheck.Expected = "none"
			ledCheck.OK = tri(r.LED.Probe != "ok")
		case r.LED.Probe == "not run":
			ledCheck.Detected = "not run: " + orDash(r.LED.Blocker)
		default:
			ledCheck.OK = tri(r.LED.Probe == "ok")
		}
	}
	area("leds", status("leds"), EvCheck{ID: "led_support", Expected: ledExp, Detected: fmt.Sprintf("supported %t, blocker %s", r.LED.Supported, orDash(r.LED.Blocker))}, ledCheck)

	// fans
	chip := EvCheck{ID: "fan_chip", Detected: strings.ToLower(r.Fan.Chip)}
	headers := EvCheck{ID: "fan_headers"}
	var seen []string
	if in.FanSt != nil {
		for _, f := range in.FanSt.Fans {
			seen = append(seen, f.ID+":"+f.Role)
		}
	}
	sort.Strings(seen)
	if len(seen) > 0 {
		headers.Detected = seen
	}
	if reg != nil && reg.Fan != nil {
		want := chipKey(reg.Fan.Chip)
		chip.Expected = want
		if strings.HasPrefix(want, "cs") {
			chip.Detected = ""
			for _, x := range in.Fan.Extra {
				if strings.HasPrefix(x.Title, "cs201x") && x.Err == "" && x.Out != "" {
					chip.Detected = "cs201x"
				}
			}
		}
		chip.OK = tri(chipKey(fmt.Sprint(chip.Detected)) == want)
		var exp []string
		for id, role := range reg.Fan.Fans {
			exp = append(exp, id+":"+role)
		}
		sort.Strings(exp)
		headers.Expected = exp
		if len(seen) > 0 {
			headers.OK = tri(slices.Equal(exp, seen))
		}
	}
	spin := EvCheck{ID: "fans_spin"}
	if len(seen) > 0 {
		all := true
		for _, f := range in.FanSt.Fans {
			if f.RPM <= 0 {
				all = false
			}
		}
		spin.Expected, spin.Detected, spin.OK = "all fans above 0 rpm", all, tri(all)
	}
	area("fans", status("fans"), chip, headers, spin)

	// case: bays, M.2 slots, LAN ports
	bays := EvCheck{ID: "bays", Detected: len(r.Bays)}
	nvme := EvCheck{ID: "nvme_slots"}
	var m2 []string
	for _, s := range r.NVMe {
		if strings.HasPrefix(s, "m2") {
			m2 = append(m2, s)
		}
	}
	sort.Strings(m2)
	nvme.Detected = nonNilStrings(m2)
	lan := EvCheck{ID: "lan_ports", Detected: len(r.Network)}
	if reg != nil {
		if reg.Bays != nil {
			bays.Expected, bays.OK = *reg.Bays, tri(*reg.Bays == len(r.Bays))
		}
		if reg.M2 != nil {
			exp := slices.Clone(reg.M2.Slots)
			sort.Strings(exp)
			nvme.Expected, nvme.OK = exp, tri(m2SlotsMatch(exp, m2, reg.M2.PCIeSlot))
		}
		if reg.LAN != nil {
			lan.Expected, lan.OK = len(reg.LAN.MaxMbit), tri(len(reg.LAN.MaxMbit) == len(r.Network))
		}
	}
	area("case", status("case"), bays, nvme, lan)

	// sensors
	cpuT := EvCheck{ID: "cpu_temperature", Expected: true}
	if in.FanSt != nil {
		cpuT.Detected = in.FanSt.Sensors.CPU != nil
		cpuT.OK = tri(in.FanSt.Sensors.CPU != nil)
	}
	area("sensors", status("sensors"), cpuT)

	// panel: the registry offers it exactly where the backend has a button map or a beeper
	tables := EvCheck{ID: "panel_tables"}
	if reg != nil {
		tables.Expected = reg.Verified["panel"].Status != "unsupported"
		tables.Detected = panel.ButtonsFor(r.Machine.Product) != nil || panel.BeeperFor(r.Machine.Product) != nil
		tables.OK = tri(tables.Expected == tables.Detected)
	}
	area("panel", status("panel"), tables)

	// watchdog
	wd := EvCheck{ID: "watchdog_state"}
	wst := "none"
	if reg != nil && reg.Fan != nil && reg.Fan.Watchdog != nil {
		wst = reg.Fan.Watchdog.Status
		wd.Expected = "armed, off or not counting"
	}
	if in.FanSt != nil && in.FanSt.Watchdog != nil {
		wd.Detected = in.FanSt.Watchdog.State
		if wd.Expected != nil {
			wd.OK = tri(!slices.Contains([]string{"action_needed", "not_effective"}, in.FanSt.Watchdog.State))
		}
	}
	area("watchdog", wst, wd)

	for _, p := range in.Rivals.Procs {
		ev.Rivals.Running = append(ev.Rivals.Running, fmt.Sprintf("%s (%s)", p.Name, p.Program))
	}
	for _, p := range in.Rivals.Plugins {
		if p.Plg {
			ev.Rivals.Installed = append(ev.Rivals.Installed, p.Name)
		}
	}
	ev.Rivals.Modules, ev.Rivals.ManualPWM, ev.Rivals.Yielding = nonNilStrings(in.Rivals.Modules), nonNilStrings(in.Rivals.Hwmon), nonNilStrings(in.Rivals.FanYields)
	ev.Rivals.Running, ev.Rivals.Installed = nonNilStrings(ev.Rivals.Running), nonNilStrings(ev.Rivals.Installed)
	return ev
}

var areaOrder = []string{"leds", "fans", "case", "sensors", "panel", "watchdog"}

var areaLabel = map[string]string{"leds": "LEDs", "fans": "fans", "case": "case", "sensors": "sensors", "panel": "front panel", "watchdog": "watchdog"}

func checkLine(c RegCheck) string {
	parts := []string{c.Status}
	if c.Version != "" {
		parts = append(parts, "on "+c.Version)
	}
	if c.Date != "" {
		parts = append(parts, c.Date)
	}
	s := strings.Join(parts, " ")
	if c.Note != "" {
		s += " - " + short(c.Note, 220)
	}
	return s
}

func shown(v any) string {
	if v == nil {
		return "-"
	}
	b, _ := json.Marshal(v)
	return strings.Trim(string(b), `"`)
}

// modelSection and its checks: the registry's view of this model next to what was detected.
func modelSection(reg *RegModel, ev Evidence) (Section, []string) {
	var l, checks []string
	m := ev.Model.DMI
	if reg == nil {
		l = append(l, fmt.Sprintf("registry: unknown model (DMI product %q, family %q, board %q, vendor %q)", m.Product, m.Family, m.Board, m.SysVendor))
		l = append(l, "this model is not in the registry yet: please file a model report (docs/cli.md#report-a-model)")
		checks = append(checks, "unknown model: not in the registry, please file a model report")
	} else {
		l = append(l, fmt.Sprintf("registry: %s (%s), overall %s", reg.ID, reg.Name, reg.Overall))
		l = append(l, "note: "+short(reg.Note, 300))
	}
	for _, name := range areaOrder {
		a, ok := ev.Areas[name]
		if !ok {
			continue
		}
		line := name + ": "
		var rc *RegCheck
		if reg != nil {
			if c, ok := reg.Verified[name]; ok {
				rc = &c
			} else if name == "watchdog" && reg.Fan != nil && reg.Fan.Watchdog != nil {
				rc = reg.Fan.Watchdog
			}
		}
		switch {
		case rc != nil:
			line += checkLine(*rc)
		case reg != nil && name == "watchdog":
			line += "none for this model"
		default:
			line += a.RegistryStatus
		}
		l = append(l, line)
		for _, c := range a.Checks {
			mark := "n/a"
			if c.OK != nil {
				mark = "ok"
				if !*c.OK {
					mark = "MISMATCH"
					hint := ""
					if c.ID == "nvme_slots" {
						hint = " (the NVMe section lists the PCI chain of each drive)"
					}
					checks = append(checks, fmt.Sprintf("MISMATCH: %s: registry expects %s, detected %s%s", c.ID, shown(c.Expected), shown(c.Detected), hint))
				}
			}
			l = append(l, fmt.Sprintf("  %s: expected %s, detected %s [%s]", c.ID, shown(c.Expected), shown(c.Detected), mark))
		}
		if rc != nil && (rc.Status == "expected" || rc.Status == "partial") && name != "watchdog" {
			if rc.Status == "expected" {
				checks = append(checks, fmt.Sprintf("%s: untested on this model (expected), please confirm it on the Feedback page", areaLabel[name]))
			} else {
				checks = append(checks, fmt.Sprintf("%s: only partly confirmed on this model: %s", areaLabel[name], short(rc.Note, 160)))
			}
		}
	}
	if reg != nil {
		var open []string
		for _, name := range areaOrder {
			if c, ok := reg.Verified[name]; ok && c.Note != "" && c.Status != "verified" && c.Status != "unsupported" {
				open = append(open, name+": "+short(c.Note, 200))
			}
		}
		l = append(l, "open points in the registry: "+orDash(strings.Join(open, " | ")))
	}
	return Section{Title: "Model (registry)", Body: strings.Join(l, "\n")}, checks
}

// evidenceSection is valid JSON with one line per top-level key and per area.
func evidenceSection(ev Evidence) Section {
	raw, _ := json.Marshal(ev)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var l []string
	for _, k := range keys {
		v := string(top[k])
		if k == "areas" {
			var areas map[string]json.RawMessage
			_ = json.Unmarshal(top[k], &areas)
			names := make([]string, 0, len(areas))
			for n := range areas {
				names = append(names, n)
			}
			sort.Strings(names)
			var sub []string
			for _, n := range names {
				sub = append(sub, fmt.Sprintf("    %q: %s", n, areas[n]))
			}
			v = "{\n" + strings.Join(sub, ",\n") + "\n  }"
		}
		l = append(l, fmt.Sprintf("  %q: %s", k, v))
	}
	return Section{Title: "Machine-readable (diag/1)", Body: "{\n" + strings.Join(l, ",\n") + "\n}", Lang: "json"}
}

// m2SlotsMatch compares the expected slot names with the detected ones. A plain "m2" is an SSD whose slot
// the plugin cannot tell (no root-port map): it fits any free expected slot, and on a model with a PCIe slot
// the SSDs beyond the M.2 slots sit in that.
func m2SlotsMatch(expected, detected []string, pcieSlot bool) bool {
	free := slices.Clone(expected)
	loose := 0
	for _, d := range detected {
		if d == "m2" {
			loose++
			continue
		}
		i := slices.Index(free, d)
		if i < 0 {
			return false
		}
		free = slices.Delete(free, i, i+1)
	}
	return pcieSlot || loose <= len(free)
}

var issueRefRe = regexp.MustCompile(`/(?:issues|pull)/(\d+)$`)

// PanelStatus is how well the front panel (buttons and beeper) is known on the model, from the registry's panel check
// (web/models/*.ts), for `ugreen-nas panel` and the diag: "confirmed by users, #165" or "vendor source, not yet confirmed".
func PanelStatus(product string) string {
	unconfirmed := panel.Unconfirmed
	models, err := loadRegistry(modelsJSON)
	if err != nil {
		return unconfirmed
	}
	reg := matchModel(models, product)
	if reg == nil {
		return unconfirmed
	}
	c, ok := reg.Verified["panel"]
	if !ok {
		return unconfirmed
	}
	var text string
	switch c.Status {
	case "verified":
		text = "verified by a maintainer"
	case "community":
		text = "confirmed by users"
	case "partial":
		text = "partly confirmed by users"
	default:
		return unconfirmed
	}
	if m := issueRefRe.FindStringSubmatch(c.Ref); m != nil {
		text += ", #" + m[1]
	}
	return text
}

// PanelConfirmed says whether users (or a maintainer) confirmed the front panel on the model: the registry's panel check
// is community or verified. A partial check (the beeper only), an expected one or an unknown model is not enough for the
// shutdown on a held power button.
func PanelConfirmed(product string) bool {
	models, err := loadRegistry(modelsJSON)
	if err != nil {
		return false
	}
	reg := matchModel(models, product)
	if reg == nil {
		return false
	}
	c, ok := reg.Verified["panel"]
	return ok && (c.Status == "community" || c.Status == "verified")
}
