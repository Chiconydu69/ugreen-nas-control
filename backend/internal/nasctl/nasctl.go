// Package nasctl is ugreen-nas-ctl: starts and stops the fan daemon, switches the LED part on and off, and
// answers the plugin's web page (led-status).
package nasctl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fsx"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/proc"
)

const usageText = `Usage:
  ugreen-nas-ctl start|stop|restart|status   fan daemon (also runs in mode "bios",
                                             where it only watches) and, if switched on, the MQTT publisher
  ugreen-nas-ctl mqtt-start|mqtt-stop|mqtt-status
                                             the MQTT publisher alone (mqtt_enabled="1" in fan.cfg)
  ugreen-nas-ctl led-status                  LED part as JSON: enabled, supported, conflict
  ugreen-nas-ctl led-enable|led-disable      switch the LED part on or off
  ugreen-nas-ctl led-import                  take over the settings of UGREEN LED Control
  ugreen-nas-ctl led-boot                    bring the LEDs up (install, boot), if enabled
  ugreen-nas-ctl report                      anonymised JSON for a model report
  ugreen-nas-ctl diag                        anonymised read-only diagnostics as Markdown for a bug report
                                             (no hardware writes; the only system change is modprobe i2c-dev, if missing)
  ugreen-nas-ctl event stopping              array stop hook: signals shutdown on the power LED
`

type Ctl struct {
	Env Env
	Out io.Writer
	Err io.Writer

	// Replaced in tests.
	Sleep  func(time.Duration)
	Detect func() []conflict.Conflict

	found   []conflict.Conflict
	scanned bool
}

// rivalList runs the detector once per invocation.
func (c *Ctl) rivalList() []conflict.Conflict {
	if !c.scanned {
		c.found, c.scanned = c.Detect(), true
	}
	return c.found
}

func New(env Env, out, errw io.Writer) *Ctl {
	return &Ctl{Env: env, Out: out, Err: errw, Sleep: time.Sleep, Detect: func() []conflict.Conflict { return rivals(env) }}
}

func rivals(env Env) []conflict.Conflict {
	r := conflict.Default()
	r.PluginsBoot, r.PluginsWeb, r.ProcModules = env.PluginsBoot, env.PluginsWeb, env.ProcModules
	return r.Detect()
}

func (c *Ctl) out(format string, a ...any)  { _, _ = fmt.Fprintf(c.Out, format, a...) }
func (c *Ctl) errf(format string, a ...any) { _, _ = fmt.Fprintf(c.Err, format, a...) }

func (c *Ctl) Main(args []string) int {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "start":
		return c.start()
	case "stop":
		c.stop()
		return 0
	case "restart":
		o := c.Out
		c.Out = io.Discard
		c.stop()
		c.Out = o
		return c.start()
	case "mqtt-start":
		return c.pubStart()
	case "mqtt-stop":
		c.pubStop()
		c.out("MQTT publisher stopped.\n")
		return 0
	case "mqtt-status":
		if c.running(c.Env.PubPidFile) {
			c.out("running (pid %s)\n", c.pidText(c.Env.PubPidFile))
			return 0
		}
		c.out("stopped\n")
		return 3
	case "status":
		if c.running(c.Env.PidFile) {
			c.out("running (pid %s)\n", c.pidText(c.Env.PidFile))
			return 0
		}
		c.out("stopped\n")
		return 3
	case "led-status":
		c.ledStatus()
		return 0
	case "led-enable":
		return c.ledEnable()
	case "led-import":
		return c.ledImport()
	case "led-disable":
		c.ledDisable()
		return 0
	case "led-boot":
		return c.ledBoot()
	case "led-stop":
		c.ledStop()
		return 0
	case "report":
		return c.report()
	case "diag":
		return c.diag()
	case "event":
		if len(args) > 1 && args[1] == "stopping" {
			c.eventStopping()
			return 0
		}
	}
	c.out("%s", usageText)
	return 2
}

// pidText is the pid file's content without trailing newlines ($(cat file)).
func (c *Ctl) pidText(pidFile string) string {
	data, _ := os.ReadFile(pidFile)
	return strings.TrimRight(string(data), "\n")
}

// alive: the pid is a live process of this plugin. A stale pidfile whose pid was reused by another process says no.
func (c *Ctl) alive(pidText string) bool {
	pid, err := strconv.Atoi(pidText)
	return err == nil && pid > 0 && syscall.Kill(pid, 0) == nil && proc.Ours(c.Env.Proc, pid)
}

func (c *Ctl) running(pidFile string) bool {
	st, err := os.Stat(pidFile)
	return err == nil && st.Mode().IsRegular() && c.alive(c.pidText(pidFile))
}

func isFile(p string) bool {
	st, err := os.Stat(p) //nolint:gosec // p is a fixed plugin path or a test override from the environment
	return err == nil && st.Mode().IsRegular()
}

func isDir(p string) bool {
	st, err := os.Stat(p) //nolint:gosec // same
	return err == nil && st.IsDir()
}

func nonEmpty(p string) bool {
	st, err := os.Stat(p) //nolint:gosec // same
	return err == nil && st.Size() > 0
}

func executable(p string) bool {
	st, err := os.Stat(p) //nolint:gosec // same
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// run starts a tool and waits; nil writers discard. A tool that cannot be started counts as failed.
func run(path string, stdout, stderr io.Writer, args ...string) bool {
	cmd := exec.Command(path, args...)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run() == nil
}

// detach starts a process in its own session with output to /dev/null (stderr to a file when given).
func detach(path string, stderrFile string, args ...string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if stderrFile != "" {
		if f, err := fsx.OpenTrunc(stderrFile); err == nil {
			defer func() { _ = f.Close() }()
			cmd.Stderr = f
		}
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	// Reap it when it ends, so a daemon that dies at once is not a zombie that still answers kill -0.
	go func() { _ = cmd.Wait() }()
	return cmd.Process.Pid, nil
}

var fanControlRe = regexp.MustCompile(`^fan_control="(.*)"$`)

// fanOff: fan_control="0" in fan.cfg. This plugin must not touch the fan chip then (another plugin or the BIOS runs the fans).
func (c *Ctl) fanOff() bool {
	data, err := os.ReadFile(c.Env.FanCfg)
	if err != nil {
		return false
	}
	last := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := fanControlRe.FindStringSubmatch(line); m != nil {
			last = m[1]
		}
	}
	return last == "0"
}

var errorRe = regexp.MustCompile(`^.*"error": "(.*)".*$`)

// probeError is what the fan tool says went wrong: its JSON "error" field, else its whole output.
func probeError(probe string) string {
	var found []string
	for _, line := range strings.Split(probe, "\n") {
		if m := errorRe.FindStringSubmatch(line); m != nil && m[1] != "" {
			found = append(found, m[1])
		}
	}
	if len(found) > 0 {
		return strings.Join(found, "\n")
	}
	return probe
}

// start: the publisher's failure must not stop the fans from starting, nor the other way round.
func (c *Ctl) start() int {
	rc := c.fanStart()
	c.pubStart()
	return rc
}

func (c *Ctl) fanStart() int {
	if c.fanOff() {
		c.out("Fan control is switched off in the settings.\n")
		return 0
	}
	if c.running(c.Env.PidFile) {
		c.out("Fan daemon is already running.\n")
		return 0
	}

	var probe bytes.Buffer
	if !run(c.Env.FanTool, &probe, &probe, "probe") {
		text := strings.TrimRight(probe.String(), "\n")
		c.errf("ERROR: %s\n", strings.TrimRight(probeError(text), "\n"))
		return 1
	}

	pid, err := detach(c.Env.FanTool, c.Env.ErrLog, "daemon")
	if err != nil {
		c.errf("ERROR: Fan daemon did not start: %v\n", err)
		return 1
	}
	_ = fsx.WriteFile(c.Env.PidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644)

	// A daemon that cannot start (no BIOS snapshot, no ports) ends at once.
	c.Sleep(500 * time.Millisecond)
	if !c.running(c.Env.PidFile) {
		_ = os.Remove(c.Env.PidFile)
		first := ""
		if data, err := os.ReadFile(c.Env.ErrLog); err == nil {
			first, _, _ = strings.Cut(string(data), "\n")
		}
		c.errf("ERROR: Fan daemon did not start: %s\n", first)
		return 1
	}

	c.out("Fan daemon started.\n")
	return 0
}

func (c *Ctl) stop() {
	c.pubStop()
	c.fanStop()
}

// fanStop waits for the daemon: it writes the BIOS fan settings back on SIGTERM.
func (c *Ctl) fanStop() {
	c.terminate(c.Env.PidFile, 50)
	c.out("Fan daemon stopped.\n")
}

// terminate sends SIGTERM to the process of a pid file and waits up to n tenths of a second for it to end. A process
// that is still there then gets SIGKILL; the file only goes once the process is gone.
func (c *Ctl) terminate(pidFile string, n int) {
	if c.running(pidFile) {
		pid, _ := strconv.Atoi(c.pidText(pidFile))
		_ = syscall.Kill(pid, syscall.SIGTERM)
		if !c.waitGone(pid, n) && proc.Ours(c.Env.Proc, pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			if !c.waitGone(pid, 20) {
				return
			}
		}
	}
	_ = os.Remove(pidFile)
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

var mqttEnabledRe = regexp.MustCompile(`^mqtt_enabled="(.*)"$`)

// pubEnabled: mqtt_enabled="1" in fan.cfg (the last such line wins).
func (c *Ctl) pubEnabled() bool {
	data, err := os.ReadFile(c.Env.FanCfg)
	if err != nil {
		return false
	}
	last := ""
	for _, line := range strings.Split(string(data), "\n") {
		if m := mqttEnabledRe.FindStringSubmatch(line); m != nil {
			last = m[1]
		}
	}
	return last == "1"
}

// pubStart runs `ugreen-nas-fan publish` apart from the fan daemon, so it also runs where the fans are not controlled.
// Without the setting it does nothing.
func (c *Ctl) pubStart() int {
	if !c.pubEnabled() || c.running(c.Env.PubPidFile) {
		return 0
	}
	pid, err := detach(c.Env.FanTool, c.Env.PubLog, "publish")
	if err != nil {
		c.errf("ERROR: MQTT publisher did not start: %v\n", err)
		return 1
	}
	_ = fsx.WriteFile(c.Env.PubPidFile, []byte(strconv.Itoa(pid)+"\n"), 0o644)

	// It ends at once without a host in the settings.
	c.Sleep(500 * time.Millisecond)
	if !c.running(c.Env.PubPidFile) {
		_ = os.Remove(c.Env.PubPidFile)
		last := ""
		if data, err := os.ReadFile(c.Env.PubLog); err == nil {
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			last = lines[len(lines)-1]
		}
		c.errf("ERROR: MQTT publisher did not start: %s\n", last)
		return 1
	}

	c.out("MQTT publisher started.\n")
	return 0
}

func (c *Ctl) pubStop() { c.terminate(c.Env.PubPidFile, 30) }

// ledModel is the first line of the DMI product name.
func (c *Ctl) ledModel() string {
	data, err := os.ReadFile(filepath.Join(c.Env.DMIDir, "product_name"))
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return line
}

func hasPrefixAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// ledModelOK: UGREEN's own models, as UGREEN LED Control knew them; the controller is the same across DX/DXP/iDX.
func ledModelOK(model string) bool {
	if hasPrefixAny(model, "DXP", "iDX") {
		return true
	}
	return len(model) > 2 && strings.HasPrefix(model, "DX") && model[2] >= '0' && model[2] <= '9'
}

// ledUntested: models UGREEN LED Control has verified or had confirmed by users; every other model works after the
// same protocol but with a guessed bay order. The DXP2800 GT has its LEDs in the CS201x, not on the LED MCU.
func ledUntested(model string) bool {
	return model == "DXP2800 GT" || !hasPrefixAny(model, "DXP6800", "DXP8800", "DXP4800", "DX4600", "DX4700", "DXP2800", "DXP480T")
}

const standaloneID = "ugreen-led-control"

// ledBlocker says why the LEDs cannot be used here: model, driver, module; empty if nothing is in the way. The
// rival detector decides about the other LED tools; the installed UGREEN LED Control is only a warning.
func (c *Ctl) ledBlocker() string {
	if !ledModelOK(c.ledModel()) {
		return "model"
	}
	module := false
	for _, f := range c.rivalList() {
		if !controls(f, conflict.LED) {
			continue
		}
		switch {
		case f.Kind == conflict.KindModule:
			module = true
		case f.Kind == conflict.KindPlugin && f.ID == standaloneID:
		case f.Kind == conflict.KindDaemon:
			return "rival"
		case f.Kind == conflict.KindPlugin:
			return "driver"
		}
	}
	if module {
		return "module"
	}
	return ""
}

func controls(f conflict.Conflict, what string) bool {
	for _, c := range f.Controls {
		if c == what {
			return true
		}
	}
	return false
}

// ledWarning: the installed plugin UGREEN LED Control is no obstacle: both run on their own and use the same LED
// tool with a shared bus lock. Only the last one to write wins, which the page points out.
func (c *Ctl) ledWarning() string {
	for _, f := range c.rivalList() {
		if f.Kind == conflict.KindPlugin && f.ID == standaloneID {
			return "standalone"
		}
	}
	return ""
}

func (c *Ctl) ledEnabled() bool { return isFile(c.Env.LEDFlag) }

// ledImportAvailable: the settings of an installed UGREEN LED Control that this plugin does not have yet (or has differently).
func (c *Ctl) ledImportAvailable() bool {
	if !nonEmpty(c.Env.LEDImportSrc) {
		return false
	}
	src, _ := os.ReadFile(c.Env.LEDImportSrc)
	cfg, err := os.ReadFile(c.Env.LEDCfg)
	return err != nil || !bytes.Equal(src, cfg)
}

func (c *Ctl) ledStatus() {
	blocker := c.ledBlocker()
	model := c.ledModel()
	model = strings.Map(func(r rune) rune {
		if r == '"' || r == '\\' {
			return -1
		}
		return r
	}, model)
	c.out(`{"enabled": %t, "supported": %t, "blocker": "%s", "warning": "%s", "untested": %t, "import_available": %t, "model": "%s"}`+"\n",
		c.ledEnabled(), blocker != "model", blocker, c.ledWarning(), ledUntested(c.ledModel()), c.ledImportAvailable(), model)
}

// ledStopLegacy: earlier versions kept the daemon's pid in the standalone plugin's files. Such a process is ours
// only if it runs from our own led/ directory; the standalone plugin's is left alone.
func (c *Ctl) ledStopLegacy() {
	for _, pidfile := range c.Env.LegacyPids {
		data, _ := os.ReadFile(pidfile)
		text := strings.TrimRight(string(data), "\n")
		if text == "" {
			continue
		}
		pid, err := strconv.Atoi(text)
		if err != nil || pid <= 0 {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(c.Env.Proc, strconv.Itoa(pid), "cmdline"))
		if err != nil || !strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), c.Env.LEDDir+"/") {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
		_ = os.Remove(pidfile)
	}
}

// ledStop ends this plugin's own LED daemon and effect (their pidfiles are named after this plugin); nothing
// happens when they are not running.
func (c *Ctl) ledStop() {
	c.ledStopLegacy()
	if executable(c.Env.LEDCtl) {
		run(c.Env.LEDCtl, nil, nil, "fx", "stop")
		run(c.Env.LEDCtl, nil, nil, "daemon", "stop")
	}
}

var (
	modeStatusRe = regexp.MustCompile(`(?m)^mode="status"$`)
	fxNameRe     = regexp.MustCompile(`(?m)^fx_name="[a-z]`)
)

// ledBoot does what UGREEN LED Control does at install and boot: drivers, saved settings, status daemon, night
// mode, effect. It runs only with the switch on and nothing in the way.
func (c *Ctl) ledBoot() int {
	if !c.ledEnabled() {
		return 0
	}
	if blocker := c.ledBlocker(); blocker != "" {
		c.out("LEDs not started: %s\n", blocker)
		return 1
	}

	run("modprobe", c.Out, nil, "i2c_i801")
	run("modprobe", c.Out, nil, "i2c-dev")

	ctl := c.Env.LEDCtl
	if strings.HasPrefix(c.ledModel(), "DXP480T") {
		// No controller at 0x3a: the power LED sits on its own chip, which stays in its flashing power-on state until
		// it gets the steady-white init.
		if !run(ctl, c.Out, c.Err, "dxp480t") {
			c.out("WARNING: The power LED did not accept the steady-white init.\n")
		}
	} else if run(c.Env.LEDI2C, nil, nil, "probe") {
		run(ctl, c.Out, c.Err, "apply")

		cfg, _ := os.ReadFile(c.Env.LEDCfg)
		// Restart also replaces a daemon still running the previous version's code.
		if modeStatusRe.Match(cfg) {
			run(ctl, c.Out, c.Out, "daemon", "restart")
		}

		// Night mode right away; afterwards cron checks it every minute.
		run(ctl, c.Out, c.Err, "schedule", "--force")

		if fxNameRe.Match(cfg) {
			run(ctl, c.Out, c.Out, "fx", "start")
		}
	} else {
		c.out("WARNING: The LED controller does not answer on the SMBus; no LEDs available.\n")
		run(ctl, c.Out, c.Out, "smbus")
	}

	_ = os.MkdirAll(filepath.Dir(c.Env.LEDCron), 0o755)
	_ = fsx.WriteFile(c.Env.LEDCron, []byte("* * * * * "+ctl+" schedule >/dev/null 2>&1\n"), 0o644)
	run(c.Env.UpdateCron, c.Out, nil)
	return 0
}

func copyFile(from, to string) {
	if data, err := os.ReadFile(from); err == nil {
		_ = fsx.WriteFile(to, data, 0o644)
	}
}

func (c *Ctl) ledEnable() int {
	if blocker := c.ledBlocker(); blocker != "" {
		c.errf("ERROR: The LEDs cannot be switched on: %s\n", blocker)
		return 1
	}

	_ = os.MkdirAll(filepath.Dir(c.Env.LEDFlag), 0o755)
	_ = fsx.WriteFile(c.Env.LEDFlag, nil, 0o644)

	// First use with an installed UGREEN LED Control: start with its settings instead of the defaults.
	if _, err := os.Stat(c.Env.LEDCfg); err != nil && nonEmpty(c.Env.LEDImportSrc) {
		copyFile(c.Env.LEDImportSrc, c.Env.LEDCfg)
		c.out("Took over the settings of UGREEN LED Control.\n")
	}
	return c.ledBoot()
}

// ledImport takes over the settings of UGREEN LED Control (colours, effects, status mode, night mode, ...). What
// this plugin had is kept as leds.cfg.bak.
func (c *Ctl) ledImport() int {
	if !nonEmpty(c.Env.LEDImportSrc) {
		c.errf("ERROR: No settings of UGREEN LED Control found.\n")
		return 1
	}

	_ = os.MkdirAll(filepath.Dir(c.Env.LEDCfg), 0o755)
	if _, err := os.Stat(c.Env.LEDCfg); err == nil {
		copyFile(c.Env.LEDCfg, c.Env.LEDCfg+".bak")
	}
	copyFile(c.Env.LEDImportSrc, c.Env.LEDCfg)

	// Put the new settings on the LEDs, if this part is running.
	if c.ledEnabled() {
		c.ledBoot()
	}
	c.out("Took over the settings of UGREEN LED Control.\n")
	return 0
}

func (c *Ctl) ledDisable() {
	c.ledStop()
	_ = os.Remove(c.Env.LEDFlag)
	_ = os.Remove(c.Env.LEDCron)
	run(c.Env.UpdateCron, c.Out, nil)
	c.out("LEDs switched off; they keep their last state until the next power cycle.\n")
}

// eventStopping is emhttp's hook at the start of every array stop, also during shutdown and reboot. The LED tool
// decides whether the system goes down and lets the power LED signal it; emhttp waits for the hook, so the work
// runs in the background.
func (c *Ctl) eventStopping() {
	if !c.ledEnabled() {
		return
	}
	_, _ = detach(c.Env.LEDCtl, "", "shutdown")
}
