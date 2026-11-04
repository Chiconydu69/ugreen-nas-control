package hw

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

func TestLockPathResolution(t *testing.T) {
	t.Setenv(LockEnv, "")
	if got := LockPath(); got != DefaultLockPath {
		t.Errorf("default: %q", got)
	}
	// The env override is honoured in testhooks builds only; release builds keep the default.
	envWant := DefaultLockPath
	if testenv.Enabled {
		envWant = "/tmp/x.lock"
	}
	t.Setenv(LockEnv, "/tmp/x.lock")
	if got := LockPath(); got != envWant {
		t.Errorf("env: %q", got)
	}
	restore := SetLockPath("/tmp/y.lock")
	if got := LockPath(); got != "/tmp/y.lock" {
		t.Errorf("override: %q", got)
	}
	restore()
	if got := LockPath(); got != envWant {
		t.Errorf("restored: %q", got)
	}
}

func TestWithLockAtRunsAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	for range 2 { // a second acquisition proves the first released
		ran := false
		if err := WithLockAt(path, func() error { ran = true; return nil }); err != nil || !ran {
			t.Fatalf("ran %v err %v", ran, err)
		}
	}
	if err := WithLockAt(filepath.Join(t.TempDir(), "no", "dir", "lock"), func() error { return nil }); err == nil {
		t.Error("unopenable lock file must fail")
	}
}

func TestWithLockAtTimesOutWhenHeld(t *testing.T) {
	old := LockTimeout
	LockTimeout = 80 * time.Millisecond
	defer func() { LockTimeout = old }()
	path := filepath.Join(t.TempDir(), "lock")
	err := WithLockAt(path, func() error {
		// A second open file description of the same file conflicts, like another process would.
		ran := false
		start := time.Now()
		err := WithLockAt(path, func() error { ran = true; return nil })
		if !errors.Is(err, ErrLockTimeout) || ran {
			t.Errorf("held lock: err %v ran %v", err, ran)
		}
		if d := time.Since(start); d < 60*time.Millisecond || d > 2*time.Second {
			t.Errorf("waited %v", d)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := WithLockAt(path, func() error { return nil }); err != nil {
		t.Errorf("free again: %v", err)
	}
}
