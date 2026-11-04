package fan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// KindFixed: the wiring is known (DXP6800 Pro, DXP4800 GT, iDX6011 Pro).
	KindFixed = "fixed"
	// KindProbe: an IT8613E whose header wiring is not known. Fan control stays off until the user confirmed a map
	// found by `probe-fans run`.
	KindProbe = "probe"
	// KindReadOnly: the plain iDX6011/iDX6012. Tach, duty and mode are shown; no register is written, so the
	// daemon, `set` and `calibrate` stay off (ecSpecIdx, errReadOnly).
	KindReadOnly = "readonly"
)

// FanKind says how the model's fans are reached, "" for a model without fan control (every DX and the rest). The
// DXP2800 GT is a probe-like model too: its CS201x fans need `ugreen-nas-fan cs201x confirm`, not the IT8613E probe.
func FanKind(model string) string {
	switch {
	case model == "DXP6800 Pro", isDXP4800GT(model), isIdxPro(model):
		return KindFixed
	case isProbeModel(model), isDXP2800GT(model):
		return KindProbe
	case isIdxPlain(model):
		return KindReadOnly
	}
	return ""
}

// isIdxPro: UGREEN has the Pro driver for DMI "iDX6011 Pro" only; an "iDX6012 Pro" does not exist.
func isIdxPro(model string) bool { return model == "iDX6011 Pro" }

// isIdxPlain: the plain iDX6011 and iDX6012 share one driver (ug_idx6011-sio).
func isIdxPlain(model string) bool { return model == "iDX6011" || model == "iDX6012" }

// isProbeModel: the IT8613E models without a known header map. The IT8613E at 0xa30 on the DXP2800 shows a fan
// on tach 3 and none on tach 2; UGREEN's drivers name the expected wiring (see probeHints), which the probe
// checks; nothing is assumed, the probe decides. DX4600/DX4700 stay out: a source comment is the only evidence.
func isProbeModel(model string) bool {
	switch model {
	case "DXP2800", "DXP4800", "DXP4800 Plus", "DXP4800 Pro", "DXP6800", "DXP6800 Plus", "DXP8800", "DXP8800 Pro", "DXP8800 Plus":
		return true
	}
	return false
}

// probeHint is the wiring the probe expects for a model; the probe compares its result with it, never trusts it.
// Sources (facts only, nothing copied): UGREEN's own GPL-2.0-only fan drivers (ug_it86x-cpufan.c:221-275,
// ug_it86x-sio.c:234-236@c75c5ab: which header is driven as cpu and as case fan per DMI name; the code never shows
// that the fan on header N is the one read on tach N, only the author's rpm samples at ug_it86x-cpufan.c:48-73 agree)
// and the community reports IT-Kuny/UGREEN-DXP-FAN-NAS-Driver README:262 (GPL-3.0; DXP2800: IT8613E at 0xa30, fan2
// 0 rpm, fan3 1726 rpm) and smashah/ugreen-dxp-fan-cli (DXP4800 Plus drives pwm2 and pwm3). A strong hint, not proof
// of the physical wiring: fan control of these models stays off until a probe result is confirmed.
//
// UGREEN's fan driver matches the prefix "DXP6800" and "DXP8800" (ug_it86x-cpufan.c:227-235@c75c5ab), so the plain
// DXP6800, "DXP6800 Plus", the plain DXP8800 and "DXP8800 Pro" share the DXP6800 Pro / DXP8800 Plus wiring on paper;
// only the DXP6800 Pro is verified, the others are probed. DXP6800 Ultra and DXP6800S have no model file with fans
// and stay without fan control.
//
// Header 4 is unconfirmed: UGREEN's driver names 0x1e as its control register (ug_it86x-cpufan.c:82-83@c75c5ab), we
// write 0x7f (it8613Headers). Until a device settles it, no new model gets to write header 4 (isProbeModel).
type probeHint struct {
	Wiring []FanMapOutput // ID is left empty: the probe's ids come from allocateIDs
}

func hintOutputs(roles ...string) []FanMapOutput {
	// roles are "header:role", e.g. "3:case".
	out := make([]FanMapOutput, 0, len(roles))
	for _, r := range roles {
		out = append(out, FanMapOutput{Role: r[2:], PWM: "pwm" + r[:1], Tach: "fan" + r[:1]})
	}
	return out
}

var probeHints = map[string]probeHint{
	"DXP2800": {hintOutputs("3:case")},
	// ug_it86x-cpufan.c:221-226,265: header 3 only; tach 2 is read but no PWM 2 is written.
	"DXP4800":      {hintOutputs("3:case")},
	"DXP4800 Plus": {hintOutputs("2:cpu", "3:case")},
	"DXP4800 Pro":  {hintOutputs("2:cpu", "3:case")},
	// ug_it86x-cpufan.c:227-235,265-270,441-451: both case headers get the same duty.
	"DXP8800 Plus": {hintOutputs("2:cpu", "3:case", "4:case")},
	"DXP8800":      {hintOutputs("2:cpu", "3:case", "4:case")},
	"DXP8800 Pro":  {hintOutputs("2:cpu", "3:case", "4:case")},
	"DXP6800":      {hintOutputs("2:cpu", "3:case", "4:case")},
	"DXP6800 Plus": {hintOutputs("2:cpu", "3:case", "4:case")},
	// Not a probe model (header 4, no model file with fans): hint only. cpu on header 3, case on 2 and 4
	// (ug_it86x-cpufan.c:236-243,271-275@c75c5ab).
	"DXP480T Plus": {hintOutputs("3:cpu", "2:case", "4:case")},
}

// Fan floor of the probe models: the driver never writes less, and the probe never drives below it. Case fans of
// this class stall near 20 % (iDX6011 Pro measurement of the same family, noodlemctwoodle fan.h:25-31), so 25 %.
const probeFloorPct = 25

// Limits found for single models, more conservative than the defaults: iDX6011 Pro critical CPU 88 °C, disk 60 °C
// (noodlemctwoodle/unraid-ugreen-idx6011-pro, MIT, src/panel/fan.h:25-31@d202a7c).
func modelLimits(model string) (cpuCrit, diskCrit int, ok bool) {
	if isIdxPro(model) {
		return 88, 60, true
	}
	return 0, 0, false
}

// ApplyModelLimits lowers the emergency limits of a config to what the model needs; it never raises one. The minimum
// speed never goes below the duty the hardware applies (DXP2800 GT: csFloorPct), so that the targets the daemon records
// are what runs.
func ApplyModelLimits(c Config, model string) Config {
	c.MinPct = max(c.MinPct, minPctFloor(model))
	if cpu, disk, ok := modelLimits(model); ok {
		c.CPUCrit = min(c.CPUCrit, cpu)
		c.DiskCrit = min(c.DiskCrit, disk)
	}
	return c
}

// The confirmed fan map of a probe model. Without a valid one, fan control stays off.
var fanMapPath = envOr("UGREEN_FAN_MAP", filepath.Join(filepath.Dir(configPath), "fan_map.json"))

// FanMap is the file /boot/config/plugins/ugreen-nas-control/fan_map.json.
type FanMap struct {
	Version int            `json:"version"`
	Model   string         `json:"model"`
	Chip    string         `json:"chip"`
	Time    int64          `json:"time"`
	Outputs []FanMapOutput `json:"outputs"`
}

// FanMapOutput: fan ID (fan1..fan4, the daemon's roles) driven by output PWM, read on tach input Tach.
type FanMapOutput struct {
	ID   string `json:"id"`
	Role string `json:"role"` // cpu or case
	PWM  string `json:"pwm"`
	Tach string `json:"tach"`
}

var errNoFanMap = errors.New("no confirmed fan map")

// loadFanMap reads the map; errNoFanMap when there is none, another error when it does not fit this NAS.
func loadFanMap(model string) (*FanMap, error) {
	if isDXP2800GT(model) {
		return loadCS201xMap(model)
	}
	b, err := os.ReadFile(fanMapPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoFanMap
	}
	if err != nil {
		return nil, err
	}
	var m FanMap
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("fan map unreadable: %w", err)
	}
	if err := m.validate(model); err != nil {
		return nil, fmt.Errorf("fan map ignored: %w", err)
	}
	return &m, nil
}

func (m *FanMap) validate(model string) error {
	if m.Version != 1 || m.Chip != "IT8613" {
		return errors.New("unknown format")
	}
	if m.Model != model {
		return fmt.Errorf("made for %q, this is %q", m.Model, model)
	}
	if len(m.Outputs) == 0 || len(m.Outputs) > itePWMCount {
		return errors.New("no fans in it")
	}
	var ids, pwms, tachs []string
	for _, o := range m.Outputs {
		if _, ok := iteHeaderOf("pwm", o.PWM, itePWMCount); !ok {
			return fmt.Errorf("unknown output %q", o.PWM)
		}
		if _, ok := iteHeaderOf("fan", o.Tach, len(it8613Headers)); !ok {
			return fmt.Errorf("unknown tach %q", o.Tach)
		}
		if r, ok := roles[o.ID]; !ok || r != o.Role {
			return fmt.Errorf("fan %q is not a %s fan", o.ID, o.Role)
		}
		if slices.Contains(ids, o.ID) || slices.Contains(pwms, o.PWM) || slices.Contains(tachs, o.Tach) {
			return errors.New("an id, output or tach is used twice")
		}
		ids, pwms, tachs = append(ids, o.ID), append(pwms, o.PWM), append(tachs, o.Tach)
	}
	return nil
}

// channels turns the map into daemon channels: tach of one header, PWM of another where the board crosses them.
func (m *FanMap) channels() []Channel {
	out := make([]Channel, 0, len(m.Outputs))
	for _, o := range m.Outputs {
		t, _ := iteHeaderOf("fan", o.Tach, len(it8613Headers))
		p, _ := iteHeaderOf("pwm", o.PWM, itePWMCount)
		out = append(out, iteChannel(o.ID, t, p))
	}
	slices.SortFunc(out, func(a, b Channel) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// allocateIDs gives each chosen output a fan ID that fits its role (the daemon's roles table is fixed:
// fan1, fan2 = cpu, fan3, fan4 = case). "fanN" for pwmN when that fits, else the first free one.
func allocateIDs(choice map[string]string) ([]FanMapOutput, error) {
	pwms := make([]string, 0, len(choice))
	for pwm := range choice {
		pwms = append(pwms, pwm)
	}
	slices.Sort(pwms)

	used := map[string]bool{}
	var out []FanMapOutput
	for _, pwm := range pwms {
		n, _ := iteHeaderOf("pwm", pwm, itePWMCount)
		id := tachID(n + 1)
		if roles[id] == choice[pwm] && !used[id] {
			used[id] = true
			out = append(out, FanMapOutput{ID: id, Role: choice[pwm], PWM: pwm})
		}
	}
	for _, pwm := range pwms {
		if slices.ContainsFunc(out, func(o FanMapOutput) bool { return o.PWM == pwm }) {
			continue
		}
		var id string
		for _, c := range []string{"fan1", "fan2", "fan3", "fan4"} {
			if roles[c] == choice[pwm] && !used[c] {
				id = c
				break
			}
		}
		if id == "" {
			return nil, fmt.Errorf("at most two %s fans", choice[pwm])
		}
		used[id] = true
		out = append(out, FanMapOutput{ID: id, Role: choice[pwm], PWM: pwm})
	}
	slices.SortFunc(out, func(a, b FanMapOutput) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// minPctFloor is the lowest speed the hardware of the model runs at; 0 when it has none above the configurable minimum.
func minPctFloor(model string) int {
	if isDXP2800GT(model) {
		return csFloorPct
	}
	return 0
}
