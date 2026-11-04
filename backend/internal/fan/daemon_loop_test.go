package fan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

type loopChip struct {
	mu     sync.Mutex
	writes map[string][]byte
}

func (c *loopChip) Name() string             { return "fake" }
func (c *loopChip) Describe() string         { return "fake" }
func (c *loopChip) Details() map[string]any  { return nil }
func (c *loopChip) MaxDuty() byte            { return 255 }
func (c *loopChip) AutoDutyKnown() bool      { return true }
func (c *loopChip) RPM(Channel) (int, error) { return 1000, nil }
func (c *loopChip) Temps() []*int            { return nil }
func (c *loopChip) Outputs() []Channel {
	return []Channel{{ID: "fan2"}, {ID: "fan3"}}
}
func (c *loopChip) PWM(Channel) (byte, bool, error) { return 0, false, nil }
func (c *loopChip) SetPWM(ch Channel, d byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes[ch.ID] = append(c.writes[ch.ID], d)
	return nil
}
func (c *loopChip) Snapshot() (Snapshot, error) { return Snapshot{"fan2": {1, 2}}, nil }
func (c *loopChip) Restore(Snapshot) error      { return nil }
func (c *loopChip) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.writes["fan2"]) + len(c.writes["fan3"])
}

// runLoop runs the daemon for a few ticks with the given config, clock and rivals, then stops it and returns the last
// state it wrote.
func runLoop(t *testing.T, cfg string, clock time.Time, rivals []conflict.Conflict, pause bool, s Sensors) (State, *loopChip) {
	t.Helper()
	dir := t.TempDir()
	set := func(p *string, name string) {
		old := *p
		*p = filepath.Join(dir, name)
		t.Cleanup(func() { *p = old })
	}
	set(&configPath, "fan.cfg")
	set(&statePath, "state.json")
	defer func(old string) { historyPath = old }(historyPath)
	historyPath = ""
	set(&biosPath, "bios")
	set(&lockPath, "lock")
	set(&calibPath, "fan_rpm.json")
	set(&calibPausePath, "pause")
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if pause {
		if err := os.WriteFile(calibPausePath, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldTick, oldRead, oldNow, oldDet := tick, readSensors, now, detectConflicts
	tick = 10 * time.Millisecond
	readSensors = func() Sensors { return s }
	now = func() time.Time { return clock }
	detectConflicts = func() []conflict.Conflict { return rivals }
	t.Cleanup(func() { tick, readSensors, now, detectConflicts = oldTick, oldRead, oldNow, oldDet })

	chip := &loopChip{writes: map[string][]byte{}}
	done := make(chan error, 1)
	go func() { done <- runDaemon(chip) }()

	var st State
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(statePath); err == nil && json.Unmarshal(b, &st) == nil && st.Time > 0 {
			time.Sleep(60 * time.Millisecond) // a few more ticks
			b, _ = os.ReadFile(statePath)
			_ = json.Unmarshal(b, &st)
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return st, chip
}

const loopQuiet = v2Base + "quiet_enabled=\"1\"\nquiet_from=\"22:00\"\nquiet_to=\"06:00\"\nquiet_cap=\"30\"\n"

func TestLoopQuietHoursAndState(t *testing.T) {
	st, chip := runLoop(t, loopQuiet, mon3am, nil, false, sens(45, 40))
	if !st.QuietActive || st.Cap == nil || *st.Cap != 30 || st.Targets["fan3"] != 30 || st.Targets["fan2"] != 25 {
		t.Fatalf("state %+v", st)
	}
	if got := chip.writes["fan3"][0]; got != percentToDuty(30, 255) {
		t.Fatalf("written %d", got)
	}
}

func TestLoopEmergencyInQuietHoursIs100(t *testing.T) {
	st, _ := runLoop(t, loopQuiet, mon3am, nil, false, sens(95, 40))
	if st.Failsafe != "cpu_hot" || st.QuietActive || st.Targets["fan2"] != 100 || st.Targets["fan3"] != 100 {
		t.Fatalf("state %+v", st)
	}
}

func TestLoopCalibrationPausesTheNewLogic(t *testing.T) {
	st, chip := runLoop(t, loopQuiet, mon3am, nil, true, sens(45, 40))
	if n := chip.count(); n != 0 || st.QuietActive || st.Cap != nil || len(st.Targets) != 0 {
		t.Fatalf("writes %d, state %+v", n, st)
	}
}

func TestLoopRivalStillYields(t *testing.T) {
	st, chip := runLoop(t, loopQuiet, mon3am, rival(), false, sens(45, 40))
	if n := chip.count(); n != 0 || len(st.Conflict) != 1 || st.QuietActive {
		t.Fatalf("writes %d, state %+v", n, st)
	}
}
