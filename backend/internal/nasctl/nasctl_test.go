package nasctl

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
)

type fixture struct {
	t    *testing.T
	dir  string
	c    *Ctl
	out  *bytes.Buffer
	errw *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	p := func(s string) string { return filepath.Join(dir, s) }
	env := Env{
		FanTool: p("fan"), PidFile: p("run/fan.pid"), ErrLog: p("run/fan.err"), FanCfg: p("fan.cfg"),
		LEDDir: p("led"), LEDCtl: p("led/ugreen-led-ctl"), LEDI2C: p("led/ugreen-led-i2c"), LEDFlag: p("led.enabled"),
		LEDCron: p("x.cron"), LEDCfg: p("leds.cfg"), LEDImportSrc: p("old/leds.cfg"), DMIDir: p("dmi"),
		PluginsBoot: p("boot"), PluginsWeb: p("web"), ProcModules: p("modules"), UpdateCron: p("update_cron"),
		LegacyPids: []string{p("run/legacy.pid")}, Proc: p("proc"),
	}
	for _, d := range []string{"run", "led", "dmi", "proc"} {
		if err := os.MkdirAll(p(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, p("dmi/product_name"), "DXP6800 Pro\n")
	f := &fixture{t: t, dir: dir, out: &bytes.Buffer{}, errw: &bytes.Buffer{}}
	f.c = New(env, f.out, f.errw)
	f.c.Sleep = func(time.Duration) {}
	f.c.Detect = func() []conflict.Conflict { return nil }
	return f
}

// fakeProcess starts a real `sleep` and describes it to the fake /proc as cmdline.
func (f *fixture) fakeProcess(cmdline string) (pid int, gone <-chan struct{}) {
	f.t.Helper()
	child := exec.Command("sleep", "100")
	if err := child.Start(); err != nil {
		f.t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); close(done) }()
	f.t.Cleanup(func() { _ = child.Process.Kill() })
	dir := filepath.Join(f.c.Env.Proc, strconv.Itoa(child.Process.Pid))
	write(f.t, filepath.Join(dir, "cmdline"), strings.ReplaceAll(cmdline, " ", "\x00")+"\x00")
	return child.Process.Pid, done
}

func ended(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	case <-time.After(1500 * time.Millisecond):
		return false
	}
}

// waitFile returns the content of path once it exists. The scripts under test publish it with an atomic rename, since
// `echo >file` creates the file empty before it writes.
func waitFile(t *testing.T, path string) string {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if data, err := os.ReadFile(path); err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	t.Fatalf("%s was not written", filepath.Base(path))
	return ""
}

func TestStopNeverSignalsAForeignProcess(t *testing.T) {
	f := newFixture(t)
	pid, gone := f.fakeProcess("sleep 100")
	write(t, f.c.Env.PidFile, strconv.Itoa(pid)+"\n")
	if f.c.running(f.c.Env.PidFile) {
		t.Error("a foreign process counts as the running daemon")
	}
	if rc := f.c.Main([]string{"stop"}); rc != 0 {
		t.Errorf("stop: %d", rc)
	}
	if ended(gone) {
		t.Error("the foreign process was signalled")
	}
	if _, err := os.Stat(f.c.Env.PidFile); err == nil {
		t.Error("the stale pidfile stays")
	}
}

func TestStopEndsOurDaemon(t *testing.T) {
	f := newFixture(t)
	pid, gone := f.fakeProcess("/x/backend/ugreen-nas-fan daemon")
	write(t, f.c.Env.PidFile, strconv.Itoa(pid)+"\n")
	if !f.c.running(f.c.Env.PidFile) {
		t.Fatal("our daemon is not recognised")
	}
	f.c.Sleep = time.Sleep
	f.c.Main([]string{"stop"})
	if !ended(gone) {
		t.Error("the daemon was not stopped")
	}
}

// led-stop ends a leftover daemon of this plugin's own led/ directory that uses the old shared pidfile, and keeps
// the standalone plugin's.
func TestLedStopLegacyPidfile(t *testing.T) {
	f := newFixture(t)
	foreign, foreignGone := f.fakeProcess("/usr/local/emhttp/plugins/ugreen-led-control/backend/ugreen-led-ctl daemon")
	write(t, f.c.Env.LegacyPids[0], strconv.Itoa(foreign)+"\n")
	f.c.ledStop()
	if ended(foreignGone) {
		t.Error("the standalone plugin's daemon was stopped")
	}
	if _, err := os.Stat(f.c.Env.LegacyPids[0]); err != nil {
		t.Error("the foreign pidfile was removed")
	}

	own, ownGone := f.fakeProcess(f.c.Env.LEDDir + "/ugreen-led-ctl daemon run")
	write(t, f.c.Env.LegacyPids[0], strconv.Itoa(own)+"\n")
	f.c.ledStop()
	if !ended(ownGone) {
		t.Error("the leftover daemon of our own folder was not stopped")
	}
	if _, err := os.Stat(f.c.Env.LegacyPids[0]); err == nil {
		t.Error("its pidfile stays")
	}
}

func TestBlockerComesFromTheRivalDetector(t *testing.T) {
	f := newFixture(t)
	for _, tc := range []struct {
		name          string
		list          []conflict.Conflict
		blocker, warn string
	}{
		{"none", nil, "", ""},
		{"standalone plugin only warns", []conflict.Conflict{{Kind: conflict.KindPlugin, ID: "ugreen-led-control", Controls: []string{conflict.LED}}}, "", "standalone"},
		{"driver plugin", []conflict.Conflict{{Kind: conflict.KindPlugin, ID: "ugreenleds-driver", Controls: []string{conflict.LED}}}, "driver", ""},
		{"another LED plugin", []conflict.Conflict{{Kind: conflict.KindPlugin, ID: "UGREEN-LEDs-unRAID", Controls: []string{conflict.LED}}}, "driver", ""},
		{"running LED daemon", []conflict.Conflict{{Kind: conflict.KindDaemon, ID: "ugreen-leds", Controls: []string{conflict.LED}, Severity: conflict.SeverityBlock}}, "rival", ""},
		{"kernel module", []conflict.Conflict{{Kind: conflict.KindModule, ID: "led_ugreen", Controls: []string{conflict.LED}}}, "module", ""},
		{"fan plugin does not matter", []conflict.Conflict{{Kind: conflict.KindPlugin, ID: "fanctrlplus", Controls: []string{conflict.Fan}}}, "", ""},
		{"driver before module", []conflict.Conflict{{Kind: conflict.KindModule, ID: "led_ugreen", Controls: []string{conflict.LED}}, {Kind: conflict.KindPlugin, ID: "ugreenleds-driver", Controls: []string{conflict.LED}}}, "driver", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.c.found, f.c.scanned = nil, false
			f.c.Detect = func() []conflict.Conflict { return tc.list }
			if got := f.c.ledBlocker(); got != tc.blocker {
				t.Errorf("blocker %q, want %q", got, tc.blocker)
			}
			if got := f.c.ledWarning(); got != tc.warn {
				t.Errorf("warning %q, want %q", got, tc.warn)
			}
		})
	}
}

func TestEventStopping(t *testing.T) {
	f := newFixture(t)
	marker := filepath.Join(f.dir, "called")
	write(t, f.c.Env.LEDCtl, "#!/bin/sh\necho \"$@\" >"+marker+".tmp && mv "+marker+".tmp "+marker+"\n")
	if err := os.Chmod(f.c.Env.LEDCtl, 0o755); err != nil {
		t.Fatal(err)
	}

	if rc := f.c.Main([]string{"event", "stopping"}); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Error("the LED tool ran with the LEDs switched off")
	}

	write(t, f.c.Env.LEDFlag, "")
	if rc := f.c.Main([]string{"event", "stopping"}); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	if got := waitFile(t, marker); got != "shutdown" {
		t.Errorf("called with %q", got)
	}
}

func TestUsage(t *testing.T) {
	f := newFixture(t)
	for _, args := range [][]string{nil, {"bogus"}, {"event"}, {"event", "other"}} {
		if rc := f.c.Main(args); rc != 2 {
			t.Errorf("%v: rc %d", args, rc)
		}
	}
	if !strings.Contains(f.out.String(), "report") {
		t.Error("usage does not list report")
	}
}

// fakePublisher makes FanTool a script that records its arguments and then sleeps, and has the fake /proc describe the
// process it becomes the moment the command waits for it to come up.
func fakePublisher(t *testing.T, f *fixture, script string) (args string) {
	t.Helper()
	args = filepath.Join(f.dir, "args")
	write(t, f.c.Env.FanTool, "#!/bin/sh\necho \"$@\" >"+args+".tmp && mv "+args+".tmp "+args+"\n"+script+"\n")
	if err := os.Chmod(f.c.Env.FanTool, 0o755); err != nil {
		t.Fatal(err)
	}
	f.c.Env.PubPidFile, f.c.Env.PubLog = filepath.Join(f.dir, "run/mqtt.pid"), filepath.Join(f.dir, "run/mqtt.log")
	f.c.Sleep = func(time.Duration) {
		data, _ := os.ReadFile(f.c.Env.PubPidFile)
		pid := strings.TrimSpace(string(data))
		write(t, filepath.Join(f.c.Env.Proc, pid, "cmdline"), "/x/backend/ugreen-nas-fan\x00publish\x00")
		time.Sleep(200 * time.Millisecond)
	}
	return args
}

func TestMQTTPublisherOnlyRunsWhenSwitchedOn(t *testing.T) {
	f := newFixture(t)
	args := fakePublisher(t, f, "sleep 100")
	if rc := f.c.Main([]string{"mqtt-start"}); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	if _, err := os.Stat(args); err == nil {
		t.Fatal("the publisher started without mqtt_enabled")
	}
	if rc := f.c.Main([]string{"mqtt-status"}); rc != 3 {
		t.Errorf("status of a stopped publisher: rc %d", rc)
	}
	write(t, f.c.Env.FanCfg, "mqtt_enabled=\"0\"\nmqtt_enabled=\"1\"\n")
	if rc := f.c.Main([]string{"mqtt-start"}); rc != 0 {
		t.Fatalf("rc %d: %s", rc, f.errw)
	}
	if got := waitFile(t, args); got != "publish" {
		t.Errorf("started with %q", got)
	}
	if rc := f.c.Main([]string{"mqtt-status"}); rc != 0 {
		t.Errorf("status of a running publisher: rc %d", rc)
	}
	pid, _ := strconv.Atoi(f.c.pidText(f.c.Env.PubPidFile))
	f.c.Sleep = time.Sleep
	f.c.Main([]string{"mqtt-stop"})
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Error("the publisher was not stopped")
	}
	if _, err := os.Stat(f.c.Env.PubPidFile); err == nil {
		t.Error("its pidfile stays")
	}
}

// The publisher starts with the fan daemon, also where fan control is off, and a publisher that cannot start leaves
// the fan daemon's result alone.
func TestStartRunsThePublisherNextToTheFans(t *testing.T) {
	f := newFixture(t)
	fakePublisher(t, f, "echo no MQTT host in the settings >&2; exit 1")
	// The script must really be gone when pubStart looks again; a fixed pause is too short on a loaded machine.
	f.c.Sleep = func(time.Duration) {
		pid, _ := strconv.Atoi(f.c.pidText(f.c.Env.PubPidFile))
		for end := time.Now().Add(10 * time.Second); syscall.Kill(pid, 0) == nil && time.Now().Before(end); {
			time.Sleep(5 * time.Millisecond)
		}
	}
	write(t, f.c.Env.FanCfg, "fan_control=\"0\"\nmqtt_enabled=\"1\"\n")
	if rc := f.c.Main([]string{"start"}); rc != 0 {
		t.Errorf("start: rc %d", rc)
	}
	if !strings.Contains(f.errw.String(), "MQTT publisher did not start") {
		t.Errorf("no error for the publisher: %q", f.errw)
	}
	if _, err := os.Stat(f.c.Env.PubPidFile); err == nil {
		t.Error("the pidfile of a publisher that ended stays")
	}
	if rc := f.c.Main([]string{"mqtt-start"}); rc != 1 {
		t.Errorf("mqtt-start alone reports the failure: rc %d", rc)
	}
}

func TestStopKillsADaemonThatIgnoresSIGTERM(t *testing.T) {
	f := newFixture(t)
	ready := filepath.Join(f.dir, "ready")
	child := exec.Command("sh", "-c", `trap "" TERM; : >"$0"; while :; do sleep 1; done`, ready)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = child.Wait(); close(done) }()
	t.Cleanup(func() { _ = child.Process.Kill() })
	pid := child.Process.Pid
	write(t, filepath.Join(f.c.Env.Proc, strconv.Itoa(pid), "cmdline"), "/x/backend/ugreen-nas-fan\x00daemon\x00")
	write(t, f.c.Env.PidFile, strconv.Itoa(pid)+"\n")
	waitFile(t, ready)
	f.c.Sleep = time.Sleep
	f.c.terminate(f.c.Env.PidFile, 2)
	if !ended(done) {
		t.Error("the daemon survived")
	}
	if _, err := os.Stat(f.c.Env.PidFile); err == nil {
		t.Error("the pidfile stays after the kill")
	}
}

// ledBootCalls runs ledBoot with a fake LED tool pair that logs every call, and returns the log.
func ledBootCalls(t *testing.T, f *fixture, product string, enabled bool) string {
	t.Helper()
	log := filepath.Join(f.dir, "calls")
	script := "#!/bin/sh\necho \"$(basename $0) $@\" >>" + log + "\n"
	write(t, f.c.Env.LEDCtl, script)
	write(t, f.c.Env.LEDI2C, script)
	write(t, f.c.Env.UpdateCron, script)
	for _, p := range []string{f.c.Env.LEDCtl, f.c.Env.LEDI2C, f.c.Env.UpdateCron} {
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.c.Env.LEDCron = filepath.Join(f.dir, "cron.d", "x.cron")
	write(t, filepath.Join(f.dir, "dmi/product_name"), product+"\n")
	if enabled {
		write(t, f.c.Env.LEDFlag, "")
	}
	if rc := f.c.ledBoot(); rc != 0 {
		t.Fatalf("rc %d", rc)
	}
	data, _ := os.ReadFile(log)
	return string(data)
}

func TestLedBootDXP480TSendsTheWhiteInitWithoutProbe(t *testing.T) {
	f := newFixture(t)
	got := ledBootCalls(t, f, "DXP480T Plus", true)
	if !strings.Contains(got, "ugreen-led-ctl dxp480t\n") {
		t.Errorf("no steady-white init: %q", got)
	}
	if strings.Contains(got, "probe") || strings.Contains(got, "apply") || strings.Contains(f.out.String(), "does not answer") {
		t.Errorf("0x3a controller path ran on a model without it: %q / %q", got, f.out.String())
	}
}

func TestLedBootDXP480TLeavesTheLEDAloneWhenSwitchedOff(t *testing.T) {
	f := newFixture(t)
	if got := ledBootCalls(t, f, "DXP480T Plus", false); got != "" {
		t.Errorf("LED tools ran with LED control off: %q", got)
	}
}

func TestLedBootOtherModelsStillProbe(t *testing.T) {
	f := newFixture(t)
	got := ledBootCalls(t, f, "DXP6800 Pro", true)
	if !strings.Contains(got, "ugreen-led-i2c probe\n") || strings.Contains(got, "dxp480t") {
		t.Errorf("calls %q", got)
	}
}
