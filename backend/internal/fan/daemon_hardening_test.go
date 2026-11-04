package fan

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// hardeningEnv points the daemon at a temp dir and a quick tick; the returned function starts it.
func hardeningEnv(t *testing.T, cfg string) (chip *loopChip, start func() chan error) {
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
	t.Cleanup(func() { historyPath = oldHist })
	if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTick, oldRead, oldDet, oldIn := tick, readSensors, detectConflicts, newInputSource
	tick = 10 * time.Millisecond
	readSensors = func() Sensors { return sens(45, 40) }
	detectConflicts = func() []conflict.Conflict { return nil }
	t.Cleanup(func() { tick, readSensors, detectConflicts, newInputSource = oldTick, oldRead, oldDet, oldIn })
	chip = &loopChip{writes: map[string][]byte{}}
	return chip, func() chan error {
		done := make(chan error, 1)
		go func() { done <- runDaemon(chip) }()
		return done
	}
}

// fakeSlowInput reads once per tick, like the Registry's per-tick cache: later asks of the same tick are answered from it.
type fakeSlowInput struct {
	read func(ctx context.Context) (float64, bool)
	once *sync.Once
	v    *float64
	ok   *bool
}

func newFakeSlowInput(read func(ctx context.Context) (float64, bool)) fakeSlowInput {
	return fakeSlowInput{read: read, once: &sync.Once{}, v: new(float64), ok: new(bool)}
}

func (f fakeSlowInput) Read(ctx context.Context, _ InputSpec) (float64, bool) {
	f.once.Do(func() { *f.v, *f.ok = f.read(ctx) })
	return *f.v, *f.ok
}

// A slow input (script, nvidia-smi, storcli) is read before the lock: with the read in progress the lock is free,
// and a read that takes longer than a tick never holds it.
func TestSlowInputRunsOutsideTheLock(t *testing.T) {
	chip, start := hardeningEnv(t, v2Base+"cpu_input=\"gpu\"\n")
	var reads, underLock atomic.Int32
	newInputSource = func(Config, Sensors) InputSource {
		return newFakeSlowInput(func(_ context.Context) (float64, bool) {
			reads.Add(1)
			old := hw.LockTimeout
			hw.LockTimeout = 20 * time.Millisecond
			err := hw.WithLockAt(lockPath, func() error { return nil })
			hw.LockTimeout = old
			if err != nil {
				underLock.Add(1)
			}
			time.Sleep(30 * time.Millisecond)
			return 50, true
		})
	}
	done := start()
	deadline := time.Now().Add(5 * time.Second)
	for chip.count() < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if reads.Load() == 0 || chip.count() < 4 {
		t.Fatalf("reads %d writes %d", reads.Load(), chip.count())
	}
	if n := underLock.Load(); n != 0 {
		t.Errorf("%d input reads ran while the daemon held the lock", n)
	}
}

// A hung input read does not hold the shutdown: the signal cancels it.
func TestShutdownDuringSlowInputIsBounded(t *testing.T) {
	_, start := hardeningEnv(t, v2Base+"cpu_input=\"gpu\"\n")
	started := make(chan struct{})
	var once sync.Once
	newInputSource = func(Config, Sensors) InputSource {
		return newFakeSlowInput(func(ctx context.Context) (float64, bool) {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return 0, false
		})
	}
	done := start()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("input never read")
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

// Another process holds the lock past the timeout: the tick writes nothing and the daemon tries again on the next
// one (it neither blocks forever nor dies).
func TestLockTimeoutIsAFailedTick(t *testing.T) {
	chip, start := hardeningEnv(t, v2Base)
	oldTimeout := hw.LockTimeout
	hw.LockTimeout = 40 * time.Millisecond
	t.Cleanup(func() { hw.LockTimeout = oldTimeout })

	var held atomic.Bool
	release := make(chan struct{})
	var once sync.Once
	detectConflicts = func() []conflict.Conflict {
		once.Do(func() { // the first tick: someone else takes the lock before the daemon does
			locked := make(chan struct{})
			go func() {
				_ = hw.WithLockAt(lockPath, func() error {
					held.Store(true)
					close(locked)
					<-release
					return nil
				})
				held.Store(false)
			}()
			<-locked
		})
		return nil
	}
	done := start()
	time.Sleep(300 * time.Millisecond) // many ticks while the lock is held
	if n := chip.count(); n != 0 {
		t.Fatalf("%d writes while another process held the lock", n)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for chip.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if chip.count() == 0 {
		t.Fatal("daemon never wrote after the lock came free")
	}
}

type restoreChip struct {
	loopChip
	restored atomic.Int32
}

func (c *restoreChip) Restore(Snapshot) error { c.restored.Add(1); return nil }

// A CLI holds the lock longer than a tick waits: the stop still hands the fans back (it waits StopLockTimeout).
func TestStopWaitsLongerThanATickForTheLock(t *testing.T) {
	dir := t.TempDir()
	oldLock, oldState := lockPath, statePath
	lockPath, statePath = filepath.Join(dir, "lock"), filepath.Join(dir, "state.json")
	oldTick, oldStop := hw.LockTimeout, hw.StopLockTimeout
	hw.LockTimeout, hw.StopLockTimeout = 30*time.Millisecond, 5*time.Second
	t.Cleanup(func() { lockPath, statePath, hw.LockTimeout, hw.StopLockTimeout = oldLock, oldState, oldTick, oldStop })

	locked := make(chan struct{})
	go func() {
		_ = hw.WithLockAt(lockPath, func() error { close(locked); time.Sleep(250 * time.Millisecond); return nil })
	}()
	<-locked
	chip := &restoreChip{loopChip: loopChip{writes: map[string][]byte{}}}
	endFanControl(chip, Snapshot{}, &Petter{}, nil, func(string, ...any) {})
	if chip.restored.Load() != 1 {
		t.Fatal("fans not handed back while the lock was busy for longer than a tick's wait")
	}
}
