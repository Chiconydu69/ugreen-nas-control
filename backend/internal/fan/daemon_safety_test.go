package fan

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

// safetyChip fails the writes of fan2 (or panics in them) and records the restore.
type safetyChip struct {
	loopChip
	failFan2, panicFan2 bool
	restored            chan struct{}
	once                sync.Once
}

func (c *safetyChip) SetPWM(ch Channel, d byte) error {
	if ch.ID == "fan2" {
		if c.panicFan2 {
			panic("boom")
		}
		if c.failFan2 {
			return errors.New("write failed")
		}
	}
	return c.loopChip.SetPWM(ch, d)
}

func (c *safetyChip) Restore(Snapshot) error {
	c.once.Do(func() { close(c.restored) })
	return nil
}

func newSafetyChip() *safetyChip {
	return &safetyChip{loopChip: loopChip{writes: map[string][]byte{}}, restored: make(chan struct{})}
}

// startSafetyDaemon runs the daemon on chip with a hot CPU (emergency) and returns its result channel.
func startSafetyDaemon(t *testing.T, chip Fans) chan error {
	t.Helper()
	dir := t.TempDir()
	set := func(p *string, name string) {
		old := *p
		*p = filepath.Join(dir, name)
		t.Cleanup(func() { *p = old })
	}
	set(&configPath, "fan.cfg")
	set(&statePath, "state.json")
	set(&biosPath, "bios")
	set(&lockPath, "lock")
	set(&calibPath, "fan_rpm.json")
	set(&calibPausePath, "pause")
	oldHist := historyPath
	historyPath = ""
	if err := os.WriteFile(configPath, []byte(v2Base), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTick, oldRead, oldDet, oldBackoff := tick, readSensors, detectConflicts, restoreBackoff
	tick, restoreBackoff = 10*time.Millisecond, time.Millisecond
	readSensors = func() Sensors { return sens(95, 40) }
	detectConflicts = func() []conflict.Conflict { return nil }
	t.Cleanup(func() {
		tick, readSensors, detectConflicts, restoreBackoff, historyPath = oldTick, oldRead, oldDet, oldBackoff, oldHist
	})
	done := make(chan error, 1)
	go func() { done <- runDaemon(chip) }()
	return done
}

func TestEmergencyReachesEveryOutputDespiteAFailingOne(t *testing.T) {
	chip := newSafetyChip()
	chip.failFan2 = true
	done := startSafetyDaemon(t, chip)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		chip.mu.Lock()
		n := len(chip.writes["fan3"])
		chip.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if w := chip.writes["fan3"]; len(w) == 0 || w[0] != 255 {
		t.Fatalf("fan3 writes %v: the healthy output did not get 100 %% while fan2 failed", w)
	}
}

func TestDaemonRestoresOnPanic(t *testing.T) {
	chip := newSafetyChip()
	chip.panicFan2 = true
	done := startSafetyDaemon(t, chip)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a crash returned no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not end after the panic")
	}
	select {
	case <-chip.restored:
	default:
		t.Fatal("fans not handed back after the panic")
	}
}

func TestDaemonRestoresOnHangupAndQuit(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGHUP, syscall.SIGQUIT} {
		chip := newSafetyChip()
		done := startSafetyDaemon(t, chip)
		deadline := time.Now().Add(5 * time.Second)
		for chip.count() == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		_ = syscall.Kill(os.Getpid(), sig)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		select {
		case <-chip.restored:
		default:
			t.Fatalf("%v: fans not handed back", sig)
		}
	}
}
