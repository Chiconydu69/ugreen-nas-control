package fan

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"sort"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// Fan probe. On an IT8613E whose header wiring is not known, it finds out which PWM output moves which tachometer
// instead of trusting a table: one output at a time goes to a test speed while all others hold, every tach is read,
// and the tach that responds belongs to that output. The result is only a proposal; it becomes the fan map (and
// switches fan control on) when the user confirms it (probe_cmd.go).
//
// Safety, in the order the code applies it:
//   - runs only on an explicit command, holding the calibration lock (so not beside a calibration) and a fresh pause
//     file (the daemon keeps its hands off the fans);
//   - nothing is written below the model floor (probeFloorPct), by the prober and again by the chip;
//   - the state before the first write is kept in memory and in probeRestorePath (a killed probe can be undone with
//     `probe-fans restore`), and written back on every exit: done, aborted, error, panic, SIGTERM/SIGINT;
//   - each test lasts at most probeWindow; the temperature rules of the daemon (emergency limits, model limits) and a
//     CPU ceiling are checked every second, and any breach aborts;
//   - a fan that turned and then reads 0 rpm twice in a row (stall) aborts.
var (
	probeStatePath    = envOr("UGREEN_FAN_PROBE_STATE", "/var/run/ugreen-nas-control.probe.json")
	probeProposalPath = envOr("UGREEN_FAN_PROBE_PROPOSAL", "/var/run/ugreen-nas-control.probe-proposal.json")
	probeRestorePath  = envOr("UGREEN_FAN_PROBE_RESTORE", "/var/run/ugreen-nas-control.probe-restore.json")
)

const (
	probeHoldPct = 50 // every output that is not under test
	probeTestPct = 80 // the output under test
	// A tach counts as responding when it rose by this much over its reading before the test (and by a quarter of it),
	// on two reads in the window.
	probeMinRise = 150
	probeHits    = 2

	probeSettle  = 4 * time.Second
	probeWindow  = 8 * time.Second // test duration per output, the hard limit
	probePoll    = time.Second
	probeStalled = 2 // reads at 0 rpm in a row

	proposalFresh = 24 * time.Hour
)

// probeRig is what the prober needs from the chip. Outputs are "pwm1".., tachs "fan1"..; the rig keeps the floor.
type probeRig interface {
	PWMs() []string
	Tachs() []string
	SetPct(pwm string, pct int) error
	RPM(tach string) (int, error)
	Snapshot() (Snapshot, error)
	Restore(Snapshot) error
}

type ProbeOutput struct {
	PWM string `json:"pwm"`
	// mapped: exactly one tach responded; no_fan: none did; ambiguous: several did, or another output moved the same
	// tach. Only mapped ones can be confirmed.
	Status     string   `json:"status"`
	Tach       string   `json:"tach,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	BaseRPM    int      `json:"base_rpm"`
	PeakRPM    int      `json:"peak_rpm"`
}

type ProbeProposal struct {
	Version int           `json:"version"`
	Model   string        `json:"model"`
	Chip    string        `json:"chip"`
	Time    int64         `json:"time"`
	Outputs []ProbeOutput `json:"outputs"`
	// Expected is what UGREEN's drivers and reports for the model say; Agrees compares it with the finding (nil: nothing reported).
	Expected string `json:"expected,omitempty"`
	Agrees   *bool  `json:"agrees,omitempty"`
}

// ProbeProgress: state running, done or aborted. Reason of an abort: stopped (signal), cpu_hot, disk_hot,
// cpu_unreadable, fan_stalled, read_error, hardware, busy, fan_control_off, rival.
type ProbeProgress struct {
	State  string `json:"state"`
	PWM    string `json:"pwm,omitempty"`
	Step   int    `json:"step"`
	Steps  int    `json:"steps"`
	Reason string `json:"reason,omitempty"`
	Time   int64  `json:"time"`
}

type prober struct {
	rig               probeRig
	chip, model       string
	floor, hold, test int
	settle, window    time.Duration
	poll              time.Duration
	lock              func(func() error) error
	unsafe            func() string // reason to stop, "" when the temperatures are fine
	stop              <-chan struct{}
	progress          func(ProbeProgress)
	logf              func(string, ...any)

	live map[string]bool // tachs that showed a turning fan before the first change
	zero map[string]int  // consecutive 0 rpm reads of a live tach
}

func (p *prober) pct(v int) int { return min(max(v, p.floor), 100) }

func (p *prober) wait(d time.Duration) string {
	end := time.Now().Add(d)
	for {
		left := time.Until(end)
		if left <= 0 {
			return ""
		}
		select {
		case <-p.stop:
			return "stopped"
		case <-time.After(min(left, p.poll)):
		}
		if r := p.unsafe(); r != "" {
			return r
		}
	}
}

func (p *prober) set(pwm string, pct int) string {
	if err := p.lock(func() error { return p.rig.SetPct(pwm, p.pct(pct)) }); err != nil {
		p.logf("fan probe: write to %s failed: %v", pwm, err)
		return "hardware"
	}
	return ""
}

func (p *prober) readAll() (map[string]int, string) {
	out := map[string]int{}
	err := p.lock(func() error {
		for _, t := range p.rig.Tachs() {
			v, err := p.rig.RPM(t)
			if err != nil {
				return err
			}
			out[t] = v
		}
		return nil
	})
	if err != nil {
		p.logf("fan probe: tach read failed: %v", err)
		return nil, "read_error"
	}
	return out, ""
}

// stalled aborts when a tach that showed a turning fan reads 0 on several reads in a row.
func (p *prober) stalled(rpms map[string]int) string {
	for t, v := range rpms {
		switch {
		case !p.live[t]:
		case v == 0:
			if p.zero[t]++; p.zero[t] >= probeStalled {
				p.logf("fan probe: %s stopped turning", t)
				return "fan_stalled"
			}
		default:
			p.zero[t] = 0
		}
	}
	return ""
}

func rose(base, now int) bool { return now-base >= max(probeMinRise, base/4) }

func (p *prober) run() (ProbeProposal, string) {
	prop := ProbeProposal{Version: 1, Model: p.model, Chip: p.chip}
	p.live, p.zero = map[string]bool{}, map[string]int{}
	pwms := p.rig.PWMs()

	cur, r := p.readAll()
	if r != "" {
		return prop, r
	}
	for t, v := range cur {
		p.live[t] = v > 0
	}

	for _, pwm := range pwms {
		if r := p.set(pwm, p.hold); r != "" {
			return prop, r
		}
	}
	if r := p.wait(p.settle); r != "" {
		return prop, r
	}
	base, r := p.readAll()
	if r != "" {
		return prop, r
	}
	if r := p.stalled(base); r != "" {
		return prop, r
	}

	for i, pwm := range pwms {
		if r := p.unsafe(); r != "" {
			return prop, r
		}
		p.progress(ProbeProgress{State: "running", PWM: pwm, Step: i + 1, Steps: len(pwms), Time: time.Now().Unix()})
		if r := p.set(pwm, p.test); r != "" {
			return prop, r
		}

		hits, peak := map[string]int{}, map[string]int{}
		for end := time.Now().Add(p.window); time.Now().Before(end); {
			if r := p.wait(min(p.poll, time.Until(end))); r != "" {
				return prop, r
			}
			rpms, r := p.readAll()
			if r != "" {
				return prop, r
			}
			if r := p.stalled(rpms); r != "" {
				return prop, r
			}
			for t, v := range rpms {
				peak[t] = max(peak[t], v)
				if rose(base[t], v) {
					hits[t]++
				}
			}
		}

		out := ProbeOutput{PWM: pwm, Status: "no_fan"}
		for _, t := range p.rig.Tachs() {
			if hits[t] >= probeHits {
				out.Candidates = append(out.Candidates, t)
			}
		}
		switch len(out.Candidates) {
		case 1:
			t := out.Candidates[0]
			out.Status, out.Tach, out.Candidates = "mapped", t, nil
			out.BaseRPM, out.PeakRPM = base[t], peak[t]
		default:
			if len(out.Candidates) > 1 {
				out.Status = "ambiguous"
			}
		}
		prop.Outputs = append(prop.Outputs, out)

		// Back to the holding speed and let it settle; the next output starts from there.
		if r := p.set(pwm, p.hold); r != "" {
			return prop, r
		}
		if r := p.wait(p.settle); r != "" {
			return prop, r
		}
		if base, r = p.readAll(); r != "" {
			return prop, r
		}
		if r := p.stalled(base); r != "" {
			return prop, r
		}
	}

	// Two outputs on one tach cannot both be right.
	seen := map[string]int{}
	for _, o := range prop.Outputs {
		if o.Status == "mapped" {
			seen[o.Tach]++
		}
	}
	for i, o := range prop.Outputs {
		if o.Status == "mapped" && seen[o.Tach] > 1 {
			prop.Outputs[i].Candidates, prop.Outputs[i].Status, prop.Outputs[i].Tach = []string{o.Tach}, "ambiguous", ""
		}
	}
	prop.Time = time.Now().Unix()
	prop.Expected, prop.Agrees = compareHint(p.model, prop.Outputs)
	return prop, ""
}

func compareHint(model string, outs []ProbeOutput) (string, *bool) {
	h, ok := probeHints[model]
	if !ok {
		return "", nil
	}
	var found, want, pwms []string
	for _, o := range outs {
		if o.Status == "mapped" {
			found = append(found, o.PWM+":"+o.Tach)
		}
	}
	for _, w := range h.Wiring {
		want, pwms = append(want, w.PWM+":"+w.Tach), append(pwms, w.PWM)
	}
	return "outputs " + joinIDs(pwms), ptr(slices.Equal(sortedCopy(found), sortedCopy(want)))
}

func ptr[T any](v T) *T { return &v }

func sortedCopy(s []string) []string {
	c := slices.Clone(s)
	sort.Strings(c)
	return c
}

func joinIDs(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " and "
		}
		out += v
	}
	return out
}

// restoreRig writes the saved state back, trying again after a failure.
func restoreRig(rig probeRig, s Snapshot, lock func(func() error) error, logf func(string, ...any)) (err error) {
	for i := 1; i <= restoreTries; i++ {
		if err = lock(func() error { return rig.Restore(s) }); err == nil {
			return nil
		}
		logf("fan probe: restoring the fan settings failed (attempt %d of %d): %v", i, restoreTries, err)
		if i < restoreTries {
			time.Sleep(time.Duration(i) * restoreBackoff)
		}
	}
	return err
}

// probeSession is everything around one run: lock, pause, saved state, progress, restore, result files.
type probeSession struct {
	rig    probeRig
	chip   string
	model  string
	unsafe func() string
	lock   func(func() error) error
	logf   func(string, ...any)
	stop   chan struct{}

	settle, window, poll time.Duration
	pauseBeat            time.Duration
}

// execute returns the abort reason ("" when done) and an error for what the caller should print.
func (s *probeSession) execute() (reason string, err error) {
	cal, err := lockCalibration()
	if errors.Is(err, errBusy) {
		return "busy", fmt.Errorf("not started: busy")
	}
	if err != nil {
		return "hardware", fmt.Errorf("not started: %w", err)
	}
	defer func() { _ = cal.Close() }()

	_ = os.Remove(probeProposalPath)
	report := func(pr ProbeProgress) { _ = writeJSONAtomic(probeStatePath, pr) }
	pwms := s.rig.PWMs()

	// Nothing is written until the state to go back to is saved, in memory and on disk.
	var before Snapshot
	if err := s.lock(func() (err error) { before, err = s.rig.Snapshot(); return }); err != nil || len(before) == 0 {
		s.logf("fan probe: cannot read the fan state: %v", err)
		report(ProbeProgress{State: "aborted", Reason: "hardware", Steps: len(pwms), Time: time.Now().Unix()})
		return "hardware", fmt.Errorf("aborted: hardware")
	}
	if err := writeJSONAtomic(probeRestorePath, before); err != nil {
		s.logf("fan probe: cannot save the fan state: %v", err)
		report(ProbeProgress{State: "aborted", Reason: "hardware", Steps: len(pwms), Time: time.Now().Unix()})
		return "hardware", fmt.Errorf("aborted: hardware")
	}

	var lastProgress atomic.Int64
	lastProgress.Store(time.Now().UnixNano())
	beat := func() { _ = fsx.WriteFile(calibPausePath, []byte(time.Now().Format(time.RFC3339)), 0o644) }
	every := s.pauseBeat
	if every == 0 {
		every = 2 * time.Second
	}
	stopBeat := startHeartbeat(beat, every, func() bool {
		return time.Since(time.Unix(0, lastProgress.Load())) < heartbeatStall
	})

	pr := &prober{
		rig: s.rig, chip: s.chip, model: s.model,
		floor: probeFloorPct, hold: probeHoldPct, test: probeTestPct,
		settle: s.settle, window: s.window, poll: s.poll,
		lock: s.lock, unsafe: s.unsafe, stop: s.stop, logf: s.logf,
		progress: func(p ProbeProgress) { lastProgress.Store(time.Now().UnixNano()); report(p) },
	}

	reason = "hardware"
	var prop ProbeProposal
	defer func() {
		crashed := false
		if rec := recover(); rec != nil {
			crashed, reason = true, "hardware"
			err = fmt.Errorf("aborted: crashed: %v", rec)
			s.logf("fan probe crashed: %v", rec)
		}
		stopBeat()
		if rerr := restoreRig(s.rig, before, s.lock, s.logf); rerr != nil {
			s.logf("fan probe: could not restore the fans: %v", rerr)
			if err == nil {
				err = fmt.Errorf("fans not restored: %w", rerr)
			}
		} else {
			_ = os.Remove(probeRestorePath)
		}
		_ = os.Remove(calibPausePath)
		now := time.Now().Unix()
		switch {
		case reason == "" && !crashed:
			report(ProbeProgress{State: "done", Step: len(pwms), Steps: len(pwms), Time: now})
		default:
			report(ProbeProgress{State: "aborted", Reason: reason, Steps: len(pwms), Time: now})
		}
	}()

	prop, reason = pr.run()
	if reason != "" {
		return reason, fmt.Errorf("aborted: %s", reason)
	}
	if err := writeJSONAtomic(probeProposalPath, prop); err != nil {
		reason = "hardware"
		return reason, fmt.Errorf("cannot save the result: %w", err)
	}
	return "", nil
}

func runProbe() error {
	model := productName()
	if err := errNoIT8613Probe(model); err != nil {
		return err
	}
	if !LoadConfig(configPath).FanControl {
		return fmt.Errorf("not started: fan_control_off")
	}
	refuseWhileRival()

	ports, err := hw.OpenPorts()
	if err != nil {
		return err
	}
	defer func() { _ = ports.Close() }()
	var c *Chip
	if err := withLock(func() (err error) { c, err = Detect(ports); return }); err != nil {
		return err
	}
	c.floor = percentToDuty(probeFloorPct, 255)

	stop := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sig)
	go func() {
		<-sig
		close(stop)
	}()

	logf := func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
	cb := &calibrator{
		cfg:         func() Config { return ApplyModelLimits(LoadConfig(configPath), model) },
		sensors:     ReadSensors,
		daemonAlive: func() bool { return true },
		stop:        stop, logf: logf,
	}
	s := &probeSession{
		rig: iteRig{c}, chip: c.Name(), model: model, unsafe: cb.unsafe, lock: withLock, logf: logf, stop: stop,
		settle: probeSettle, window: probeWindow, poll: probePoll,
	}
	_, err = s.execute()
	return err
}

func orUnknown(model string) string {
	if model == "" {
		return "unknown model"
	}
	return model
}
