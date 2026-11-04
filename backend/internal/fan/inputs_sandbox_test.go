package fan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// useScriptDir points the sandbox at a fresh folder.
func useScriptDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sensors.d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldDir, oldTimeout := sensorsDir, scriptTimeout
	sensorsDir, scriptTimeout = dir, 400*time.Millisecond
	t.Cleanup(func() { sensorsDir, scriptTimeout = oldDir, oldTimeout })
	return dir
}

func writeScript(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func readScriptOnce(t *testing.T, name string) (float64, bool, string) {
	t.Helper()
	r, _, _, _ := newTestRegistry(t, Sensors{})
	spec := InputSpec{Kind: "script", Name: name}
	v, ok := r.Read(context.Background(), spec)
	return v, ok, r.Detail(spec)
}

func TestDefaultSandboxLimits(t *testing.T) {
	if scriptTimeout != 2*time.Second || scriptMaxOut != 64 || slowTTL != 10*time.Second {
		t.Fatalf("limits changed: %v %d %v", scriptTimeout, scriptMaxOut, slowTTL)
	}
	if sensorsDir != "/boot/config/plugins/ugreen-nas-control/sensors.d" {
		t.Fatalf("script folder %q", sensorsDir)
	}
}

func TestScriptReadings(t *testing.T) {
	dir := useScriptDir(t)
	for name, c := range map[string]struct {
		body string
		want float64
	}{
		"plain": {"#!/bin/sh\necho 42\n", 42},
		"deci":  {"#!/bin/sh\necho 41.5\n", 41.5},
		"neg":   {"#!/bin/sh\necho -5\n", -5},
		"edge":  {"#!/bin/sh\necho 150\n", 150},
		"low":   {"#!/bin/sh\necho -20\n", -20},
		"bare":  {"#!/bin/sh\nprintf 37\n", 37},
	} {
		writeScript(t, dir, name, c.body, 0o755)
		if v, ok, detail := readScriptOnce(t, name); !ok || v != c.want {
			t.Errorf("%s = %v, %v (%s), want %v", name, v, ok, detail, c.want)
		}
	}
}

func TestScriptFailures(t *testing.T) {
	dir := useScriptDir(t)
	huge := "#!/bin/sh\nyes 1 | head -c 100000\n"
	for name, c := range map[string]struct {
		body string
		mode os.FileMode
		want string
	}{
		"text":    {"#!/bin/sh\necho hot\n", 0o755, "not a single number"},
		"two":     {"#!/bin/sh\necho 40 41\n", 0o755, "not a single number"},
		"empty":   {"#!/bin/sh\n", 0o755, "no output"},
		"toohot":  {"#!/bin/sh\necho 151\n", 0o755, "outside"},
		"toocold": {"#!/bin/sh\necho -21\n", 0o755, "outside"},
		"huge":    {huge, 0o755, "longer than 64"},
		"exit1":   {"#!/bin/sh\necho 40\nexit 1\n", 0o755, "exit status 1"},
		"slow":    {"#!/bin/sh\nsleep 5\necho 40\n", 0o755, "timed out"},
		"nobang":  {"echo 40\n", 0o644, "no shebang"},
		"badint":  {"#!/usr/bin/python3\nprint(40)\n", 0o644, "not allowed"},
		"shargs":  {"#!/bin/bash -e\necho 40\n", 0o644, "arguments are not supported"},
	} {
		writeScript(t, dir, name, c.body, c.mode)
		start := time.Now()
		v, ok, detail := readScriptOnce(t, name)
		if ok || !strings.Contains(detail, c.want) {
			t.Errorf("%s: %v, %v, %q; want a failure containing %q", name, v, ok, detail, c.want)
		}
		if time.Since(start) > 3*time.Second {
			t.Errorf("%s took %v", name, time.Since(start))
		}
	}
}

func TestScriptNameAndLocationChecks(t *testing.T) {
	dir := useScriptDir(t)
	writeScript(t, dir, "real", "#!/bin/sh\necho 40\n", 0o755)
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if _, ok, detail := readScriptOnce(t, "link"); ok || !strings.Contains(detail, "not a regular file") {
		t.Errorf("a link must be refused: %v %q", ok, detail)
	}
	if _, ok, detail := readScriptOnce(t, "absent"); ok || !strings.Contains(detail, "not found") {
		t.Errorf("absent: %v %q", ok, detail)
	}
	for _, bad := range []string{"../real", "Real", "", "a b", strings.Repeat("a", 33)} {
		if _, err := checkScript(bad); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}

	sensorsDir = filepath.Join(t.TempDir(), "gone")
	if _, ok, detail := readScriptOnce(t, "real"); ok || !strings.Contains(detail, "no script folder") {
		t.Errorf("missing folder: %v %q", ok, detail)
	}
}

func TestScriptRunsWithAnEmptyEnvironmentAndNoArguments(t *testing.T) {
	dir := useScriptDir(t)
	t.Setenv("UNC_SECRET", "1")
	writeScript(t, dir, "env", "#!/bin/sh\nif [ -n \"$UNC_SECRET\" ] || [ -n \"$HOME\" ] || [ -n \"$PATH_NOT_SET\" ]; then echo 99; elif [ $# -ne 0 ]; then echo 98; else echo 42; fi\n", 0o755)
	if v, ok, detail := readScriptOnce(t, "env"); !ok || v != 42 {
		t.Fatalf("env/args leaked: %v %v %q", v, ok, detail)
	}
}

func TestTimeoutKillsTheWholeProcessGroup(t *testing.T) {
	dir := useScriptDir(t)
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	writeScript(t, dir, "forker", fmt.Sprintf("#!/bin/sh\nsleep 30 &\necho $! > %s\nwait\n", pidFile), 0o755)
	start := time.Now()
	if _, ok, detail := readScriptOnce(t, "forker"); ok || !strings.Contains(detail, "timed out") {
		t.Fatalf("%v %q", ok, detail)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("the child %d of a timed-out script is still running", pid)
}

func TestScriptRateLimitCachesSuccessAndFailure(t *testing.T) {
	dir := useScriptDir(t)
	count := filepath.Join(t.TempDir(), "count")
	good := fmt.Sprintf("#!/bin/sh\necho x >> %s\necho 40\n", count)
	bad := fmt.Sprintf("#!/bin/sh\necho x >> %s\necho nope\n", count)
	writeScript(t, dir, "flaky", bad, 0o755)

	r, clock, _, logs := newTestRegistry(t, Sensors{})
	ctx := context.Background()
	spec := InputSpec{Kind: "script", Name: "flaky"}
	read := func() bool { r.Tick(nil); _, ok := r.Read(ctx, spec); return ok }

	for range 4 { // 4 ticks, 2 s apart: one run, failure cached
		if read() {
			t.Fatal("bad output must be missing")
		}
		clock.advance(2 * time.Second)
	}
	if n := runs(t, count); n != 1 {
		t.Fatalf("a failing script ran %d times in 8 s, want 1", n)
	}
	clock.advance(10 * time.Second)
	read()
	clock.advance(10 * time.Second)
	read()
	if n := runs(t, count); n != 3 {
		t.Fatalf("runs = %d", n)
	}
	if len(*logs) != 1 {
		t.Fatalf("the same failure must be logged once, got %q", *logs)
	}

	writeScript(t, dir, "flaky", good, 0o755)
	clock.advance(10 * time.Second)
	if !read() {
		t.Fatal("the fixed script must read")
	}
	if len(*logs) != 2 || !strings.Contains((*logs)[1], "works again") {
		t.Fatalf("recovery log: %q", *logs)
	}
	// A different failure is a new message.
	writeScript(t, dir, "flaky", "#!/bin/sh\nexit 3\n", 0o755)
	clock.advance(10 * time.Second)
	read()
	if len(*logs) != 3 {
		t.Fatalf("logs: %q", *logs)
	}
}

func TestRunSandboxedStdoutCapKillsTheWriter(t *testing.T) {
	dir := useScriptDir(t)
	p := writeScript(t, dir, "flood", "#!/bin/sh\nwhile :; do echo 12345678901234567890; done\n", 0o755)
	start := time.Now()
	_, err := runSandboxed(context.Background(), p, nil, "", 5*time.Second, 64)
	if err == nil || !strings.Contains(err.Error(), "longer than 64") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("a flooding script was not stopped at once: %v", time.Since(start))
	}
}

func TestScriptWithoutExecBitRuns(t *testing.T) {
	dir := useScriptDir(t)
	for name, body := range map[string]string{
		"sh":    "#!/bin/sh\necho 42\n",
		"bash":  "#!/bin/bash\necho 42\n",
		"envsh": "#!/usr/bin/env sh\necho 42\n",
	} {
		writeScript(t, dir, name, body, 0o600)
		if v, ok, detail := readScriptOnce(t, name); !ok || v != 42 {
			t.Errorf("%s = %v, %v (%s)", name, v, ok, detail)
		}
	}
}

func TestParseShebang(t *testing.T) {
	for _, c := range []struct{ line, want, err string }{
		{"#!/bin/sh", "/bin/sh", ""},
		{"#!/bin/bash\r\n", "/bin/bash", ""},
		{"#!  /bin/sh", "/bin/sh", ""},
		{"#!/usr/bin/env sh", "/bin/sh", ""},
		{"#!/usr/bin/env bash", "", ""},
		{"", "", "no shebang"},
		{"echo hi", "", "no shebang"},
		{"# !/bin/sh", "", "no shebang"},
		{"#!", "", "empty shebang"},
		{"#!/usr/bin/python3", "", "not allowed"},
		{"#!/tmp/evil", "", "not allowed"},
		{"#!/bin/bash -e", "", "arguments"},
		{"#!/usr/bin/env", "", "one interpreter"},
		{"#!/usr/bin/env python3", "", "not allowed"},
		{"#!/usr/bin/env -S sh -e", "", "one interpreter"},
	} {
		got, err := parseShebang(c.line)
		switch {
		case c.err != "":
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%q: err %v, want %q", c.line, err, c.err)
			}
		case c.want != "":
			if err != nil || got != c.want {
				t.Errorf("%q = %q, %v; want %q", c.line, got, err, c.want)
			}
		}
	}
}

func TestShebangAllowlistIsAbsoluteAndFixed(t *testing.T) {
	for name, paths := range interpreters {
		for _, p := range paths {
			if !filepath.IsAbs(p) || filepath.Base(p) != name {
				t.Errorf("%s: %q", name, p)
			}
		}
	}
}
