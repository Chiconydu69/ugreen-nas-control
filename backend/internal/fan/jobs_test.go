package fan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Fixtures follow the Unraid layout as documented; verify on a real box (see jobs.go).
const (
	varIdle    = "mdState=\"STARTED\"\nmdResync=\"0\"\nmdResyncPos=\"0\"\nmdResyncAction=\"check P\"\n"
	varRunning = "mdState=\"STARTED\"\nmdResync=\"7814026532\"\nmdResyncPos=\"1234\"\nmdResyncAction=\"check P\"\n"
	mdIdle     = "Personalities :\nmdState=STARTED\nmdResync=0\nmdResyncDt=0\n"
	mdRunning  = "mdState=STARTED\nmdResync=7814026532\nmdResyncPos=1234\n"
	mdClassic  = "md0 : active raid1 sdb[1] sda[0]\n      976630464 blocks [2/2] [UU]\n      [==>..................]  resync = 12.3% (120000000/976630464) finish=60.0min speed=100000K/sec\n"
	mdPending  = "md0 : active raid1 sdb[1] sda[0]\n      976630464 blocks [2/2] [UU]\n        resync=PENDING\n"
)

func probeIn(t *testing.T, files map[string]string) *jobProbe {
	t.Helper()
	dir := t.TempDir()
	write := func(name string) string {
		p := filepath.Join(dir, name)
		if s, ok := files[name]; ok {
			if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	return &jobProbe{
		mdstat: write("mdstat"), varIni: write("var.ini"), moverPID: write("mover.pid"), procDir: filepath.Join(dir, "proc"),
		alive: func(int) bool { return true },
		pgrep: func() bool { return false },
	}
}

func TestParityDetection(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  bool
	}{
		"nothing":         {nil, false},
		"idle":            {map[string]string{"var.ini": varIdle, "mdstat": mdIdle}, false},
		"var.ini running": {map[string]string{"var.ini": varRunning, "mdstat": mdIdle}, true},
		"mdstat running":  {map[string]string{"var.ini": varIdle, "mdstat": mdRunning}, true},
		"mdstat only":     {map[string]string{"mdstat": mdRunning}, true},
		"classic resync":  {map[string]string{"mdstat": mdClassic}, true},
		"classic pending": {map[string]string{"mdstat": mdPending}, false},
		"garbage":         {map[string]string{"var.ini": "mdResync=lots\n", "mdstat": "\x00\x01"}, false},
	} {
		p := probeIn(t, tc.files)
		if got := parityRunning(p.mdstat, p.varIni); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestMoverDetection(t *testing.T) {
	p := probeIn(t, map[string]string{"mover.pid": "4242\n"})
	if err := os.MkdirAll(filepath.Join(p.procDir, "4242"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.procDir, "4242", "cmdline"), []byte("/bin/bash\x00/usr/local/sbin/mover\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !p.moverRunning() {
		t.Error("live mover not seen")
	}
	// the pid was reused by something else
	if err := os.WriteFile(filepath.Join(p.procDir, "4242", "cmdline"), []byte("sshd\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p.moverRunning() {
		t.Error("reused pid counted as the mover")
	}
	// stale: nobody there
	p.alive = func(int) bool { return false }
	if p.moverRunning() {
		t.Error("stale pid file counted")
	}
	q := probeIn(t, map[string]string{"mover.pid": "not a pid"})
	if q.moverRunning() {
		t.Error("garbage pid file counted")
	}
	r := probeIn(t, nil)
	if r.moverRunning() {
		t.Error("no file, no process")
	}
	r.pgrep = func() bool { return true }
	if !r.moverRunning() {
		t.Error("pgrep fallback not used")
	}
	// pid 1 is never a mover
	one := probeIn(t, map[string]string{"mover.pid": "1"})
	if one.moverRunning() {
		t.Error("pid 1")
	}
}

func TestPidAlive(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Error("own pid not alive")
	}
	if pidAlive(0x7ffffff0) {
		t.Error("impossible pid alive")
	}
}

func TestJobProbeCachesTenSeconds(t *testing.T) {
	p := probeIn(t, map[string]string{"var.ini": varRunning})
	t0 := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if s := p.Status(t0, true, true); !s.Parity || s.Mover {
		t.Fatalf("%+v", s)
	}
	if err := os.WriteFile(p.varIni, []byte(varIdle), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := p.Status(t0.Add(9*time.Second), true, true); !s.Parity {
		t.Error("re-read within the TTL")
	}
	if s := p.Status(t0.Add(10*time.Second), true, true); s.Parity {
		t.Error("not re-read after the TTL")
	}
	// jobs nobody asked about are not looked at
	p.pgrep = func() bool { t.Error("pgrep although mover is off"); return true }
	_ = p.Status(t0.Add(time.Hour), false, true)
}

func TestPgrepMoverTimesOut(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pgrep"), []byte("#!/bin/sh\nexec /bin/sleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	old := pgrepTimeout
	pgrepTimeout = 100 * time.Millisecond
	defer func() { pgrepTimeout = old }()
	start := time.Now()
	if pgrepMover() {
		t.Error("a pgrep that never answers must not report a mover")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("pgrep not stopped: %v", d)
	}
}
