package fan

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
)

// What the web page needs to let the user pick an input (api.php fan_inputs, fan_input_test; contract-fan-inputs.ts).

// InputOption is one choice of the input select. Reading is null where the input was not read (scripts are only run
// by the test button and the daemon, never by listing).
type InputOption struct {
	Spec    string   `json:"spec"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Reading *float64 `json:"reading"`
	Usable  bool     `json:"usable"`
	Detail  string   `json:"detail,omitempty"`
}

type InputDisk struct {
	Name    string `json:"name"`
	Device  string `json:"device,omitempty"`
	Temp    *int   `json:"temp"`
	Standby bool   `json:"standby"`
	NVMe    bool   `json:"nvme"`
}

type InputGroupState struct {
	Spec    string   `json:"spec"`
	Reading *float64 `json:"reading"`
	Usable  bool     `json:"usable"`
	Detail  string   `json:"detail,omitempty"`
}

type InputsReport struct {
	OK      bool                       `json:"ok"`
	Options []InputOption              `json:"options"`
	Disks   []InputDisk                `json:"disks"`
	Groups  map[string]InputGroupState `json:"groups"`
}

type InputTestResult struct {
	OK      bool     `json:"ok"`
	Spec    string   `json:"spec"`
	Reading *float64 `json:"reading"`
	Usable  bool     `json:"usable"`
	Detail  string   `json:"detail,omitempty"`
	Error   string   `json:"error,omitempty"`
}

func reading1(v float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	v = math.Round(v*10) / 10
	return &v
}

// gpuPresent: a GPU hwmon sensor, or an nvidia-smi that answers.
func gpuPresent(ctx context.Context, r *Registry, s Sensors) bool {
	if s.GPU != nil {
		return true
	}
	_, ok := r.Read(ctx, InputSpec{Kind: "gpu"})
	return ok
}

// ListInputs offers only inputs that are present: a GPU, HBA or NIC shows up when probed, a script when its file passes the
// checks. Specs the config already uses stay listed (unusable) so the select never loses the saved value.
func ListInputs(ctx context.Context, r *Registry, c Config, s Sensors) InputsReport {
	r.begin(c, s)
	rep := InputsReport{OK: true, Options: []InputOption{}, Disks: []InputDisk{}, Groups: map[string]InputGroupState{}}
	add := func(spec InputSpec, label string) {
		o := InputOption{Spec: spec.String(), Kind: spec.Kind, Label: label}
		if spec.Kind == "script" {
			if _, err := checkScript(spec.Name); err != nil {
				o.Detail = err.Error()
			} else {
				o.Usable = true
			}
		} else {
			v, ok := r.Read(ctx, spec)
			o.Reading, o.Usable = reading1(v, ok), ok
		}
		if o.Kind == "" {
			o.Kind = "auto"
		}
		rep.Options = append(rep.Options, o)
	}

	autoV, autoOK := AutoInput("case", c, s)
	rep.Options = append(rep.Options, InputOption{Spec: "auto", Kind: "auto", Label: "auto", Reading: reading1(autoV, autoOK), Usable: autoOK})
	if s.CPU != nil {
		add(InputSpec{Kind: "cpu"}, "CPU")
	}
	if len(s.Disks) > 0 {
		add(InputSpec{Kind: "disk_max"}, "hottest drive")
		add(InputSpec{Kind: "disk_avg"}, "average of the drives")
	}
	if len(s.NVMe) > 0 {
		add(InputSpec{Kind: "nvme"}, "NVMe")
	}
	if gpuPresent(ctx, r, s) {
		add(InputSpec{Kind: "gpu"}, "GPU")
	}
	if _, ok := r.Read(ctx, InputSpec{Kind: "hba"}); ok {
		add(InputSpec{Kind: "hba"}, "HBA")
	}
	seen := map[string]bool{}
	for _, n := range s.NIC {
		if !seen[n.Iface] {
			seen[n.Iface] = true
			add(InputSpec{Kind: "nic", Name: n.Iface}, "network "+n.Iface)
		}
	}
	for _, name := range ScriptNames() {
		add(InputSpec{Kind: "script", Name: name}, "script "+name)
	}

	for _, d := range s.Disks {
		rep.Disks = append(rep.Disks, InputDisk{Name: d.Name, Device: d.Device, Temp: d.Temp, Standby: d.Standby})
	}
	for _, n := range s.NVMe {
		rep.Disks = append(rep.Disks, InputDisk{Name: n.Name, Device: n.Name, Temp: n.Temp, NVMe: true})
	}

	for role, g := range map[string]Group{"cpu": c.CPU, "case": c.Case} {
		st := InputGroupState{Spec: g.Input.String()}
		switch {
		case g.Input.Kind == "":
			v, ok := AutoInput(role, c, s)
			st.Reading, st.Usable = reading1(v, ok), ok
		case usesScript(g.Input):
			st.Detail = "scripts run only in the daemon"
		default:
			v, ok := r.Read(ctx, g.Input)
			st.Reading, st.Usable = reading1(v, ok), ok
		}
		rep.Groups[role] = st
	}
	return rep
}

func usesScript(in InputSpec) bool {
	if in.Kind == "script" {
		return true
	}
	for _, p := range in.Parts {
		if usesScript(p) {
			return true
		}
	}
	return false
}

func scriptNamesOf(in InputSpec) []string {
	if in.Kind == "script" {
		return []string{in.Name}
	}
	var names []string
	for _, p := range in.Parts {
		names = append(names, scriptNamesOf(p)...)
	}
	return names
}

// inputTestGap: the least time between two test runs of one script (a fresh process per click, so the stamp is a file).
const inputTestGap = 2 * time.Second

var inputTestDir = envOr("UGREEN_INPUT_TEST_DIR", "/var/run")

// rateLimitTest refuses a script test that follows another within inputTestGap; otherwise it stamps the script.
func rateLimitTest(now time.Time, name string) error {
	if !scriptRe.MatchString(name) {
		return fmt.Errorf("%q is not a valid script name", name)
	}
	stamp := filepath.Join(inputTestDir, "ugreen-nas-control.input-test."+name)
	if st, err := os.Stat(stamp); err == nil { //nolint:gosec // name matches scriptRe above
		if age := now.Sub(st.ModTime()); age >= 0 && age < inputTestGap {
			return fmt.Errorf("script %s was tested a moment ago, try again in %d s", name, int((inputTestGap-age).Seconds())+1)
		}
	}
	if err := fsx.WriteFile(stamp, nil, 0o600); err != nil { //nolint:gosec // name matches scriptRe above
		return nil // no stamp directory is no reason to refuse
	}
	return os.Chtimes(stamp, now, now) //nolint:gosec // same path
}

// TestInput reads one spec once and says what came out. Scripts are run in the sandbox, at most once per inputTestGap.
func TestInput(ctx context.Context, r *Registry, c Config, specText string) InputTestResult {
	res := InputTestResult{Spec: specText}
	spec, err := ParseInputSpec(specText)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Spec = spec.String()
	names := scriptNamesOf(spec)
	sort.Strings(names)
	for _, n := range names {
		if err := rateLimitTest(r.now(), n); err != nil {
			res.Error = err.Error()
			return res
		}
	}
	res.OK = true
	r.Tick(nil)
	var v float64
	var ok bool
	if spec.Kind == "" {
		v, ok = AutoInput("case", c, r.sensorSnapshotLocked())
	} else {
		v, ok = r.Read(ctx, spec)
	}
	res.Reading, res.Usable = reading1(v, ok), ok
	if !ok {
		res.Detail = r.Detail(spec)
		if res.Detail == "" {
			res.Detail = "no reading"
		}
		for _, n := range names {
			if d := r.Detail(InputSpec{Kind: "script", Name: n}); d != "" {
				res.Detail = d
			}
		}
	}
	return res
}

func (r *Registry) sensorSnapshotLocked() Sensors {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sensorSnapshot()
}
