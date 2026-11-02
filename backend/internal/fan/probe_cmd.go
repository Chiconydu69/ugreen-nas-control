package fan

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

type ProbeStatus struct {
	OK         bool           `json:"ok"`
	Model      string         `json:"model"`
	Kind       string         `json:"kind"`                // fixed, probe or "" (no fan control)
	Map        *FanMap        `json:"map"`                 // the confirmed map, null until confirmed
	MapError   string         `json:"map_error,omitempty"` // a map file exists but does not fit this NAS
	Proposal   *ProbeProposal `json:"proposal"`            // last complete run, waiting for confirmation
	Progress   *ProbeProgress `json:"progress"`
	FanControl bool           `json:"fan_control_possible"` // false until the map is confirmed
}

func readProgress(path string) *ProbeProgress {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var p ProbeProgress
	if json.Unmarshal(b, &p) != nil {
		return nil
	}
	if p.State == "running" && time.Since(time.Unix(p.Time, 0)) > progressFree {
		p.State, p.Reason = "aborted", "stopped"
	}
	return &p
}

func readProposal(model string) *ProbeProposal {
	b, err := os.ReadFile(probeProposalPath)
	if err != nil {
		return nil
	}
	var p ProbeProposal
	if json.Unmarshal(b, &p) != nil || p.Version != 1 || p.Model != model || time.Since(time.Unix(p.Time, 0)) > proposalFresh {
		return nil
	}
	return &p
}

// errNoIT8613Probe: the IT8613E header probe writes to Super-I/O registers; the DXP2800 GT has a Chipsea CS201x
// and goes through `cs201x confirm`. Every probe-fans path checks it first, so no IT8613E write can happen there.
func errNoIT8613Probe(model string) error {
	if isDXP2800GT(model) {
		return fmt.Errorf("%s has a Chipsea CS201x, not an IT8613E: use /usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan cs201x", model)
	}
	if FanKind(model) != KindProbe {
		return fmt.Errorf("%s has no fan probe: its fans are wired as known", orUnknown(model))
	}
	return nil
}

func probeStatus() ProbeStatus {
	model := productName()
	st := ProbeStatus{OK: true, Model: model, Kind: FanKind(model)}
	if isDXP2800GT(model) {
		// Confirmed through `cs201x confirm`; no proposal, no probe progress.
		m, err := loadCS201xMap(model)
		switch {
		case err == nil:
			st.Map, st.FanControl = m, true
		case !errors.Is(err, errNoFanMap):
			st.MapError = err.Error()
		}
		return st
	}
	if st.Kind != KindProbe {
		st.FanControl = st.Kind == KindFixed
		return st
	}
	m, err := loadFanMap(model)
	switch {
	case err == nil:
		st.Map, st.FanControl = m, true
	case !errors.Is(err, errNoFanMap):
		st.MapError = err.Error()
	}
	st.Proposal, st.Progress = readProposal(model), readProgress(probeStatePath)
	return st
}

// confirmProbe turns the proposal into the fan map. choice: output -> role ("cpu" or "case"); outputs not named
// are left alone. Only outputs the probe found a fan on can be confirmed.
func confirmProbe(choice map[string]string) (*FanMap, error) {
	model := productName()
	if err := errNoIT8613Probe(model); err != nil {
		return nil, err
	}
	prop := readProposal(model)
	if prop == nil {
		return nil, errors.New("no fresh probe result for this NAS: run the fan probe first")
	}
	if len(choice) == 0 {
		return nil, errors.New("name at least one fan, e.g. pwm2=cpu pwm3=case")
	}
	found := map[string]ProbeOutput{}
	for _, o := range prop.Outputs {
		found[o.PWM] = o
	}
	for pwm, role := range choice {
		o, ok := found[pwm]
		switch {
		case !ok:
			return nil, fmt.Errorf("%s is not in the probe result", pwm)
		case o.Status != "mapped":
			return nil, fmt.Errorf("%s has no single fan (%s); it cannot be confirmed", pwm, o.Status)
		case role != "cpu" && role != "case":
			return nil, fmt.Errorf("%s: role must be cpu or case", pwm)
		}
	}
	outs, err := allocateIDs(choice)
	if err != nil {
		return nil, err
	}
	for i := range outs {
		outs[i].Tach = found[outs[i].PWM].Tach
	}
	m := &FanMap{Version: 1, Model: model, Chip: prop.Chip, Time: time.Now().Unix(), Outputs: outs}
	if err := m.validate(model); err != nil {
		return nil, err
	}
	if err := writeJSONAtomic(fanMapPath, m); err != nil {
		return nil, fmt.Errorf("cannot save %s: %w", fanMapPath, err)
	}
	_ = os.Remove(probeProposalPath)
	return m, nil
}

func forgetMap() error {
	if err := os.Remove(fanMapPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// restoreProbe writes back the state a killed probe left behind (the probe restores itself on every normal exit).
func restoreProbe() (bool, error) {
	if isDXP2800GT(productName()) {
		return false, errNoIT8613Probe(productName())
	}
	b, err := os.ReadFile(probeRestorePath)
	if err != nil {
		return false, nil
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return false, err
	}
	ports, err := hw.OpenPorts()
	if err != nil {
		return false, err
	}
	defer func() { _ = ports.Close() }()
	var c *Chip
	if err := withLock(func() (err error) { c, err = Detect(ports); return }); err != nil {
		return false, err
	}
	if err := restoreRig(iteRig{c}, s, withLock, func(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...) }); err != nil {
		return false, err
	}
	_ = os.Remove(probeRestorePath)
	return true, nil
}

// readTachs reads every tach input without writing anything: the read-only view of a probe model without a map.
func readTachs() (any, error) {
	model := productName()
	if err := errNoIT8613Probe(model); err != nil {
		return nil, err
	}
	if !LoadConfig(configPath).FanControl {
		return nil, errors.New("fan control is switched off in the settings")
	}
	ports, err := hw.OpenPorts()
	if err != nil {
		return nil, err
	}
	defer func() { _ = ports.Close() }()
	type tach struct {
		ID  string `json:"id"`
		RPM int    `json:"rpm"`
	}
	var out []tach
	err = withLock(func() error {
		c, err := Detect(ports)
		if err != nil {
			return err
		}
		rig := iteRig{c}
		for _, id := range rig.Tachs() {
			rpm, err := rig.RPM(id)
			if err != nil {
				return err
			}
			out = append(out, tach{id, rpm})
		}
		return nil
	})
	return map[string]any{"ok": true, "tachs": out}, err
}

func parseChoice(args []string) (map[string]string, error) {
	choice := map[string]string{}
	for _, a := range args {
		pwm, role, ok := strings.Cut(a, "=")
		if !ok || pwm == "" {
			return nil, fmt.Errorf("%q: expected output=role, e.g. pwm2=cpu", a)
		}
		choice[pwm] = role
	}
	return choice, nil
}

func ProbeCommand(args []string) {
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "run", "confirm", "tachs", "restore":
		if isDXP2800GT(productName()) {
			fail(errNoIT8613Probe(productName()))
		}
	}
	switch args[0] {
	case "status":
		printJSON(probeStatus())
	case "run":
		if err := runProbe(); err != nil {
			fail(err)
		}
		fmt.Println("Fan probe finished; confirm the result to use it.")
	case "confirm":
		choice, err := parseChoice(args[1:])
		if err != nil {
			fail(err)
		}
		m, err := confirmProbe(choice)
		if err != nil {
			fail(err)
		}
		printJSON(map[string]any{"ok": true, "map": m})
	case "forget":
		if err := forgetMap(); err != nil {
			fail(err)
		}
		fmt.Println("Fan map removed; fan control is off on this model until a new probe is confirmed.")
	case "restore":
		done, err := restoreProbe()
		if err != nil {
			fail(err)
		}
		if !done {
			fmt.Println("Nothing to restore: no fan probe was interrupted.")
			return
		}
		fmt.Println("Fan settings from before the probe restored.")
	case "tachs":
		refuseWhileRival()
		out, err := readTachs()
		if err != nil {
			fail(err)
		}
		printJSON(out)
	default:
		usage()
	}
}

// BacklightCommand is `ugreen-nas-fan backlight [set <0-100>]` (iDX6011 Pro).
func BacklightCommand(args []string) {
	if !isIdxPro(productName()) {
		fail(fmt.Errorf("%s has no backlight control: it exists for the iDX6011 Pro only", orUnknown(productName())))
	}
	refuseWhileRival()
	p, err := hw.OpenPorts()
	if err != nil {
		fail(err)
	}
	defer func() { _ = p.Close() }()
	var ec *EC
	if err := withLock(func() (err error) { ec, err = detectEC(p, ecSpecIdxPro); return }); err != nil {
		fail(err)
	}
	switch {
	case len(args) == 0:
		var pct, raw int
		if err := withLock(func() (err error) { pct, raw, err = ec.BacklightPct(); return }); err != nil {
			fail(err)
		}
		out := map[string]any{"ok": true, "pct": pct, "raw": raw}
		if pct == BacklightUnknownPct && raw == 0 {
			out["pct"] = "default" // the register holds the vendor default; the panel shows its own brightness
		}
		printJSON(out)
	case len(args) == 2 && args[0] == "set":
		var pct int
		if _, err := fmt.Sscanf(args[1], "%d", &pct); err != nil || fmt.Sprint(pct) != args[1] || pct < 0 || pct > 100 {
			fail(fmt.Errorf("brightness must be 0-100"))
		}
		if err := withLock(func() error { return ec.SetBacklight(pct) }); err != nil {
			fail(err)
		}
		fmt.Printf("Backlight at %d%%.\n", pct)
	default:
		usage()
	}
}
