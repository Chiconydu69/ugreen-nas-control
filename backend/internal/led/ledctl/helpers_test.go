package ledctl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// memChip is an in-memory LED controller: the registers of the fake chip script.
type memChip struct {
	mu    sync.Mutex
	state map[string][7]string // mode brightness R G B on off
	calls []string
	fail  bool
}

var allLEDs = []string{"power", "netdev", "disk1", "disk2", "disk3", "disk4", "disk5", "disk6"}

func newMemChip() *memChip { return &memChip{state: map[string][7]string{}} }

func (m *memChip) led(name string) [7]string {
	if s, ok := m.state[name]; ok {
		return s
	}
	return [7]string{"on", "255", "255", "255", "255", "0", "0"}
}

func (m *memChip) Exec(args []string, stdout, _ io.Writer) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, strings.Join(args, " "))
	if m.fail {
		return 1
	}
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		args = args[1:]
	}
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "probe":
		if stdout != nil {
			_, _ = fmt.Fprint(stdout, "{\n  \"bus\": \"/dev/i2c-4\",\n  \"address\": \"0x3a\"\n}\n")
		}
	case "get":
		names := []string{args[1]}
		if args[1] == "all" {
			names = allLEDs
		}
		for _, n := range names {
			s := m.led(n)
			if stdout != nil {
				_, _ = fmt.Fprintf(stdout, "%s %s\n", n, strings.Join(s[:], " "))
			}
		}
	case "set":
		s := m.led(args[1])
		switch args[2] {
		case "color":
			s[2], s[3], s[4] = args[3], args[4], args[5]
		case "brightness":
			s[1] = args[3]
		case "on":
			s[0] = "on"
		case "off":
			s[0] = "off"
		case "blink", "breath":
			s[0], s[5], s[6] = args[2], args[3], args[4]
		}
		m.state[args[1]] = s
	}
	return 0
}

func (m *memChip) callList() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *memChip) set(name string, s [7]string) {
	m.mu.Lock()
	m.state[name] = s
	m.mu.Unlock()
}

func (m *memChip) get(name string) [7]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.led(name)
}

type fakeRunner struct {
	mu       sync.Mutex
	outputs  map[string]string // "name args..." or "name" -> stdout
	codes    map[string]int
	calls    []string
	spawned  []SpawnSpec
	have     map[string]bool
	spawnErr error
}

func newRunner() *fakeRunner {
	return &fakeRunner{outputs: map[string]string{}, codes: map[string]int{"pgrep": 1}, have: map[string]bool{}}
}

func (f *fakeRunner) Output(name string, args ...string) ([]byte, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, key)
	if code, ok := f.codes[key]; ok {
		return []byte(f.outputs[key]), code
	}
	if code, ok := f.codes[name]; ok {
		return []byte(f.outputs[name]), code
	}
	if out, ok := f.outputs[key]; ok {
		return []byte(out), 0
	}
	return []byte(f.outputs[name]), 0
}

func (f *fakeRunner) Has(name string) bool { return f.have[name] }

func (f *fakeRunner) Spawn(s SpawnSpec) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spawned = append(f.spawned, s)
	if f.spawnErr != nil {
		return 0, f.spawnErr
	}
	return os.Getpid(), nil
}

func (f *fakeRunner) called(prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

type rig struct {
	t    *testing.T
	dir  string
	env  Env
	chip *memChip
	run  *fakeRunner
	out  *bytes.Buffer
	err  *bytes.Buffer
	c    *Ctl
}

// newRig builds a Ctl on a temp root; every path of Env points into it.
func newRig(t *testing.T, product string) *rig {
	t.Helper()
	dir := t.TempDir()
	p := func(rel string) string { return filepath.Join(dir, rel) }
	for _, d := range []string{"run", "sys/block", "sys/net", "sys/ata", "sys/pci", "sys/i2c", "smart", "udev", "notify/unread"} {
		if err := os.MkdirAll(p(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env := Env{
		Tool: "/nonexistent/led-tool", ToolFromEnv: true, Self: "/nonexistent/ugreen-led-ctl",
		Config: p("leds.cfg"), DisksINI: p("disks.ini"), VarINI: p("var.ini"), SmartDir: p("smart"),
		SysBlock: p("sys/block"), SysNet: p("sys/net"), SysATA: p("sys/ata"), SysPCI: p("sys/pci"),
		PidFile: p("run/pid"), ScheduleState: p("run/schedule"), FxPidFile: p("run/fx.pid"), FxLog: p("run/fx.log"),
		IdentifyPrefix: p("run/identify-"), AlertState: p("run/alert"), PhasePrefix: p("run/phase-"),
		ShutdownMark: p("run/shutdown"), BusCache: p("run/bus"), DynamixCfg: p("dynamix.cfg"), UdevData: p("udev"),
		MaxspeedCache: p("run/maxspeed"), NotifyDir: p("notify"), Now: "1200", Product: product,
		DMIProduct: p("dmi"), OSRelease: p("osrelease"), ProcIomem: p("iomem"), ProcModules: p("modules"), SysI2C: p("sys/i2c"),
	}
	r := &rig{t: t, dir: dir, env: env, chip: newMemChip(), run: newRunner(), out: &bytes.Buffer{}, err: &bytes.Buffer{}}
	r.c = New(env, r.chip, r.run, r.out, r.err)
	r.c.Sleep = func(time.Duration) {}
	r.c.Name = "ctl"
	return r
}

func (r *rig) write(rel, content string) {
	r.t.Helper()
	path := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) config(content string) {
	r.write("leds.cfg", content)
	r.c.cfgLoad()
}

func (r *rig) ctl(args ...string) int {
	r.out.Reset()
	r.err.Reset()
	return r.c.Main(r.t.Context(), args)
}
