package fan

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A fan input is where the temperature of a group comes from (InputSpec in config_v2.go names it). The daemon asks
// a Registry once per tick: Tick hands over that tick's sensor snapshot, Read answers from it.
//
// Safety (docs: fan features, invariants 2 and 5): an unreadable input reports ok=false and the caller falls back by
// the group's on_missing rule; no reader here ever writes the fan chip, and a script, GPU, HBA or NIC temperature never
// feeds the disk emergency rule (the caller only compares disks and NVMe to disk_crit).

type Input interface {
	// Read gives the temperature in °C, false when the input has no usable reading right now.
	Read(ctx context.Context) (float64, bool)
}

const (
	// slowTTL: a script, nvidia-smi or storcli runs at most this often; the answer (or the failure) is reused in between.
	slowTTL = 10 * time.Second
	// Plausible range of an external temperature: outside it the reading is treated as missing.
	scriptMin, scriptMax = -20.0, 150.0
)

type Registry struct {
	mu      sync.Mutex
	now     func() time.Time
	sensors func() Sensors
	logf    func(format string, args ...any)

	snap   *Sensors
	ignore map[string]bool // ignore_drives of the tick: left out of the drive and NVMe readings
	tick   map[string]reading
	slow   map[string]slowEntry
	// logged: the last failure reported per slow source, so a broken script is logged once, not every 10 s.
	logged map[string]string
	detail map[string]string
}

type reading struct {
	v  float64
	ok bool
}

type slowEntry struct {
	reading
	at     time.Time
	detail string
}

// NewRegistry reads the real system. logf may be nil.
func NewRegistry(logf func(string, ...any)) *Registry {
	return newRegistry(time.Now, ReadSensors, logf)
}

func newRegistry(now func() time.Time, sensors func() Sensors, logf func(string, ...any)) *Registry {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Registry{
		now: now, sensors: sensors, logf: logf,
		tick: map[string]reading{}, slow: map[string]slowEntry{}, logged: map[string]string{}, detail: map[string]string{},
	}
}

// Tick starts a tick: the per-tick cache is dropped and s (when not nil) is the sensor snapshot every input of
// this tick reads from; with nil the registry reads the sensors itself on first use.
func (r *Registry) Tick(s *Sensors) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snap = s
	r.ignore = nil
	clear(r.tick)
}

func (r *Registry) begin(c Config, s Sensors) {
	r.Tick(&s)
	r.mu.Lock()
	r.ignore = c.IgnoreDrives
	r.mu.Unlock()
}

func (r *Registry) Input(spec InputSpec) Input { return boundInput{r, spec} }

type boundInput struct {
	r    *Registry
	spec InputSpec
}

func (b boundInput) Read(ctx context.Context) (float64, bool) { return b.r.Read(ctx, b.spec) }

// Detail is why the spec's last slow reading failed ("" when it works or has not been tried).
func (r *Registry) Detail(spec InputSpec) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.detail[spec.String()]
}

// Read gives the temperature of spec. The auto spec has no reader here: it depends on the config (ignored drives,
// NVMe offset); the daemon answers it with AutoInput.
func (r *Registry) Read(ctx context.Context, spec InputSpec) (float64, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read(ctx, spec)
}

func (r *Registry) sensorSnapshot() Sensors {
	if r.snap == nil {
		s := r.sensors()
		r.snap = &s
	}
	return *r.snap
}

func (r *Registry) read(ctx context.Context, spec InputSpec) (float64, bool) {
	key := spec.String()
	if got, ok := r.tick[key]; ok {
		return got.v, got.ok
	}
	var got reading
	switch spec.Kind {
	case "cpu":
		if s := r.sensorSnapshot(); s.CPU != nil {
			got = reading{float64(*s.CPU), true}
		}
	case "nvme":
		got = hottest(nvmeTemps(r.sensorSnapshot(), nil, r.ignore))
	case "disk_max":
		got = hottest(diskTemps(r.sensorSnapshot(), spec.Disks, r.ignore))
	case "disk_avg":
		got = average(diskTemps(r.sensorSnapshot(), spec.Disks, r.ignore))
	case "nic":
		got = hottest(nicTemps(r.sensorSnapshot(), spec.Name))
	case "gpu":
		got = r.readGPU(ctx)
	case "hba":
		got = r.readHBA(ctx)
	case "script":
		got = r.readScript(ctx, spec.Name)
	case "max":
		var temps []float64
		for _, p := range spec.Parts {
			if v, ok := r.read(ctx, p); ok {
				temps = append(temps, v)
			}
		}
		got = hottest(temps)
	}
	r.tick[key] = got
	return got.v, got.ok
}

func hottest(temps []float64) reading {
	if len(temps) == 0 {
		return reading{}
	}
	m := temps[0]
	for _, t := range temps[1:] {
		m = max(m, t)
	}
	return reading{m, true}
}

func average(temps []float64) reading {
	if len(temps) == 0 {
		return reading{}
	}
	sum := 0.0
	for _, t := range temps {
		sum += t
	}
	return reading{sum / float64(len(temps)), true}
}

// diskTemps: the awake, readable, not ignored drives among names (Unraid names such as disk1 or cache, or nvme0); no names = every
// array and cache drive. A spun-down drive is skipped, never woken up.
func diskTemps(s Sensors, names []string, ignore map[string]bool) []float64 {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []float64
	for _, d := range s.Disks {
		if d.Temp != nil && !d.Standby && !ignore[d.Name] && (len(want) == 0 || want[d.Name]) {
			out = append(out, float64(*d.Temp))
		}
	}
	if len(want) > 0 {
		out = append(out, nvmeTemps(s, want, ignore)...)
	}
	return out
}

func nvmeTemps(s Sensors, want, ignore map[string]bool) []float64 {
	var out []float64
	for _, n := range s.NVMe {
		if n.Temp != nil && !ignore[n.Name] && (len(want) == 0 || want[n.Name]) {
			out = append(out, float64(*n.Temp))
		}
	}
	return out
}

func nicTemps(s Sensors, iface string) []float64 {
	var out []float64
	for _, n := range s.NIC {
		if n.Iface == iface && n.Temp != nil {
			out = append(out, float64(*n.Temp))
		}
	}
	return out
}

// AutoInput is the default input of a group, as the fans always had it: the CPU package for "cpu"; for "case" the
// hottest drive or the hottest NVMe minus nvme_offset, ignored drives left out.
func AutoInput(role string, c Config, s Sensors) (float64, bool) {
	if role == "cpu" {
		if s.CPU == nil {
			return 0, false
		}
		return float64(*s.CPU), true
	}
	disk, nvme := s.MaxDiskExcept(c.IgnoreDrives), s.MaxNVMeExcept(c.IgnoreDrives)
	switch {
	case disk != nil && (nvme == nil || *disk >= *nvme-c.NVMeOffset):
		return float64(*disk), true
	case nvme != nil:
		return float64(*nvme - c.NVMeOffset), true
	}
	return 0, false
}

// slowRead runs fetch at most once per slowTTL for key; the failure is kept for that time too, so a broken
// script is not started again every tick, and it is logged once until it changes.
func (r *Registry) slowRead(key string, fetch func() (float64, error)) reading {
	now := r.now()
	if e, ok := r.slow[key]; ok && now.Sub(e.at) < slowTTL && !now.Before(e.at) {
		return e.reading
	}
	v, err := fetch()
	e := slowEntry{reading: reading{v, err == nil}, at: now}
	if err != nil {
		e.detail = err.Error()
		if r.logged[key] != e.detail {
			r.logged[key] = e.detail
			r.logf("fan input %s unusable: %s", key, e.detail)
		}
	} else if _, was := r.logged[key]; was {
		delete(r.logged, key)
		r.logf("fan input %s works again", key)
	}
	r.slow[key] = e
	r.detail[key] = e.detail
	return e.reading
}

var numberRe = regexp.MustCompile(`^-?[0-9]{1,4}(\.[0-9]{1,6})?$`)

// parseTemp: one plain decimal number, nothing else, within the plausible range.
func parseTemp(out []byte) (float64, error) {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, fmt.Errorf("no output")
	}
	if !numberRe.MatchString(s) {
		return 0, fmt.Errorf("output is not a single number")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("output is not a number")
	}
	if v < scriptMin || v > scriptMax {
		return 0, fmt.Errorf("%s °C is outside %.0f..%.0f", s, scriptMin, scriptMax)
	}
	return v, nil
}
