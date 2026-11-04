package parity

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The scenarios of ugreen-nas-ctl (fan daemon control, the LED switch) against a fake plugin tree: stand-ins for the
// fan and LED tools note their calls, nothing touches /boot or /usr/local.

type nasScenario struct {
	name     string
	intended string // reason the Go port differs from the shell on purpose; the scenario must differ
	setup    func(n *nrig)
	run      func(n *nrig)
}

type nrig struct {
	t       *testing.T
	impl    string
	root    string
	ctl     string
	env     map[string]string
	cmds    []cmdRec
	cleanup []*exec.Cmd
}

const nasCtlStub = `#!/bin/bash
echo "%s $*" >>"$CALL_LOG"
echo "%s-out $*"
[ -e "$STUBS/%s.fail" ] && exit 1
exit 0
`

const nasFanStub = `#!/bin/bash
echo "fan $*" >>"$CALL_LOG"
case "$1" in
    probe)
        if [ -e "$STUBS/fan.probe.fail" ]; then
            cat "$STUBS/fan.probe.fail"
            exit 1
        fi
        echo '{"ok": true, "chip": "it8613"}'
        ;;
    daemon)
        if [ -e "$STUBS/fan.daemon.fail" ]; then
            echo "no BIOS snapshot" >&2
            exit 1
        fi
        exec -a "$SLEEPER" bash -c 'while :; do sleep 1; done'
        ;;
esac
`

func newNasRig(t *testing.T, impl, product string) *nrig {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := &nrig{t: t, impl: impl, root: dir, env: map[string]string{}}
	for _, d := range []string{"ctl", "dmi", "boot", "web", "led", "run", "old", "stubs", "fan", "proc", "sysmod", "hwmon"} {
		n.mkdir(d)
	}
	n.ctl = n.path("ctl/ugreen-nas-ctl")
	data, err := os.ReadFile(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	n.write("ctl/ugreen-nas-ctl", string(data))
	n.exec("ctl/ugreen-nas-ctl")

	n.write("led/ugreen-led-ctl", stubFor(nasCtlStub, "ctl"))
	n.write("led/ugreen-led-i2c", stubFor(nasCtlStub, "i2c"))
	n.write("update_cron", stubFor(nasCtlStub, "update_cron"))
	n.write("fan/ugreen-nas-fan", nasFanStub)
	for _, f := range []string{"led/ugreen-led-ctl", "led/ugreen-led-i2c", "update_cron", "fan/ugreen-nas-fan"} {
		n.exec(f)
	}
	n.write("dmi/product_name", product+"\n")
	n.write("proc/modules", "")

	e := n.env
	e["CALL_LOG"] = n.path("calls.log")
	e["STUB_LOG"] = n.path("stubs.log")
	e["STUBS"] = n.path("stubs")
	e["SLEEPER"] = filepath.Join(n.root, "sleepers", "ugreen-nas-fan")
	e["UGREEN_DMI"] = n.path("dmi")
	e["UGREEN_PLUGINS_BOOT"] = n.path("boot")
	e["UGREEN_PLUGINS_WEB"] = n.path("web")
	e["UGREEN_PROC_MODULES"] = n.path("proc/modules")
	e["UGREEN_SYS_MODULE"] = n.path("sysmod")
	e["UGREEN_SYS_HWMON"] = n.path("hwmon")
	e["UGREEN_LED_DIR"] = n.path("led")
	e["UGREEN_LED_FLAG"] = n.path("boot/led.enabled")
	e["UGREEN_LED_CRON"] = n.path("boot/x.cron")
	e["UGREEN_LED_CFG"] = n.path("boot/leds.cfg")
	e["UGREEN_LED_IMPORT_SRC"] = n.path("old/leds.cfg")
	e["UGREEN_UPDATE_CRON"] = n.path("update_cron")
	e["UGREEN_LED_LEGACY_PIDS"] = n.path("run/legacy.pid")
	e["UGREEN_NAS_FAN"] = n.path("fan/ugreen-nas-fan")
	e["UGREEN_FAN_PIDFILE"] = n.path("run/fan.pid")
	e["UGREEN_FAN_ERRLOG"] = n.path("run/fan.err")
	e["UGREEN_FAN_CONFIG"] = n.path("boot/fan.cfg")
	t.Cleanup(n.stopAll)
	return n
}

func stubFor(tmpl, name string) string { return strings.ReplaceAll(tmpl, "%s", name) }

func (n *nrig) path(rel string) string { return filepath.Join(n.root, rel) }

func (n *nrig) mkdir(rel string) {
	if err := os.MkdirAll(n.path(rel), 0o755); err != nil {
		n.t.Fatal(err)
	}
}

func (n *nrig) write(rel, content string) {
	n.mkdir(filepath.Dir(rel))
	if err := writeFile(n.path(rel), content); err != nil {
		n.t.Fatal(err)
	}
}

func (n *nrig) exec(rel string) {
	if err := os.Chmod(n.path(rel), 0o755); err != nil {
		n.t.Fatal(err)
	}
}

func (n *nrig) remove(rel string) { _ = os.RemoveAll(n.path(rel)) }

func (n *nrig) read(rel string) string {
	data, _ := os.ReadFile(n.path(rel))
	return string(data)
}

func (n *nrig) model(p string) { n.write("dmi/product_name", p+"\n") }

func (n *nrig) environ() []string {
	env := []string{"PATH=" + filepath.Join(testdataDir, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + n.root, "LC_ALL=C", "TZ=UTC"}
	keys := make([]string, 0, len(n.env))
	for k := range n.env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+n.env[k])
	}
	return env
}

func (n *nrig) runs(lines ...string) {
	for _, l := range lines {
		n.run(strings.Fields(l)...)
	}
}

func (n *nrig) run(args ...string) int {
	cmd := exec.Command(n.ctl, args...)
	cmd.Env = n.environ()
	cmd.Dir = n.root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	if err := startProc(cmd); err != nil {
		n.t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		n.t.Fatalf("%s: ctl %v timed out", n.impl, args)
	}
	code := cmd.ProcessState.ExitCode()
	n.cmds = append(n.cmds, cmdRec{Args: args, RC: code, Stdout: so.String(), Stderr: se.String()})
	return code
}

// sleeper starts a long-lived process whose pid stands in for a running daemon.
func (n *nrig) sleeper(pidfile string) {
	cmd := sleeperCmd(n.root, "ugreen-nas-fan")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := startProc(cmd); err != nil {
		n.t.Fatal(err)
	}
	n.cleanup = append(n.cleanup, cmd)
	go func() { _ = cmd.Wait() }()
	n.write(pidfile, strconv.Itoa(cmd.Process.Pid)+"\n")
}

func (n *nrig) stopAll() {
	files, _ := filepath.Glob(n.path("run/*.pid"))
	for _, f := range files {
		if data, err := os.ReadFile(f); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	}
	for _, c := range n.cleanup {
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
	}
	killUnder(n.root)
}

// snapshot: the transcript, the files of the boot folder and the state folder, the tool calls.
func (n *nrig) snapshot() golden {
	norm := (&rig{root: n.root}).normalize
	g := golden{Registers: map[string]string{}, State: map[string]string{}}
	for _, c := range n.cmds {
		g.Commands = append(g.Commands, cmdRec{Args: c.Args, RC: c.RC, Stdout: norm(c.Stdout), Stderr: norm(c.Stderr)})
	}
	for _, dir := range []string{"boot", "run"} {
		files, _ := filepath.Glob(n.path(dir + "/*"))
		for _, f := range files {
			st, err := os.Stat(f)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			data, _ := os.ReadFile(f)
			text := string(data)
			if strings.HasSuffix(f, ".pid") {
				text = "<PID>\n"
			}
			g.State[dir+"/"+filepath.Base(f)] = norm(text)
		}
	}
	g.Calls = lines(norm(n.read("calls.log")))
	g.Stubs = lines(norm(n.read("stubs.log")))
	return g
}

func nasScenarios() []nasScenario {
	cfgOn := func(n *nrig) { n.write("boot/fan.cfg", "fan_control=\"1\"\n") }
	flag := func(n *nrig) { n.write("boot/led.enabled", "") }
	return []nasScenario{
		{name: "usage", intended: "the usage lists the new report and event commands", run: func(n *nrig) { n.runs("", "bogus", "help", "-h") }},
		{name: "start-extra-args", run: func(n *nrig) { n.runs("start extra", "status", "stop extra") }},
		{name: "status-stopped", run: func(n *nrig) { n.runs("status") }},
		{name: "status-running", setup: func(n *nrig) { n.sleeper("run/fan.pid") }, run: func(n *nrig) { n.runs("status") }},
		{name: "status-stale", run: func(n *nrig) {
			n.write("run/fan.pid", "999999\n")
			n.runs("status")
			n.write("run/fan.pid", "abc\n")
			n.runs("status")
			n.write("run/fan.pid", "")
			n.runs("status")
		}},
		{name: "start-fan-off", run: func(n *nrig) {
			n.write("boot/fan.cfg", "fan_control=\"1\"\nfan_control=\"0\"\n")
			n.runs("start", "status", "restart")
			n.write("boot/fan.cfg", "fan_control=\"0\"\nfan_control=\"1\"\n")
			n.runs("start", "stop")
		}},
		{name: "start-stop", setup: cfgOn, run: func(n *nrig) {
			n.runs("status", "start", "status", "start", "restart", "status", "stop", "status", "stop")
		}},
		{name: "start-no-config", run: func(n *nrig) { n.runs("start", "stop") }},
		{name: "start-probe-fail-json", setup: func(n *nrig) {
			n.write("stubs/fan.probe.fail", "{\n  \"ok\": false,\n  \"error\": \"no supported fan controller\"\n}\n")
		}, run: func(n *nrig) { n.runs("start", "status") }},
		{name: "start-probe-fail-oneline", setup: func(n *nrig) {
			n.write("stubs/fan.probe.fail", "{\"ok\": false, \"error\": \"model \\\"X\\\" is not supported\"}")
		}, run: func(n *nrig) { n.runs("start") }},
		{name: "start-probe-fail-plain", setup: func(n *nrig) {
			n.write("stubs/fan.probe.fail", "permission denied\nsecond line\n")
		}, run: func(n *nrig) { n.runs("start") }},
		{name: "start-daemon-dies", setup: func(n *nrig) { n.write("stubs/fan.daemon.fail", "") }, run: func(n *nrig) { n.runs("start", "status") }},
		{name: "stop-stale", run: func(n *nrig) {
			n.write("run/fan.pid", "999999\n")
			n.runs("stop", "status")
		}},

		// LED status: model, blocker, warning, untested, import.
		{name: "led-status-models", run: func(n *nrig) {
			for _, m := range []string{"DXP6800 Pro", "DXP8800 Plus", "DX4600 Pro", "DX4700+", "DXP4800 GT", "DXP480T Plus", "DXP2800", "DXP9999 Future", "DX9 Mystery", "iDX6011 Pro", "iDX6012", "PRIME Z790-P", "UGREEN NAS", "DXP \"quoted\\\" Pro", ""} {
				n.model(m)
				n.runs("led-status")
			}
			n.remove("dmi/product_name")
			n.runs("led-status")
			n.write("dmi/product_name", "DXP6800 Pro\nsecond line\n")
			n.runs("led-status")
		}},
		{name: "led-status-blockers", run: func(n *nrig) {
			n.runs("led-status")
			n.write("boot/ugreen-led-control.plg", "")
			n.runs("led-status")
			n.mkdir("web/ugreen-led-control")
			n.remove("boot/ugreen-led-control.plg")
			n.runs("led-status")
			n.remove("web/ugreen-led-control")
			n.mkdir("web/ugreenleds-driver")
			n.runs("led-status")
			n.remove("web/ugreenleds-driver")
			n.write("boot/ugreenleds-driver.plg", "")
			n.runs("led-status")
			n.write("proc/modules", "other 1 0 - Live 0x0\nled_ugreen 16384 0 - Live 0x0\n")
			n.runs("led-status")
			n.remove("boot/ugreenleds-driver.plg")
			n.runs("led-status")
			n.write("proc/modules", "led_ugreen_other 1 0 - Live 0x0\n")
			n.runs("led-status")
			n.write("boot/ugreen-led-control.plg", "")
			n.write("proc/modules", "led_ugreen 16384 0 - Live 0x0\n")
			n.runs("led-status")
		}},
		{name: "led-status-import", run: func(n *nrig) {
			n.runs("led-status")
			n.write("old/leds.cfg", "")
			n.runs("led-status")
			n.write("old/leds.cfg", "brightness=\"128\"\n")
			n.runs("led-status")
			n.write("boot/leds.cfg", "brightness=\"128\"\n")
			n.runs("led-status")
			n.write("boot/leds.cfg", "brightness=\"10\"\n")
			n.runs("led-status")
			flag(n)
			n.runs("led-status")
		}},

		// Switch on, boot, import, off.
		{name: "led-enable-foreign-board", setup: func(n *nrig) { n.model("PRIME Z790-P") }, run: func(n *nrig) { n.runs("led-enable", "led-boot", "led-status") }},
		{name: "led-enable-blocked", run: func(n *nrig) {
			n.write("boot/ugreenleds-driver.plg", "")
			n.runs("led-enable")
			n.remove("boot/ugreenleds-driver.plg")
			n.write("proc/modules", "led_ugreen 1 0 - Live 0x0\n")
			n.runs("led-enable")
		}},
		{name: "led-enable", run: func(n *nrig) { n.runs("led-boot", "led-enable", "led-status", "led-enable") }},
		{name: "led-enable-takes-over", run: func(n *nrig) {
			n.write("old/leds.cfg", "mode=\"status\"\nfx_name=\"rainbow\"\n")
			n.runs("led-enable", "led-status")
			n.write("boot/leds.cfg", "mode=\"manual\"\n")
			n.runs("led-disable", "led-enable")
		}},
		{name: "led-enable-standalone", run: func(n *nrig) {
			n.write("boot/ugreen-led-control.plg", "")
			n.runs("led-enable", "led-status", "led-disable")
		}},
		{name: "led-enable-no-takeover-when-cfg", run: func(n *nrig) {
			n.write("old/leds.cfg", "brightness=\"1\"\n")
			n.write("boot/leds.cfg", "brightness=\"2\"\n")
			n.runs("led-enable")
		}},
		{name: "led-boot-off", run: func(n *nrig) { n.runs("led-boot") }},
		{name: "led-boot-conflict", setup: flag, run: func(n *nrig) {
			n.write("boot/ugreenleds-driver.plg", "")
			n.runs("led-boot")
			n.remove("boot/ugreenleds-driver.plg")
			n.write("proc/modules", "led_ugreen 1 0 - Live 0x0\n")
			n.runs("led-boot")
			n.model("PRIME Z790-P")
			n.runs("led-boot")
		}},
		{name: "led-boot-status-fx", setup: func(n *nrig) {
			flag(n)
			n.write("boot/leds.cfg", "mode=\"status\"\nfx_name=\"rainbow\"\n")
		}, run: func(n *nrig) { n.runs("led-boot") }},
		{name: "led-boot-fx-none", setup: func(n *nrig) {
			flag(n)
			n.write("boot/leds.cfg", "mode=\"manual\"\nfx_name=\"\"\n")
		}, run: func(n *nrig) { n.runs("led-boot") }},
		{name: "led-boot-fx-mode-lookalikes", setup: func(n *nrig) {
			flag(n)
			n.write("boot/leds.cfg", " mode=\"status\"\nmode='status'\nxmode=\"status\"\nfx_name=\"Rainbow\"\nfx_name=\"1\"\n")
		}, run: func(n *nrig) { n.runs("led-boot") }},
		{name: "led-boot-no-answer", setup: func(n *nrig) {
			flag(n)
			n.write("stubs/i2c.fail", "")
		}, run: func(n *nrig) { n.runs("led-boot") }},
		{name: "led-boot-tools-fail", setup: func(n *nrig) {
			flag(n)
			n.write("boot/leds.cfg", "mode=\"status\"\nfx_name=\"x\"\n")
			n.write("stubs/ctl.fail", "")
			n.write("stubs/update_cron.fail", "")
		}, run: func(n *nrig) { n.runs("led-boot") }},
		{name: "led-boot-cron-dir", setup: func(n *nrig) {
			flag(n)
			n.env["UGREEN_LED_CRON"] = n.path("boot/deep/er/x.cron")
		}, run: func(n *nrig) { n.runs("led-boot") }},
		{name: "led-import", run: func(n *nrig) {
			n.runs("led-import")
			n.write("old/leds.cfg", "brightness=\"128\"\nmode=\"status\"\n")
			n.runs("led-import")
			n.write("boot/leds.cfg", "brightness=\"10\"\n")
			n.runs("led-import")
			flag(n)
			n.write("boot/leds.cfg", "brightness=\"11\"\n")
			n.runs("led-import")
			n.remove("old/leds.cfg")
			n.runs("led-import", "led-status")
		}},
		{name: "led-disable", run: func(n *nrig) { n.runs("led-enable", "led-disable", "led-disable") }},
		{name: "led-stop", run: func(n *nrig) { n.runs("led-stop") }},
		{name: "led-stop-no-tools", run: func(n *nrig) {
			n.remove("led/ugreen-led-ctl")
			n.runs("led-stop", "led-disable")
		}},
		{name: "led-stop-legacy-empty", run: func(n *nrig) {
			n.write("run/legacy.pid", "")
			n.runs("led-stop")
		}},
		{name: "led-stop-legacy-bad-pid", intended: "the shell printed bash's own redirect error (\"/proc/N/cmdline: No such file or directory\") for a pid file naming a process that is gone; the Go port stays silent", run: func(n *nrig) {
			n.write("run/legacy.pid", "999999\n")
			n.runs("led-stop")
			n.write("run/legacy.pid", "abc\n")
			n.runs("led-stop")
		}},
	}
}

func TestNasCtl(t *testing.T) {
	if !enabled {
		t.Skip("parity needs bash >= 4 (nix develop): " + skipReason)
	}
	for _, s := range nasScenarios() {
		t.Run(s.name, func(t *testing.T) {
			t.Parallel()
			limiter <- struct{}{}
			defer func() { <-limiter }()
			name := "nasctl-" + s.name

			n := newNasRig(t, "go", "DXP6800 Pro")
			if s.setup != nil {
				s.setup(n)
			}
			s.run(n)
			got := n.snapshot().text()
			want := loadGolden(t, name).text()
			total.Add(1)
			if s.intended != "" {
				if want == got {
					different.Add(1)
					t.Errorf("marked as intended difference (%s) but Go agrees with the recorded shell: remove the mark", s.intended)
					return
				}
				intendedDiff.Add(1)
				t.Logf("intended difference: %s", s.intended)
				return
			}
			if want == got {
				equal.Add(1)
				return
			}
			different.Add(1)
			t.Errorf("Go differs from the recorded shell (- golden, + go):\n%s", unifiedDiff(want, got))
		})
	}
}
