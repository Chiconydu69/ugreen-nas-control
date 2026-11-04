// Package parity runs the Go binary against a fake system and compares what it prints and what it leaves
// behind with goldens recorded from the shell scripts it replaced (testdata/golden).
package parity

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	goBinary    string // freshly built multicall binary
	bashPath    string // bash >= 4 (the fake chip and the stubs are bash)
	testdataDir string // internal/led/parity/testdata
	fakeChip    string // web/tools/harness/fake-led-i2c
)

// rig is the Go binary in its own temp root, on a fake system.
type rig struct {
	t       *testing.T
	impl    string // "sh" or "go"
	root    string
	ctl     string
	env     map[string]string
	cmds    []cmdRec
	cleanup []*exec.Cmd
}

func newRig(t *testing.T, impl, product string) *rig {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, impl: impl, root: dir, env: map[string]string{}}
	for _, d := range []string{"ctl", "run", "led", "stubs", "boot", "notify/unread", "smart", "udev"} {
		r.mkdir(d)
	}

	r.ctl = r.path("ctl/ugreen-led-ctl")
	data, err := os.ReadFile(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	r.write("ctl/ugreen-led-ctl", string(data))
	if err := os.Chmod(r.ctl, 0o755); err != nil {
		t.Fatal(err)
	}

	e := r.env
	e["UGREEN_LED_I2C"] = filepath.Join(testdataDir, "bin", "led-chip")
	e["FAKE_CHIP"] = fakeChip
	e["FAKE_LED_DIR"] = r.path("led")
	e["CALL_LOG"] = r.path("calls.log")
	e["STUB_LOG"] = r.path("stubs.log")
	e["STUBS"] = r.path("stubs")
	e["UGREEN_PRODUCT"] = product
	e["UGREEN_NOW"] = "1200"
	e["UGREEN_OSRELEASE"] = r.path("osrelease")
	e["UGREEN_LED_CONFIG"] = r.path("boot/leds.cfg")
	e["UGREEN_DISKS_INI"] = r.path("disks.ini")
	e["UGREEN_VAR_INI"] = r.path("var.ini")
	e["UGREEN_SMART_DIR"] = r.path("smart")
	e["UGREEN_SYS_BLOCK"] = r.path("sys/block")
	e["UGREEN_SYS_NET"] = r.path("sys/class/net")
	e["UGREEN_SYS_ATA"] = r.path("sys/class/ata_port")
	e["UGREEN_SYS_PCI"] = r.path("sys/bus/pci/devices")
	e["UGREEN_UDEV_DATA"] = r.path("udev")
	e["UGREEN_DYNAMIX_CFG"] = r.path("dynamix.cfg")
	e["UGREEN_PIDFILE"] = r.path("run/led.pid")
	e["UGREEN_SCHEDULE_STATE"] = r.path("run/schedule")
	e["UGREEN_FX_PIDFILE"] = r.path("run/fx.pid")
	e["UGREEN_FX_LOG"] = r.path("run/fx.log")
	e["UGREEN_IDENTIFY_PREFIX"] = r.path("run/identify-")
	e["UGREEN_ALERT_STATE"] = r.path("run/alert")
	e["UGREEN_PHASE_PREFIX"] = r.path("run/phase-")
	e["UGREEN_SHUTDOWN_MARK"] = r.path("run/shutdown")
	e["UGREEN_BUS_CACHE"] = r.path("run/bus")
	e["UGREEN_MAXSPEED_CACHE"] = r.path("run/maxspeed")

	r.defaultSystem()
	t.Cleanup(r.stopAll)
	return r
}

func (r *rig) path(rel string) string { return filepath.Join(r.root, rel) }

func (r *rig) mkdir(rel string) {
	if err := os.MkdirAll(r.path(rel), 0o755); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) write(rel, content string) {
	r.mkdir(filepath.Dir(rel))
	if err := writeFile(r.path(rel), content); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) remove(rel string) { _ = os.RemoveAll(r.path(rel)) }

func (r *rig) symlink(rel, target string) {
	r.mkdir(filepath.Dir(rel))
	_ = os.Remove(r.path(rel))
	if err := os.Symlink(target, r.path(rel)); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) read(rel string) string {
	data, _ := os.ReadFile(r.path(rel))
	return string(data)
}

func (r *rig) sleep(d time.Duration) { time.Sleep(d) }

func (r *rig) environ() []string {
	pathEnv := filepath.Join(testdataDir, "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	env := []string{"PATH=" + pathEnv, "HOME=" + r.root, "LC_ALL=C", "TZ=UTC"}
	keys := make([]string, 0, len(r.env))
	for k := range r.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+r.env[k])
	}
	return env
}

func (r *rig) command(args ...string) *exec.Cmd {
	cmd := exec.Command(r.ctl, args...)
	cmd.Env = r.environ()
	cmd.Dir = r.root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func (r *rig) record(args []string, stdout, stderr string, code int) {
	r.cmds = append(r.cmds, cmdRec{Args: append([]string{}, args...), RC: code, Stdout: stdout, Stderr: stderr})
}

func ensureNL(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

func (r *rig) run(args ...string) int {
	cmd := r.command(args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	done := make(chan struct{})
	if err := startProc(cmd); err != nil {
		r.t.Fatal(err)
	}
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		r.t.Fatalf("%s: ctl %v timed out", r.impl, args)
	}
	code := cmd.ProcessState.ExitCode()
	r.record(args, so.String(), se.String(), code)
	return code
}

// daemon runs `daemon run` in the foreground of a goroutine, calls during while it runs,
// then ends it with SIGTERM like `daemon stop` does.
func (r *rig) daemon(during func()) {
	cmd := r.command("daemon", "run")
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := startProc(cmd); err != nil {
		r.t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	during()
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		r.t.Fatalf("%s: daemon did not end on SIGTERM", r.impl)
	}
	r.record([]string{"daemon", "run"}, so.String(), se.String(), cmd.ProcessState.ExitCode())
}

// stopAll ends every process a scenario left behind (daemon, effect, timers).
func (r *rig) stopAll() {
	files, _ := filepath.Glob(r.path("run/*.pid"))
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 && pid != os.Getpid() {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
	for _, c := range r.cleanup {
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
	}
	killUnder(r.root)
}

// killUnder ends whatever still runs from a rig's temp root: the detached timers behind identify and shutdown --test.
func killUnder(root string) {
	cmd := exec.Command("pkill", "-f", root)
	if startProc(cmd) == nil {
		_ = cmd.Wait()
	}
}

// sleeper starts a long-lived process whose pid can stand in for a running daemon. It runs under a tool name
// (ugreen-led-ctl): the controller only trusts pids of its own processes.
func (r *rig) sleeper(pidfile string) {
	cmd := sleeperCmd(r.root, "ugreen-led-ctl")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := startProc(cmd); err != nil {
		r.t.Fatal(err)
	}
	r.cleanup = append(r.cleanup, cmd)
	go func() { _ = cmd.Wait() }()
	r.write(pidfile, strconv.Itoa(cmd.Process.Pid)+"\n")
}

var (
	reNow  = regexp.MustCompile(`"now_ms": [0-9]+`)
	reSent = regexp.MustCompile(`"sent_ms": [0-9]+`)
	rePid  = regexp.MustCompile(`\(pid [0-9]+\)`)
)

func (r *rig) normalize(s string) string {
	s = strings.ReplaceAll(s, r.root, "<ROOT>")
	s = reNow.ReplaceAllString(s, `"now_ms": <MS>`)
	s = reSent.ReplaceAllString(s, `"sent_ms": <MS>`)
	s = rePid.ReplaceAllString(s, "(pid <PID>)")
	return s
}

// happened lists what the run left behind besides the transcript: chip and stub calls, registers,
// state files. Empty means nothing took effect, which a scenario that expects effects must not accept.
func (r *rig) happened() string {
	var sb strings.Builder
	sb.WriteString(r.read("calls.log"))
	sb.WriteString(r.read("stubs.log"))
	for _, pattern := range []string{"led/*", "run/*"} {
		files, _ := filepath.Glob(r.path(pattern))
		for _, f := range files {
			sb.WriteString(filepath.Base(f))
		}
	}
	return sb.String()
}

type cmdRec struct {
	Args   []string `json:"args"`
	RC     int      `json:"rc"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
}

// golden is everything a scenario leaves behind: the transcript, the LED registers, the state files, the
// calls the chip and the external commands got. It is the recorded behaviour of the shell scripts.
type golden struct {
	Name      string            `json:"name"`
	Product   string            `json:"product"`
	Commands  []cmdRec          `json:"commands"`
	Registers map[string]string `json:"registers"`
	State     map[string]string `json:"state"`
	Calls     []string          `json:"calls"`
	Stubs     []string          `json:"stubs"`
}

func lines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// snapshot collects what the run left behind, normalised (temp root, clock values, pids).
func (r *rig) snapshot(unordered bool) golden {
	g := golden{Registers: map[string]string{}, State: map[string]string{}}
	for _, c := range r.cmds {
		g.Commands = append(g.Commands, cmdRec{Args: c.Args, RC: c.RC, Stdout: r.normalize(c.Stdout), Stderr: r.normalize(c.Stderr)})
	}
	regs, _ := filepath.Glob(r.path("led/*"))
	for _, f := range regs {
		data, _ := os.ReadFile(f)
		g.Registers[filepath.Base(f)] = r.normalize(string(data))
	}
	files, _ := filepath.Glob(r.path("run/*"))
	for _, f := range files {
		base := filepath.Base(f)
		data, _ := os.ReadFile(f)
		text := string(data)
		switch {
		case strings.HasSuffix(base, ".pid"):
			text = "<PID>\n"
		case strings.HasPrefix(base, "identify-"):
			if i := strings.IndexByte(text, '\n'); i >= 0 {
				text = "<TOKEN>" + text[i:]
			}
		case strings.HasPrefix(base, "phase-"):
			f := strings.Fields(text)
			if len(f) > 0 {
				text = "<MS> " + strings.Join(f[1:], " ") + "\n"
			}
		}
		g.State[base] = r.normalize(text)
	}
	for _, name := range []string{"calls.log", "stubs.log"} {
		l := lines(r.normalize(r.read(name)))
		if unordered {
			// Timing decides the order, not what is called or how often: compare as a multiset.
			sort.Strings(l)
		}
		if name == "calls.log" {
			g.Calls = l
		} else {
			g.Stubs = l
		}
	}
	return g
}

func (g golden) text() string {
	var sb strings.Builder
	for _, c := range g.Commands {
		fmt.Fprintf(&sb, "$ ctl %s\n[rc] %d\n[stdout]\n%s", strings.Join(c.Args, " "), c.RC, ensureNL(c.Stdout))
		fmt.Fprintf(&sb, "[stderr]\n%s\n", ensureNL(c.Stderr))
	}
	sb.WriteString("== registers ==\n")
	for _, k := range sortedKeys(g.Registers) {
		fmt.Fprintf(&sb, "%s: %s", k, ensureNL(g.Registers[k]))
	}
	sb.WriteString("== state files ==\n")
	for _, k := range sortedKeys(g.State) {
		fmt.Fprintf(&sb, "%s: %s", k, ensureNL(g.State[k]))
	}
	fmt.Fprintf(&sb, "== calls.log ==\n%s\n== stubs.log ==\n%s\n", strings.Join(g.Calls, "\n"), strings.Join(g.Stubs, "\n"))
	return sb.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sleeperCmd is a long-lived process that shows up under the name of one of the plugin's tools (argv[0]): a process the
// pid checks accept as ours. (A symlink to sleep does not work: the coreutils sleep is a multicall binary itself.)
func sleeperCmd(root, name string) *exec.Cmd {
	cmd := exec.Command("bash", "-c", "while :; do sleep 1; done")
	cmd.Args[0] = filepath.Join(root, "sleepers", name)
	return cmd
}

// forkMu keeps a fork from inheriting the write fd of an executable another parallel test is still writing: that exec
// would fail with ETXTBSY ("text file busy") on Linux.
var forkMu sync.RWMutex

func startProc(cmd *exec.Cmd) error {
	forkMu.RLock()
	defer forkMu.RUnlock()
	return cmd.Start()
}

func writeFile(path, content string) error {
	forkMu.Lock()
	defer forkMu.Unlock()
	return os.WriteFile(path, []byte(content), 0o644)
}
