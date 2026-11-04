package fan

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Where the user's scripts live and the limits they run under; tests move all of these.
var (
	sensorsDir    = envOr("UGREEN_SENSORS_D", "/boot/config/plugins/ugreen-nas-control/sensors.d")
	scriptTimeout = 2 * time.Second
	// scriptMaxOut: more output than this is a failure, not a truncated reading.
	scriptMaxOut = 64
	// nvidiaSMIPaths: where the NVIDIA tool is looked for (fixed paths, no PATH search).
	nvidiaSMIPaths = nvidiaCandidates()
)

func nvidiaCandidates() []string {
	if p := envOr("UGREEN_NVIDIA_SMI", ""); p != "" {
		return []string{p}
	}
	return []string{"/usr/bin/nvidia-smi", "/usr/local/bin/nvidia-smi"}
}

// interpreters: the only programs a script may name in its shebang, with the absolute paths they are looked for at.
// Fixed lists, no PATH search.
var interpreters = map[string][]string{
	"bash": {"/bin/bash", "/usr/bin/bash"},
	"sh":   {"/bin/sh", "/usr/bin/sh"},
	"php":  {"/usr/bin/php", "/usr/local/bin/php"},
}

var envPaths = []string{"/usr/bin/env", "/bin/env"}

// parseShebang turns the first line of a script into the absolute interpreter that runs it. Only interpreters from
// the allowlist, directly or through env, and no shebang arguments.
func parseShebang(line string) (string, error) {
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "#!") {
		return "", fmt.Errorf("has no shebang (first line must be like #!/bin/bash)")
	}
	f := strings.Fields(line[2:])
	if len(f) == 0 {
		return "", fmt.Errorf("has an empty shebang")
	}
	prog, rest := f[0], f[1:]
	if slices.Contains(envPaths, prog) {
		if len(rest) != 1 {
			return "", fmt.Errorf("shebang %q must be env and one interpreter name", line)
		}
		for _, p := range interpreters[rest[0]] {
			if isFile(p) {
				return p, nil
			}
		}
		return "", fmt.Errorf("interpreter %q is not allowed or not installed (allowed: bash, sh, php)", rest[0])
	}
	if len(rest) > 0 {
		return "", fmt.Errorf("shebang arguments are not supported (%q)", line)
	}
	for _, paths := range interpreters {
		if slices.Contains(paths, prog) {
			if !isFile(prog) {
				return "", fmt.Errorf("interpreter %s is not installed", prog)
			}
			return prog, nil
		}
	}
	return "", fmt.Errorf("interpreter %q is not allowed (allowed: bash, sh, php)", prog)
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func firstLine(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 256)
	n, _ := io.ReadFull(f, buf)
	line, _, _ := strings.Cut(string(buf[:n]), "\n")
	return line, nil
}

// checkScript applies the rules for a user script before it is run: a valid name, a real folder, a regular file (no
// link) in it, a shebang naming an allowed interpreter. It returns the command: interpreter, then the script path.
//
// Trust model: /boot is vfat on Unraid, where files have no owner and no execute bit, so neither is checked and the
// script runs through its interpreter. Whoever can write the flash already controls the boot config.
func checkScript(name string) ([]string, error) {
	if !scriptRe.MatchString(name) {
		return nil, fmt.Errorf("%q is not a valid script name (a-z, 0-9, _ and -, up to 32)", name)
	}
	dir, err := os.Lstat(sensorsDir)
	if err != nil {
		return nil, fmt.Errorf("no script folder: %w", unwrapPath(err))
	}
	if !dir.IsDir() {
		return nil, fmt.Errorf("the script folder is not a directory")
	}
	path := filepath.Join(sensorsDir, name)
	st, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("script %s not found", name)
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("script %s is not a regular file (links are refused)", name)
	}
	line, err := firstLine(path)
	if err != nil {
		return nil, fmt.Errorf("script %s: %w", name, unwrapPath(err))
	}
	interp, err := parseShebang(line)
	if err != nil {
		return nil, fmt.Errorf("script %s: %w", name, err)
	}
	return []string{interp, path}, nil
}

func unwrapPath(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	return err
}

func ScriptNames() []string {
	entries, _ := os.ReadDir(sensorsDir)
	var names []string
	for _, e := range entries {
		if scriptRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

// runSandboxed starts path with args and an empty environment, no shell, no stdin, in its own process group, and gives
// back its stdout. It fails when the timeout passes, more than maxOut bytes come, or the exit status is not 0; the
// whole process group is killed in every failing case, so no child of the script survives. It runs in dir, or in / when
// dir is empty.
func runSandboxed(ctx context.Context, path string, args []string, dir string, timeout time.Duration, maxOut int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.Command(path, args...)
	cmd.Env = []string{} // empty, not nil: nil would hand over our environment
	cmd.Dir = "/"
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("cannot start: %w", unwrapExec(err))
	}
	pgid := cmd.Process.Pid
	killGroup := func() { _ = syscall.Kill(-pgid, syscall.SIGKILL) }

	type readResult struct {
		out []byte
		err error
	}
	read := make(chan readResult, 1)
	go func() {
		out, err := io.ReadAll(io.LimitReader(stdout, int64(maxOut)+1))
		read <- readResult{out, err}
	}()

	var out []byte
	select {
	case r := <-read:
		out = r.out
	case <-ctx.Done():
		killGroup()
		_ = cmd.Wait()
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	// Stdout is closed (or the cap was hit): whatever is left of the group goes, then the exit status counts.
	killGroup()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var werr error
	select {
	case werr = <-waited:
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out after %s", timeout)
	}
	if len(out) > maxOut {
		return nil, fmt.Errorf("output is longer than %d bytes", maxOut)
	}
	if werr != nil {
		// Killing the group after a clean exit is not an error: the leader had finished first.
		if ee, ok := werr.(*exec.ExitError); ok && ee.ExitCode() == -1 && len(out) > 0 {
			return out, nil
		}
		return nil, fmt.Errorf("failed: %s", strings.TrimPrefix(werr.Error(), "exec: "))
	}
	return out, nil
}

func unwrapExec(err error) error {
	if pe, ok := err.(*os.PathError); ok {
		return pe.Err
	}
	if ee, ok := err.(*exec.Error); ok {
		return ee.Err
	}
	return err
}

// readScript runs the script at most every slowTTL; the answer, or the reason it failed, is kept in between.
func (r *Registry) readScript(ctx context.Context, name string) reading {
	return r.slowRead("script:"+name, func() (float64, error) {
		cmd, err := checkScript(name)
		if err != nil {
			return 0, err
		}
		out, err := runSandboxed(ctx, cmd[0], cmd[1:], "", scriptTimeout, scriptMaxOut)
		if err != nil {
			return 0, err
		}
		return parseTemp(out)
	})
}

// readGPU: the hwmon sensor of the GPU driver when there is one, else nvidia-smi (the hottest GPU).
func (r *Registry) readGPU(ctx context.Context) reading {
	if s := r.sensorSnapshot(); s.GPU != nil {
		return reading{float64(*s.GPU), true}
	}
	return r.slowRead("gpu", func() (float64, error) { return readNvidia(ctx) })
}

func nvidiaPath() string {
	for _, p := range nvidiaSMIPaths {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// readNvidia asks nvidia-smi for temperature.gpu of every GPU and takes the hottest.
func readNvidia(ctx context.Context) (float64, error) {
	path := nvidiaPath()
	if path == "" {
		return 0, fmt.Errorf("nvidia-smi not found")
	}
	out, err := runSandboxed(ctx, path, []string{"--query-gpu=temperature.gpu", "--format=csv,noheader,nounits"}, "", scriptTimeout, 1024)
	if err != nil {
		return 0, fmt.Errorf("nvidia-smi %w", err)
	}
	best, found := 0.0, false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		v, err := parseTemp([]byte(line))
		if err != nil {
			return 0, fmt.Errorf("nvidia-smi: %w", err)
		}
		if !found || v > best {
			best, found = v, true
		}
	}
	if !found {
		return 0, fmt.Errorf("nvidia-smi: no output")
	}
	return best, nil
}

// storcliPaths: where Broadcom's storcli is looked for (fixed paths, no PATH search).
var storcliPaths = storcliCandidates()

// hbaTimeout: storcli talks to the controller firmware and takes longer than nvidia-smi.
var hbaTimeout = 5 * time.Second

// storcliWorkDir: storcli writes storcli.log into its working directory on every call; on Unraid that must be RAM
// under /var/run, not / (also RAM) or the flash. The nolog argument is not used: its first version is unverified.
var storcliWorkDir = envOr("UGREEN_STORCLI_DIR", "/var/run/ugreen-nas-control.storcli")

func storcliCandidates() []string {
	if p := envOr("UGREEN_STORCLI", ""); p != "" {
		return []string{p}
	}
	var out []string
	for _, dir := range []string{"/opt/MegaRAID/storcli", "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin"} {
		out = append(out, dir+"/storcli64", dir+"/storcli")
	}
	return out
}

// readHBA: the hottest SAS/RAID host bus adapter. The hwmon sensor of the mpt3sas driver when there is one, else
// storcli (LSI/Broadcom MegaRAID and the HBAs it manages). Both are slow reads: mpt3sas asks the firmware on every
// read.
func (r *Registry) readHBA(ctx context.Context) reading {
	return r.slowRead("hba", func() (float64, error) {
		if v, ok := hbaHwmon(); ok {
			return v, nil
		}
		return readStorcli(ctx)
	})
}

// hbaHwmon takes the hottest temp*_input (IOC, Board) of every mpt3sas hwmon device.
func hbaHwmon() (float64, bool) {
	dirs, _ := filepath.Glob(filepath.Join(hwmonDir, "hwmon*"))
	var temps []float64
	for _, dir := range dirs {
		if readString(filepath.Join(dir, "name")) != "mpt3sas" {
			continue
		}
		inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
		for _, in := range inputs {
			if t := milli(in); t != nil && float64(*t) >= scriptMin && float64(*t) <= scriptMax {
				temps = append(temps, float64(*t))
			}
		}
	}
	got := hottest(temps)
	return got.v, got.ok
}

func storcliPath() string {
	for _, p := range storcliPaths {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// readStorcli asks storcli for the temperature of every controller ("ROC temperature(Degree Celsius) 68") and takes
// the hottest. A controller without a number on its line sits out.
func readStorcli(ctx context.Context) (float64, error) {
	path := storcliPath()
	if path == "" {
		return 0, fmt.Errorf("storcli not found")
	}
	if err := os.MkdirAll(storcliWorkDir, 0o700); err != nil {
		return 0, fmt.Errorf("storcli: %w", unwrapPath(err))
	}
	out, err := runSandboxed(ctx, path, []string{"/call", "show", "temperature"}, storcliWorkDir, hbaTimeout, 16384)
	// The log grows on every call (every 10 s); keep RAM flat.
	_ = os.Remove(filepath.Join(storcliWorkDir, "storcli.log"))
	if err != nil {
		return 0, fmt.Errorf("storcli %w", err)
	}
	var temps []float64
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "ROC temperature") {
			continue
		}
		f := strings.Fields(line)
		if v, err := parseTemp([]byte(f[len(f)-1])); err == nil {
			temps = append(temps, v)
		}
	}
	got := hottest(temps)
	if !got.ok {
		return 0, fmt.Errorf("storcli: no ROC temperature in the output")
	}
	return got.v, nil
}
