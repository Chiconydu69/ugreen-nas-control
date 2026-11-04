package fan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

func rival() []conflict.Conflict {
	return []conflict.Conflict{{Kind: conflict.KindDaemon, ID: "fanctrlplus2_loop.sh", Name: "FanCtrl Plus 2", Controls: []string{conflict.Fan}, Severity: conflict.SeverityBlock}}
}

func TestYieldReportsAndAlertsOnce(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "notify.log")
	script := filepath.Join(dir, "notify")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$@\" >> "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := notifyCmd
	notifyCmd = script
	t.Cleanup(func() { notifyCmd = old })

	var logs []string
	logf := func(f string, _ ...any) { logs = append(logs, strings.TrimSpace(f)) }
	c := LoadConfig(filepath.Join(dir, "none.cfg"))
	hot := 99
	var y yielding

	for range 3 {
		st := State{Targets: map[string]int{}}
		yield(&y, c, Sensors{CPU: &hot}, rival(), &st, logf)
		if st.Unapplied != "cpu_hot" || len(st.Conflict) != 1 || st.Conflict[0] != "FanCtrl Plus 2" || len(st.Targets) != 0 {
			t.Fatalf("state %+v", st)
		}
	}
	notifications.wg.Wait()
	b, _ := os.ReadFile(log)
	if n := strings.Count(string(b), "\n"); n != 1 {
		t.Fatalf("alert sent %d times: %q", n, b)
	}
	if len(logs) != 2 { // the rival once, the emergency once
		t.Fatalf("logs %v", logs)
	}

	cool := 40
	st := State{}
	yield(&y, c, Sensors{CPU: &cool}, rival(), &st, logf)
	if st.Unapplied != "" {
		t.Fatalf("emergency over but %q", st.Unapplied)
	}
	y.reset(logf)
	if y.names != "" || y.unapplied != "" {
		t.Fatal("episode not ended")
	}
}

func TestConflictListNeverNil(t *testing.T) {
	old := detectConflicts
	detectConflicts = func() []conflict.Conflict { return nil }
	t.Cleanup(func() { detectConflicts = old })
	if l := conflictList(); l == nil || len(l) != 0 {
		t.Fatalf("got %#v", l)
	}
}

// A hung notify script is killed after notifyTimeout and repeats while it runs are dropped, not piled up.
func TestNotifyBoundedAndNoPileUp(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "starts.log")
	script := filepath.Join(dir, "notify")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho start >> "+log+"\nexec /bin/sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldCmd, oldTimeout := notifyCmd, notifyTimeout
	notifyCmd, notifyTimeout = script, 2*time.Second
	t.Cleanup(func() { notifyCmd, notifyTimeout = oldCmd, oldTimeout })

	start := time.Now()
	for range 20 {
		notify("fan1") // returns at once, never waits for the script
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("notify blocked for %v", d)
	}
	// The script must have started before the timeout is meaningful: poll for its marker, not a fixed sleep.
	for deadline := time.Now().Add(10 * time.Second); ; {
		if b, _ := os.ReadFile(log); strings.Contains(string(b), "start") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("notify script never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	notifications.wg.Wait() // returns only because the timeout killed the script
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("script not stopped: %v", d)
	}
	b, _ := os.ReadFile(log)
	if n := strings.Count(string(b), "\n"); n != 1 {
		t.Errorf("script started %d times for 20 calls, want 1", n)
	}
	notify("fan1") // the slot is free again
	notifications.wg.Wait()
	b, _ = os.ReadFile(log)
	if n := strings.Count(string(b), "\n"); n != 2 {
		t.Errorf("script started %d times, want 2", n)
	}
}
