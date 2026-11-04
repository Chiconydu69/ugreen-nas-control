// Package ledctl is ugreen-led-ctl: the CLI
// the web UI calls and the status daemon behind it.
package ledctl

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/proc"
)

// Chip talks to the LED controller. The shipped implementation runs the
// ugreen-led-i2c tool; a fake serves the tests.
type Chip interface {
	// Exec runs one tool command; nil writers discard the output. It returns the exit code.
	Exec(args []string, stdout, stderr io.Writer) int
}

type SpawnSpec struct {
	Path   string
	Args   []string
	Stderr string // file that receives stderr (truncated), empty = discard
}

// Runner runs the external commands (ip, ping, ethtool, udevadm, pgrep, ...).
type Runner interface {
	// Output runs a command and returns its stdout and exit code; stderr is discarded.
	// The code is 127 when the command cannot be started.
	Output(name string, args ...string) ([]byte, int)
	// Has reports whether the command exists in PATH.
	Has(name string) bool
	// Spawn starts a detached process (own session) and returns its pid.
	Spawn(s SpawnSpec) (int, error)
}

// ExecChip runs the LED chip tool: the multicall binary with Prefix "led-i2c", or a stand-in executable.
type ExecChip struct {
	Path   string
	Prefix []string
}

// A hung chip tool (a stuck i2c transfer) or helper (ip, udevadm, pgrep) must not hang the controller or its daemon.
var (
	chipTimeout = 30 * time.Second
	toolTimeout = 20 * time.Second
)

func (e ExecChip) Exec(args []string, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), chipTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.Path, append(slices.Clone(e.Prefix), args...)...)
	cmd.WaitDelay = time.Second
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return exitCode(cmd.Run(), stderr, e.Path)
}

// stopWait is how long a stop waits for a daemon after SIGTERM, in tenths of a second: longer than one chip call
// (chipTimeout), so a daemon blocked in ExecChip while it restores the LEDs is not SIGKILLed mid-restore.
func stopWait() int { return int((chipTimeout + 5*time.Second) / (100 * time.Millisecond)) }

func exitCode(err error, stderr io.Writer, path string) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	if stderr != nil {
		_, _ = fmt.Fprintf(stderr, "%s: No such file or directory\n", path)
	}
	return 127
}

type ExecRunner struct{}

func (ExecRunner) Output(name string, args ...string) ([]byte, int) {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	return out, exitCode(err, nil, name)
}

func (ExecRunner) Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (ExecRunner) Spawn(s SpawnSpec) (int, error) {
	cmd := exec.Command(s.Path, s.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if s.Stderr != "" {
		if f, err := fsx.OpenTrunc(s.Stderr); err == nil {
			defer func() { _ = f.Close() }()
			cmd.Stderr = f
		}
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// Reap it when it ends, so a process that dies at once is not a zombie that still answers kill -0.
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

type Ctl struct {
	Env  Env
	Chip Chip
	Run  Runner
	Out  io.Writer
	Err  io.Writer
	Name string // $0, for usage messages

	// Detect lists the other tools that drive the LEDs (the rival detector); nil: none.
	Detect func() []conflict.Conflict

	// Now and Sleep are replaced in tests.
	Now   func() time.Time
	Sleep func(time.Duration)
	// Tick is the daemon's loop period (200 ms).
	Tick time.Duration

	product string
	bays    int
	leds    []string

	cfg       map[string]string
	cfgRaw    string
	cfgLoaded bool
	warned    map[string]bool // rejected settings already logged

	disks     map[string]string // sdX -> "<slot> <status> <spundown> <temp> <errors> <rotational> <warning> <critical> <id>"
	devList   []string
	devPath   map[string]string
	serials   map[string]string
	devSig    string
	devAt     time.Time
	devLoaded bool

	dyn       map[string]string
	dynNotify string
	dynLoaded bool

	bayPorts []string

	syncSt syncState
}

type syncState struct {
	active  bool
	action  string
	percent int64
}

func New(env Env, chip Chip, run Runner, out, errw io.Writer) *Ctl {
	c := &Ctl{
		Env: env, Chip: chip, Run: run, Out: out, Err: errw,
		Name:    "ugreen-led-ctl",
		Now:     time.Now,
		Sleep:   time.Sleep,
		Tick:    200 * time.Millisecond,
		product: env.Product,
		cfg:     map[string]string{},
		serials: map[string]string{},
		devPath: map[string]string{},
		dyn:     map[string]string{},
	}
	c.bays = bayCount(env.Product)
	c.leds = []string{"power", "netdev"}
	if env.Product == "iDX6011 Pro" {
		c.leds = append(c.leds, "netdev2") // second LAN LED, chip layout idx-pro only
	}
	for i := 1; i <= c.bays; i++ {
		c.leds = append(c.leds, "disk"+strconv.Itoa(i))
	}
	return c
}

func (c *Ctl) out(format string, a ...any)  { _, _ = fmt.Fprintf(c.Out, format, a...) }
func (c *Ctl) errf(format string, a ...any) { _, _ = fmt.Fprintf(c.Err, format, a...) }

// quiet runs f with stdout and stderr discarded (`>/dev/null 2>&1`).
func (c *Ctl) quiet(f func()) {
	o, e := c.Out, c.Err
	c.Out, c.Err = io.Discard, io.Discard
	defer func() { c.Out, c.Err = o, e }()
	f()
}

// silent runs f with stdout discarded (`>/dev/null`).
func (c *Ctl) silent(f func()) {
	o := c.Out
	c.Out = io.Discard
	defer func() { c.Out = o }()
	f()
}

func (c *Ctl) validLED(name string) bool {
	for _, l := range c.leds {
		if l == name {
			return true
		}
	}
	return false
}

func (c *Ctl) requireLED(name string) bool {
	if c.validLED(name) {
		return true
	}
	c.errf("ERROR: LED '%s' not found.\n", name)
	return false
}

// tool runs one chip command with the script's stdout/stderr and returns the exit code.
// Slow blink/breath commands also leave a phase stamp for the web preview.
func (c *Ctl) tool(args ...string) int {
	if rc := c.Chip.Exec(args, c.Out, c.Err); rc != 0 {
		return rc
	}
	c.stampPhase(args)
	return 0
}

// toolOut runs a chip command and returns its stdout. Its stderr goes to the script's
// when showErr is set, nowhere otherwise.
func (c *Ctl) toolOut(showErr bool, args ...string) (string, int) {
	var sb strings.Builder
	var errw io.Writer
	if showErr {
		errw = c.Err
	}
	rc := c.Chip.Exec(args, &sb, errw)
	if rc == 0 {
		c.stampPhase(args)
	}
	return sb.String(), rc
}

// stampPhase notes when a slow blink/breath was sent, so the browser can start its
// animation in step ("epoch_ms on off").
func (c *Ctl) stampPhase(args []string) {
	if len(args) < 4 || args[0] != "set" || (args[2] != "blink" && args[2] != "breath") {
		return
	}
	on, _ := atoi(args[3])
	off, _ := atoi(argAt(args, 4))
	if on+off < 500 {
		return
	}
	line := fmt.Sprintf("%d %s %s\n", c.Now().UnixMilli(), args[3], argAt(args, 4))
	_ = fsx.WriteFile(c.Env.PhasePrefix+args[1], []byte(line), 0o644)
}

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// ledLine is "<name> <mode> <brightness> <R> <G> <B> <on_ms> <off_ms>" (the tool's --plain output).
func (c *Ctl) ledLine(led string) (string, int) {
	out, rc := c.toolOut(true, "--plain", "get", led)
	return strings.TrimRight(out, "\n"), rc
}

func (c *Ctl) ledMode(led string) string {
	line, _ := c.ledLine(led)
	parts := strings.Split(line, " ")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func (c *Ctl) log(msg string) {
	if _, code := c.Run.Output("logger", "-t", "ugreen-led-control", msg); code != 0 {
		c.out("%s\n", msg)
	}
}

// pidAlive: the pid in the file belongs to a live process of this plugin (a reused pid does not count).
func (c *Ctl) pidAlive(path string) bool {
	line, ok := readLine(path)
	if !ok || line == "" {
		return false
	}
	pid, err := strconv.Atoi(line)
	if err != nil || pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil && proc.Ours(c.Env.Proc, pid)
}

func pidFromFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// killPid ends the process of a pidfile, if it is one of ours (a stale file never kills an unrelated process), and
// waits up to n tenths of a second for it to go. One that is still there then gets SIGKILL.
func (c *Ctl) killPid(path string, n int) {
	pid := pidFromFile(path)
	if pid <= 0 || !proc.Ours(c.Env.Proc, pid) {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if c.waitGone(pid, n) {
		return
	}
	if proc.Ours(c.Env.Proc, pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		c.waitGone(pid, 20)
	}
}

func (c *Ctl) waitGone(pid, n int) bool {
	for range n {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
		c.Sleep(100 * time.Millisecond)
	}
	return syscall.Kill(pid, 0) != nil
}
