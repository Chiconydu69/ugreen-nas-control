package fan

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// isolate points every state file of the probe, the calibration and the fan map at a temp dir, and the DMI data at
// the given model.
func isolate(t *testing.T, model string) string {
	t.Helper()
	dir := t.TempDir()
	oldDMI, oldMap, oldState, oldProp, oldRestore := dmiDir, fanMapPath, probeStatePath, probeProposalPath, probeRestorePath
	oldPause, oldLock, oldWdt, oldBackoff, oldMarker := calibPausePath, calibLockPath, watchdogStatePath, restoreBackoff, iteWdtMarkerPath
	t.Cleanup(func() {
		dmiDir, fanMapPath, probeStatePath, probeProposalPath, probeRestorePath = oldDMI, oldMap, oldState, oldProp, oldRestore
		calibPausePath, calibLockPath, watchdogStatePath, restoreBackoff, iteWdtMarkerPath = oldPause, oldLock, oldWdt, oldBackoff, oldMarker
	})
	dmiDir = filepath.Join(dir, "dmi")
	if err := os.MkdirAll(dmiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dmiDir, "product_name"), []byte(model+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fanMapPath = filepath.Join(dir, "fan_map.json")
	probeStatePath = filepath.Join(dir, "probe.json")
	probeProposalPath = filepath.Join(dir, "proposal.json")
	probeRestorePath = filepath.Join(dir, "restore.json")
	calibPausePath = filepath.Join(dir, "pause")
	calibLockPath = filepath.Join(dir, "lock")
	watchdogStatePath = filepath.Join(dir, "wdt.json")
	iteWdtMarkerPath = filepath.Join(dir, "wdt.armed")
	restoreBackoff = time.Millisecond
	return dir
}

type rigWrite struct {
	pwm string
	pct int
}

// fakeRig is an IT8613E with five tachs and four outputs: drives says which tachs an output moves, a fan stands
// below stallBelow percent.
type fakeRig struct {
	mu          sync.Mutex
	pct         map[string]int
	drives      map[string][]string
	maxRPM      int
	stallBelow  map[string]int
	writes      []rigWrite
	reads       int
	onRead      func(n int)
	failRestore int
	restores    int
}

func newFakeRig(initial int, drives map[string][]string) *fakeRig {
	r := &fakeRig{pct: map[string]int{}, drives: drives, maxRPM: 2000, stallBelow: map[string]int{}}
	for _, p := range r.PWMs() {
		r.pct[p] = initial
	}
	return r
}

func (r *fakeRig) PWMs() []string  { return []string{"pwm1", "pwm2", "pwm3", "pwm4"} }
func (r *fakeRig) Tachs() []string { return []string{"fan1", "fan2", "fan3", "fan4", "fan5"} }

func (r *fakeRig) SetPct(pwm string, pct int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, rigWrite{pwm, pct})
	r.pct[pwm] = pct
	return nil
}

func (r *fakeRig) RPM(tach string) (int, error) {
	r.mu.Lock()
	r.reads++
	n, hook := r.reads, r.onRead
	rpm := 0
	for p, ts := range r.drives {
		if slices.Contains(ts, tach) && r.pct[p] >= r.stallBelow[tach] {
			rpm = max(rpm, r.pct[p]*r.maxRPM/100)
		}
	}
	r.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	return rpm, nil
}

func (r *fakeRig) Snapshot() (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Snapshot{}
	for p, v := range r.pct {
		s[p] = [2]byte{0, byte(v)}
	}
	return s, nil
}

func (r *fakeRig) Restore(s Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.restores++
	if r.restores <= r.failRestore {
		return fmt.Errorf("busy")
	}
	for p, v := range s {
		r.pct[p] = int(v[1])
	}
	return nil
}

func (r *fakeRig) minWrite() int {
	m := 100
	for _, w := range r.writes {
		m = min(m, w.pct)
	}
	return m
}

func passLock(fn func() error) error { return fn() }

func newSession(rig probeRig, unsafe func() string) *probeSession {
	if unsafe == nil {
		unsafe = func() string { return "" }
	}
	return &probeSession{
		rig: rig, chip: "IT8613", model: "DXP4800 Plus", unsafe: unsafe, lock: passLock,
		logf: func(string, ...any) {}, stop: make(chan struct{}),
		settle: 3 * time.Millisecond, window: 16 * time.Millisecond, poll: 2 * time.Millisecond, pauseBeat: time.Millisecond,
	}
}

func outputOf(p *ProbeProposal, pwm string) ProbeOutput {
	for _, o := range p.Outputs {
		if o.PWM == pwm {
			return o
		}
	}
	return ProbeOutput{}
}

func assertSafeEnd(t *testing.T, rig *fakeRig, initial int) {
	t.Helper()
	for p, v := range rig.pct {
		if v != initial {
			t.Errorf("%s left at %d %%, was %d %%", p, v, initial)
		}
	}
	for _, path := range []string{calibPausePath, probeRestorePath} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s left behind", path)
		}
	}
}

func TestProbeFindsSwappedChannels(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	// Output 2 moves tach 3 and output 3 tach 2 (swapped), output 1 has no fan, output 4 drives tach 1.
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan3"}, "pwm3": {"fan2"}, "pwm4": {"fan1"}})
	s := newSession(rig, nil)

	reason, err := s.execute()
	if reason != "" || err != nil {
		t.Fatalf("reason %q err %v", reason, err)
	}
	prop := readProposal("DXP4800 Plus")
	if prop == nil {
		t.Fatal("no proposal saved")
	}
	for pwm, want := range map[string][2]string{"pwm1": {"no_fan", ""}, "pwm2": {"mapped", "fan3"}, "pwm3": {"mapped", "fan2"}, "pwm4": {"mapped", "fan1"}} {
		if o := outputOf(prop, pwm); o.Status != want[0] || o.Tach != want[1] {
			t.Errorf("%s: %+v, want %v", pwm, o, want)
		}
	}
	if o := outputOf(prop, "pwm2"); o.PeakRPM <= o.BaseRPM {
		t.Errorf("rpm not recorded: %+v", o)
	}
	// The reports for the model say pwm2 and pwm3; the finding is the same pair plus pwm4, so it disagrees.
	if prop.Agrees == nil || *prop.Agrees || prop.Expected != "outputs pwm2 and pwm3" {
		t.Errorf("hint: %q %v", prop.Expected, prop.Agrees)
	}
	assertSafeEnd(t, rig, 30)
	if p := readProgress(probeStatePath); p == nil || p.State != "done" {
		t.Errorf("progress = %+v", p)
	}
}

func TestProbeAgreesWithReports(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}, "pwm3": {"fan3"}})
	if reason, err := newSession(rig, nil).execute(); reason != "" || err != nil {
		t.Fatal(reason, err)
	}
	if p := readProposal("DXP4800 Plus"); p == nil || p.Agrees == nil || !*p.Agrees {
		t.Fatalf("proposal = %+v", p)
	}
}

func TestProbeAmbiguous(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	// Output 1 moves two tachs; outputs 2 and 3 both move tach 3.
	rig := newFakeRig(30, map[string][]string{"pwm1": {"fan1", "fan2"}, "pwm2": {"fan3"}, "pwm3": {"fan3"}, "pwm4": {"fan4"}})
	if reason, err := newSession(rig, nil).execute(); reason != "" || err != nil {
		t.Fatal(reason, err)
	}
	prop := readProposal("DXP4800 Plus")
	for pwm, want := range map[string]string{"pwm1": "ambiguous", "pwm2": "ambiguous", "pwm3": "ambiguous", "pwm4": "mapped"} {
		if o := outputOf(prop, pwm); o.Status != want || (want == "ambiguous" && o.Tach != "") {
			t.Errorf("%s: %+v, want %s", pwm, o, want)
		}
	}
}

func TestProbeNeverWritesBelowFloor(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}})
	s := newSession(rig, nil)
	p := &prober{rig: rig, chip: "IT8613", model: s.model, floor: probeFloorPct, hold: 5, test: 10,
		settle: s.settle, window: s.window, poll: s.poll, lock: passLock, unsafe: s.unsafe, stop: s.stop,
		logf: s.logf, progress: func(ProbeProgress) {}}
	if _, reason := p.run(); reason != "" {
		t.Fatal(reason)
	}
	if m := rig.minWrite(); m < probeFloorPct {
		t.Fatalf("lowest write %d %%, floor %d %%", m, probeFloorPct)
	}
}

func TestProbeFanStallAbortsAndRestores(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	// The fan on tach 2 turns at the 70 % it had, but stands at the 50 % hold: the probe must stop and put 70 % back.
	rig := newFakeRig(70, map[string][]string{"pwm3": {"fan2"}})
	rig.stallBelow["fan2"] = 55
	reason, err := newSession(rig, nil).execute()
	if reason != "fan_stalled" || err == nil {
		t.Fatalf("reason %q err %v", reason, err)
	}
	if readProposal("DXP4800 Plus") != nil {
		t.Error("a proposal exists after an abort")
	}
	assertSafeEnd(t, rig, 70)
	if p := readProgress(probeStatePath); p == nil || p.State != "aborted" || p.Reason != "fan_stalled" {
		t.Errorf("progress = %+v", p)
	}
}

func TestProbeTemperatureSpikeAborts(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}, "pwm3": {"fan3"}})
	calls := 0
	reason, err := newSession(rig, func() string {
		if calls++; calls > 12 {
			return "cpu_hot"
		}
		return ""
	}).execute()
	if reason != "cpu_hot" || err == nil {
		t.Fatalf("reason %q err %v", reason, err)
	}
	if readProposal("DXP4800 Plus") != nil {
		t.Error("a proposal exists after an abort")
	}
	assertSafeEnd(t, rig, 30)
}

func TestProbeSignalRestores(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}, "pwm3": {"fan3"}})
	s := newSession(rig, nil)
	rig.onRead = func(n int) {
		if n == 14 {
			close(s.stop)
		}
	}
	reason, err := s.execute()
	if reason != "stopped" || err == nil {
		t.Fatalf("reason %q err %v", reason, err)
	}
	assertSafeEnd(t, rig, 30)
}

func TestProbePanicRestores(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}})
	rig.onRead = func(n int) {
		if n == 9 {
			panic("boom")
		}
	}
	reason, err := newSession(rig, nil).execute()
	if reason != "hardware" || err == nil {
		t.Fatalf("reason %q err %v", reason, err)
	}
	assertSafeEnd(t, rig, 30)
}

func TestProbeRestoreIsRetried(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}})
	rig.failRestore = 2
	if reason, err := newSession(rig, nil).execute(); reason != "" || err != nil {
		t.Fatal(reason, err)
	}
	assertSafeEnd(t, rig, 30)
}

func TestProbeRefusesBesideCalibration(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	lock, err := lockCalibration()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}})
	if reason, err := newSession(rig, nil).execute(); reason != "busy" || err == nil {
		t.Fatalf("reason %q err %v", reason, err)
	}
	if len(rig.writes) != 0 {
		t.Fatalf("wrote %v while a calibration runs", rig.writes)
	}
}

func TestProbePausesTheDaemon(t *testing.T) {
	isolate(t, "DXP4800 Plus")
	rig := newFakeRig(30, map[string][]string{"pwm2": {"fan2"}})
	paused := false
	rig.onRead = func(int) { paused = paused || calibPaused() }
	if reason, err := newSession(rig, nil).execute(); reason != "" || err != nil {
		t.Fatal(reason, err)
	}
	if !paused {
		t.Fatal("the daemon was not paused while the probe wrote")
	}
}

func TestVendorHintsCompare(t *testing.T) {
	mapped := func(pairs ...string) []ProbeOutput {
		var o []ProbeOutput
		for i := 0; i+1 < len(pairs); i += 2 {
			o = append(o, ProbeOutput{PWM: pairs[i], Tach: pairs[i+1], Status: "mapped"})
		}
		return o
	}
	cases := []struct {
		model string
		outs  []ProbeOutput
		text  string
		agree bool
	}{
		{"DXP4800", mapped("pwm3", "fan3"), "outputs pwm3", true},
		{"DXP4800", mapped("pwm2", "fan2", "pwm3", "fan3"), "outputs pwm3", false},
		{"DXP8800 Plus", mapped("pwm2", "fan2", "pwm3", "fan3", "pwm4", "fan4"), "outputs pwm2 and pwm3 and pwm4", true},
		{"DXP8800 Plus", mapped("pwm2", "fan2", "pwm3", "fan3"), "outputs pwm2 and pwm3 and pwm4", false},
		{"DXP2800", mapped("pwm3", "fan2"), "outputs pwm3", false},
		{"DXP6800 Plus", mapped("pwm2", "fan2", "pwm3", "fan3", "pwm4", "fan4"), "outputs pwm2 and pwm3 and pwm4", true},
		{"DXP8800 Pro", mapped("pwm2", "fan2", "pwm3", "fan3"), "outputs pwm2 and pwm3 and pwm4", false},
	}
	for _, c := range cases {
		text, ag := compareHint(c.model, c.outs)
		if text != c.text || ag == nil || *ag != c.agree {
			t.Errorf("%s %v: %q %v, want %q %v", c.model, c.outs, text, ag, c.text, c.agree)
		}
	}
	if _, ag := compareHint("DXP6800 Pro", nil); ag != nil {
		t.Error("no hint expected for a fixed model")
	}
	if FanKind("DXP480T Plus") != "" {
		t.Error("DXP480T Plus must stay without fan control")
	}
}
