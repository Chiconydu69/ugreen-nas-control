package ledctl

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

// Settings that do not fit never reach the chip tool.
func TestInvalidSettingsAreRejectedBeforeTheTool(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.config(strings.Join([]string{
		`power_color="--force 1 2"`, `disk1_color="1 2 3 4"`, `disk2_color="1;rm 2 3"`, `disk3_color="256 0 0"`,
		`disk1_effect="blink"`, `disk1_on="--bus"`, `disk1_off="99999999"`,
		`disk2_effect="breath"`, `disk2_on="100"`, `disk2_off="-5"`,
	}, "\n"))
	r.ctl("apply")
	for _, call := range r.chip.callList() {
		for _, bad := range []string{"--force", "--bus", "rm", "99999999", "256"} {
			if strings.Contains(call, bad) {
				t.Errorf("tool call %q carries the rejected value %q", call, bad)
			}
		}
	}
	// A rejected effect timing falls back to the default, and every rejection is logged.
	if !hasCall(r.chip.callList(), "set disk1 blink 450 450") {
		t.Errorf("calls: %v", r.chip.callList())
	}
	if !r.run.called("logger") {
		t.Error("no log line for the rejected settings")
	}
}

func hasCall(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestRejectedSettingIsLoggedOnce(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.config(`sync_color="nope"`)
	for range 3 {
		if got := r.c.colorCfg("sync_color", defaultSyncColor); got != defaultSyncColor {
			t.Fatalf("got %q", got)
		}
	}
	n := 0
	for _, c := range r.run.calls {
		if strings.HasPrefix(c, "logger") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d log lines, want 1", n)
	}
}

func TestEffectSettings(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.config("fx_name=\"rainbow\"\nfx_speed=\"9\"\nfx_leds=\"power,--bus,disk1,disk1,nope\"\nfx_reverse=\"1\"")
	r.ctl("fx", "start")
	if len(r.run.spawned) != 1 {
		t.Fatalf("spawned %v", r.run.spawned)
	}
	got := strings.Join(r.run.spawned[0].Args, " ")
	if got != "--speed 2 --reverse effect rainbow power,disk1" {
		t.Errorf("effect arguments: %q", got)
	}

	r = newRig(t, "DXP6800 Pro")
	r.config("fx_name=\"--evil\"")
	if rc := r.ctl("fx", "start"); rc != 0 || len(r.run.spawned) != 0 {
		t.Errorf("unknown effect: rc %d, spawned %v", rc, r.run.spawned)
	}
}

func TestColorSetting(t *testing.T) {
	for v, ok := range map[string]bool{"0 0 0": true, "255 255 255": true, "1 2 3": true, "256 0 0": false, "1 2": false, "1  2 3": false,
		"1 2 3 4": false, "-1 2 3": false, "a b c": false, "": false, "1 2 3\n": false} {
		if validColor(v) != ok {
			t.Errorf("validColor(%q) = %v", v, !ok)
		}
	}
}

// A pidfile whose pid was reused by another process must not make the controller signal that process.
func TestStalePidfileNeverKillsAForeignProcess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cmdline string
		killed  bool
	}{
		{"foreign process", "sleep\x00100\x00", false},
		{"our daemon", "/x/backend/ugreen-nas\x00led-ctl\x00daemon\x00run\x00", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "DXP6800 Pro")
			child := exec.Command("sleep", "100")
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _ = child.Wait(); close(done) }()
			t.Cleanup(func() { _ = child.Process.Kill() })

			proc := filepath.Join(r.dir, "proc")
			pid := strconv.Itoa(child.Process.Pid)
			if err := os.MkdirAll(filepath.Join(proc, pid), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(proc, pid, "cmdline"), []byte(tc.cmdline), 0o644); err != nil {
				t.Fatal(err)
			}
			r.c.Env.Proc = proc
			r.write("run/pid", pid+"\n")

			if running := r.c.daemonRunning(); running != tc.killed {
				t.Errorf("daemonRunning = %v", running)
			}
			r.ctl("daemon", "stop")
			select {
			case <-done:
				if !tc.killed {
					t.Error("the foreign process was signalled")
				}
			case <-time.After(1500 * time.Millisecond):
				if tc.killed {
					t.Error("our own daemon was not stopped")
				}
			}
		})
	}
}

// The status daemon yields to another tool that writes the same LEDs (the rival detector), at start.
func TestDaemonYieldsToALedRival(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	r.c.Detect = func() []conflict.Conflict {
		return []conflict.Conflict{
			{Kind: conflict.KindDaemon, ID: "ugreen-leds", Name: "UGREEN LEDs driver", Controls: []string{conflict.LED}, Severity: conflict.SeverityBlock},
			{Kind: conflict.KindDaemon, ID: "autofan", Name: "Dynamix Auto Fan Control", Controls: []string{conflict.Fan}, Severity: conflict.SeverityBlock},
		}
	}
	if rc := r.ctl("daemon", "start"); rc != 1 || !strings.Contains(r.err.String(), "UGREEN LEDs driver") || strings.Contains(r.err.String(), "Auto Fan") {
		t.Errorf("daemon start: rc %d, stderr %q", rc, r.err.String())
	}
	if len(r.run.spawned) != 0 {
		t.Error("the daemon was started")
	}

	r.write("run/pid", "1\n")
	if rc := r.ctl("daemon", "run"); rc != 0 {
		t.Errorf("daemon run: rc %d", rc)
	}
	for _, call := range r.chip.callList() {
		if strings.HasPrefix(call, "set ") {
			t.Errorf("the daemon wrote %q although a rival controls the LEDs", call)
		}
	}
	if _, err := os.Stat(filepath.Join(r.dir, "run/pid")); err == nil {
		t.Error("the daemon left its pidfile")
	}
}

// A daemon that ignores SIGTERM is waited for and then killed; the caller never goes on while it still runs.
func TestKillPidEscalatesToSIGKILL(t *testing.T) {
	r := newRig(t, "DXP6800 Pro")
	child := exec.Command("sh", "-c", `trap "" TERM; while :; do sleep 1; done`)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); close(done) }()
	t.Cleanup(func() { _ = child.Process.Kill() })
	time.Sleep(200 * time.Millisecond) // the trap is set

	proc := filepath.Join(r.dir, "proc")
	pid := strconv.Itoa(child.Process.Pid)
	if err := os.MkdirAll(filepath.Join(proc, pid), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, pid, "cmdline"), []byte("/x/backend/ugreen-nas\x00led-ctl\x00daemon\x00run\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.c.Env.Proc = proc
	r.write("run/pid", pid+"\n")
	r.c.Sleep = time.Sleep
	r.c.killPid(r.c.Env.PidFile, 2)
	select {
	case <-done:
	default:
		t.Fatal("killPid returned while the process still ran")
	}
}

func TestExecRunnerTimesOut(t *testing.T) {
	old := toolTimeout
	toolTimeout = 200 * time.Millisecond
	defer func() { toolTimeout = old }()
	start := time.Now()
	if _, code := (ExecRunner{}).Output("sleep", "30"); code == 0 || time.Since(start) > 5*time.Second {
		t.Fatalf("code %d after %v", code, time.Since(start))
	}
}
