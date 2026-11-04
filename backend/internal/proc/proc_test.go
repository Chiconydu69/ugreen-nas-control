package proc

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func fakeProc(t *testing.T, pid int, exe string, args ...string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := ""
	for _, a := range args {
		cmd += a + "\x00"
	}
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(cmd), 0o644); err != nil {
		t.Fatal(err)
	}
	if exe != "" {
		if err := os.Symlink(exe, filepath.Join(dir, "exe")); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestOurs(t *testing.T) {
	for _, c := range []struct {
		name string
		exe  string
		args []string
		want bool
	}{
		{"tool symlink", "/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas", []string{"/x/backend/ugreen-nas-fan", "daemon"}, true},
		{"multicall by exe", "/x/backend/ugreen-nas", []string{"whatever"}, true},
		{"replaced file", "/x/backend/ugreen-nas (deleted)", []string{"/x/backend/ugreen-nas", "led-ctl", "daemon", "run"}, true},
		{"multicall subcommand", "", []string{"/x/ugreen-nas", "led-ctl", "daemon", "run"}, true},
		{"tool name only", "", []string{"ugreen-led-ctl", "daemon", "run"}, true},
		{"unrelated process", "/usr/bin/sleep", []string{"sleep", "120"}, false},
		{"multicall without tool", "", []string{"/x/ugreen-nas"}, false},
		{"name only in a later argument", "", []string{"vim", "ugreen-led-ctl"}, false},
		{"similar name", "", []string{"/x/ugreen-nas-fan-helper"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := fakeProc(t, 4242, c.exe, c.args...)
			if got := Ours(root, 4242); got != c.want {
				t.Errorf("Ours = %v, want %v", got, c.want)
			}
		})
	}
}

func TestOursRejectsMissingAndInvalidPids(t *testing.T) {
	root := fakeProc(t, 10, "", "ugreen-led-ctl")
	for _, pid := range []int{0, -1, 11} {
		if Ours(root, pid) {
			t.Errorf("pid %d accepted", pid)
		}
	}
}
