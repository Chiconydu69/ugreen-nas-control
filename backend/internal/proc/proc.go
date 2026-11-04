// Package proc tells whether a pid from a pidfile still belongs to this plugin. A crashed daemon leaves its pidfile
// behind and the pid may be reused by an unrelated process; signalling it would kill that one.
package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Binary and tool names of the plugin: the multicall binary and the names it runs as.
const multicall = "ugreen-nas"

var toolNames = map[string]bool{
	"ugreen-nas-fan": true, "ugreen-led-i2c": true, "ugreen-led-ctl": true, "ugreen-nas-ctl": true,
}

var subcommands = map[string]bool{"fan": true, "led-i2c": true, "led-ctl": true, "nas-ctl": true}

// DefaultRoot is the proc filesystem.
const DefaultRoot = "/proc"

// Ours reports whether the process pid runs this plugin's binary: its executable is the multicall binary (a replaced
// file shows as "... (deleted)"), or its command line is a tool name, or `ugreen-nas <tool>`.
func Ours(root string, pid int) bool {
	if pid <= 0 {
		return false
	}
	if root == "" {
		root = DefaultRoot
	}
	dir := filepath.Join(root, strconv.Itoa(pid))
	if exe, err := os.Readlink(filepath.Join(dir, "exe")); err == nil {
		if filepath.Base(strings.TrimSuffix(exe, " (deleted)")) == multicall {
			return true
		}
	}
	args := cmdline(dir, pid)
	if len(args) == 0 {
		return false
	}
	base := filepath.Base(args[0])
	if toolNames[base] {
		return true
	}
	return base == multicall && len(args) > 1 && (subcommands[args[1]] || toolNames[args[1]])
}

func cmdline(dir string, pid int) []string {
	data, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		if runtime.GOOS == "linux" {
			return nil
		}
		// Development machines without /proc (macOS).
		out, err := exec.Command("ps", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return nil
		}
		return strings.Fields(string(out))
	}
	return strings.FieldsFunc(string(data), func(r rune) bool { return r == 0 })
}
