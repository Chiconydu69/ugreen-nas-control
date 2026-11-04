package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	for _, c := range []struct {
		args []string
		tool string
		name string
		rest []string
		ok   bool
	}{
		{[]string{"/x/backend/ugreen-nas-fan", "probe"}, "fan", "/x/backend/ugreen-nas-fan", []string{"probe"}, true},
		{[]string{"/x/led/backend/ugreen-led-i2c", "--plain", "get", "all"}, "led-i2c", "/x/led/backend/ugreen-led-i2c", []string{"--plain", "get", "all"}, true},
		{[]string{"ugreen-led-ctl", "daemon", "run"}, "led-ctl", "ugreen-led-ctl", []string{"daemon", "run"}, true},
		{[]string{"ugreen-nas-ctl"}, "nas-ctl", "ugreen-nas-ctl", []string{}, true},
		{[]string{"/x/ugreen-nas", "led-ctl", "status"}, "led-ctl", "/x/ugreen-nas led-ctl", []string{"status"}, true},
		{[]string{"/x/ugreen-nas", "fan", "info"}, "fan", "/x/ugreen-nas fan", []string{"info"}, true},
		{[]string{"/x/ugreen-nas", "panel", "buttons", "--watch"}, "panel", "/x/ugreen-nas panel", []string{"buttons", "--watch"}, true},
		{[]string{"/x/ugreen-nas", "ugreen-nas-ctl", "report"}, "nas-ctl", "/x/ugreen-nas ugreen-nas-ctl", []string{"report"}, true},
		{[]string{"/x/ugreen-nas"}, "", "", nil, false},
		{[]string{"/x/ugreen-nas", "bogus"}, "", "", nil, false},
		{[]string{"/x/other", "led-ctl"}, "led-ctl", "/x/other led-ctl", []string{}, true},
		{nil, "", "", nil, false},
	} {
		tool, name, rest, ok := resolve(c.args)
		if tool != c.tool || name != c.name || ok != c.ok || (ok && !slices.Equal(rest, c.rest)) {
			t.Errorf("resolve(%v) = %q %q %v %v", c.args, tool, name, rest, ok)
		}
	}
}

func TestUsageWithoutATool(t *testing.T) {
	if rc := run([]string{"ugreen-nas"}); rc != 2 {
		t.Errorf("rc %d", rc)
	}
}

func build(t *testing.T, dir, name string, tags ...string) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not in PATH")
	}
	out := filepath.Join(dir, name)
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	args := []string{"build", "-trimpath", "-o", out}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	cmd := exec.Command(goBin, append(args, ".")...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, msg)
	}
	return out
}

// The variables that only tests use are compiled out of the release build: a root process with a prepared
// environment (cron, php-fpm) cannot point the tools at another tree or binary.
func TestReleaseBuildIgnoresTestVariables(t *testing.T) {
	dir := t.TempDir()
	flag := filepath.Join(dir, "led.enabled")
	if err := os.WriteFile(flag, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "UGREEN_LED_FLAG="+flag, "UGREEN_DMI="+dir, "UGREEN_PLUGINS_BOOT="+dir, "UGREEN_PLUGINS_WEB="+dir, "UGREEN_PROC_MODULES="+dir+"/m", "UGREEN_PROC="+dir)
	statusOf := func(binary string) string {
		link := filepath.Join(filepath.Dir(binary), "ugreen-nas-ctl")
		_ = os.Symlink(binary, link)
		cmd := exec.Command(link, "led-status")
		cmd.Env = env
		out, _ := cmd.Output()
		return string(out)
	}
	if got := statusOf(build(t, dir, "hooks/ugreen-nas", "testhooks")); !strings.Contains(got, `"enabled": true`) {
		t.Errorf("the testhooks build ignores the test variables: %s", got)
	}
	if got := statusOf(build(t, dir, "release/ugreen-nas")); !strings.Contains(got, `"enabled": false`) {
		t.Errorf("the release build honours a test variable: %s", got)
	}
}
