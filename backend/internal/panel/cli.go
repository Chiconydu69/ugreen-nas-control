package panel

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/syslog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// Env is what the commands touch; the zero value of a field means the real thing.
type Env struct {
	DMIDir         string
	ResetEnabled   string
	NetworkCfg     string
	NetworkRules   string
	NetworkBackups string
	Powerdown      string
	SafeModeFlag   string
	OpenPorts func() (hw.Ports, error)
	OpenMem   func(write bool) (Mem, error)
	// Tick returns the poll channel and its stop func; Wait sleeps until d passed or ctx ends; Now stamps events.
	Tick func() (<-chan time.Time, func())
	Wait func(ctx context.Context, d time.Duration)
	Now  func() time.Time
	// Lock runs fn under the flock shared with the fan daemon (default hw.WithLock); ACPIDir is where the kernel EC
	// driver binding is looked up; Conflicts lists rival fan writers (default: the real detection).
	Lock      func(fn func() error) error
	ACPIDir   string
	Conflicts func() []conflict.Conflict
	// Status says how well the panel is known on a model (the model registry, nasctl.PanelStatus); default: not confirmed.
	Status func(product string) string
	// Confirmed says whether users confirmed the buttons on a model (registry panel check community or verified); default: no.
	// Settings reads fan.cfg; Logf writes to the syslog; Notify raises an Unraid notification; Shutdown is the clean power-off.
	Confirmed func(product string) bool
	Settings  func() map[string]string
	Logf      func(format string, a ...any)
	Notify    func(subject, text string)
	Shutdown  func() error
}

// Unconfirmed is the status of a model nobody confirmed the panel on.
const Unconfirmed = "vendor source, not yet confirmed"

func (e Env) withDefaults() Env {
	if e.DMIDir == "" {
		e.DMIDir = hw.DMIDir
	}
	const boot = "/boot/config/plugins/ugreen-nas-control"
	if e.ResetEnabled == "" {
		e.ResetEnabled = boot + "/network-reset.enabled"
	}
	if e.NetworkCfg == "" {
		e.NetworkCfg = "/boot/config/network.cfg"
	}
	if e.NetworkRules == "" {
		e.NetworkRules = "/boot/config/network-rules.cfg"
	}
	if e.NetworkBackups == "" {
		e.NetworkBackups = boot + "/network-backups"
	}
	if e.Powerdown == "" {
		e.Powerdown = "/usr/local/sbin/powerdown"
	}
	if e.SafeModeFlag == "" {
		e.SafeModeFlag = "/boot/unraidsafemode"
	}
	if e.OpenPorts == nil {
		e.OpenPorts = hw.OpenPorts
	}
	if e.OpenMem == nil {
		e.OpenMem = OpenMem
	}
	if e.Tick == nil {
		e.Tick = func() (<-chan time.Time, func()) {
			t := time.NewTicker(Poll)
			return t.C, t.Stop
		}
	}
	if e.Wait == nil {
		e.Wait = func(ctx context.Context, d time.Duration) {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
			case <-t.C:
			}
		}
	}
	if e.Now == nil {
		e.Now = time.Now
	}
	if e.Status == nil {
		e.Status = func(string) string { return Unconfirmed }
	}
	if e.Confirmed == nil {
		e.Confirmed = func(string) bool { return false }
	}
	if e.Settings == nil {
		e.Settings = defaultSettings
	}
	if e.Logf == nil {
		e.Logf = syslogf()
	}
	if e.Notify == nil {
		e.Notify = defaultNotify
	}
	if e.Shutdown == nil {
		e.Shutdown = defaultShutdown
	}
	return e
}

const usage = `Usage: ugreen-nas panel <command>
  buttons [--watch]            read the power and reset button (read only); --watch prints presses until Ctrl-C
  beep [--freq Hz] [--ms N]    sound the beeper for N ms (default 200, at most 2000); --freq only on PC speaker models
  power-action [--dry-run]       the opt-in shutdown on a held power button (runs until it acts); --dry-run only prints "would shut down now"
Needs root. The facts are UGREEN's driver source; which model users confirmed is in docs/panel.md.`

// Main runs `ugreen-nas panel ...` and returns the exit code. ctx ends on SIGINT/SIGTERM.
func Main(ctx context.Context, env Env, args []string, stdout, stderr io.Writer) int {
	env = env.withDefaults()
	if len(args) == 0 {
		say(stderr, "%s", usage)
		return 2
	}
	var err error
	switch args[0] {
	case "buttons":
		err = runButtons(ctx, env, args[1:], stdout, stderr)
	case "beep":
		err = runBeep(ctx, env, args[1:], stderr)
	case "reset-network-watch":
		err = runResetNetworkWatch(ctx, env, args[1:], stdout)
	case "power-action":
		err = runPowerAction(ctx, env, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		say(stdout, "%s", usage)
		return 0
	default:
		say(stderr, "%s", usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		say(stderr, "ERROR: %v", err)
		if errors.As(err, new(usageError)) {
			return 2
		}
		return 1
	}
	return 0
}

type usageError string

func (u usageError) Error() string { return string(u) }

func flags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("panel "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func product(env Env) string { return hw.ProductName(env.DMIDir) }

func runButtons(ctx context.Context, env Env, args []string, stdout, stderr io.Writer) error {
	fs := flags("buttons", stderr)
	watch := fs.Bool("watch", false, "print press and release events until interrupted")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 {
		return usageError("unexpected argument " + fs.Arg(0))
	}
	model := product(env)
	spec := ButtonsFor(model)
	if spec == nil {
		return fmt.Errorf("no button map for model %q (known: %s)", model, buttonModels())
	}
	m, err := env.OpenMem(false) // read-only
	if err != nil {
		return fmt.Errorf("open /dev/mem: %w (needs root; a kernel with STRICT_DEVMEM may refuse the pad registers)", err)
	}
	defer func() { _ = m.Close() }()
	say(stdout, "model: %s (%s)", model, env.Status(model))
	if spec.Power == nil {
		say(stdout, "power: not readable by address")
	}
	if !*watch {
		st, err := spec.ReadState(m)
		if err != nil {
			return err
		}
		if st.PowerUnreadable {
			say(stdout, "power: %s", unreadableNote)
		}
		if st.ResetUnreadable {
			say(stdout, "reset: %s", unreadableNote)
		}
		say(stdout, "%s", describe(st))
		return nil
	}
	tick, stop := env.Tick()
	defer stop()
	return spec.Watch(ctx, m, stdout, tick, env.Now)
}

func runBeep(ctx context.Context, env Env, args []string, stderr io.Writer) error {
	fs := flags("beep", stderr)
	freq := fs.Int("freq", 0, fmt.Sprintf("tone in Hz, %d-%d (PC speaker models only; default about 503)", MinHz, MaxHz))
	ms := fs.Int("ms", DefaultMs, fmt.Sprintf("duration in ms, %d-%d", MinMs, MaxMs))
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 {
		return usageError("unexpected argument " + fs.Arg(0))
	}
	if *ms < MinMs || *ms > MaxMs {
		return usageError(fmt.Sprintf("--ms must be %d-%d", MinMs, MaxMs))
	}
	if *freq != 0 && (*freq < MinHz || *freq > MaxHz) {
		return usageError(fmt.Sprintf("--freq must be %d-%d", MinHz, MaxHz))
	}
	model := product(env)
	spec := BeeperFor(model)
	if spec == nil {
		return fmt.Errorf("no beeper known for model %q (known: %s)", model, beeperModels())
	}
	if *freq != 0 && spec.Kind != PCSpeaker {
		return usageError(fmt.Sprintf("the %s beeper has a fixed tone; --freq is for PC speaker models", spec.Kind))
	}
	b, closeFn, err := spec.open(openers{ports: env.OpenPorts, mem: env.OpenMem, lock: env.Lock, acpiDir: env.ACPIDir, conflicts: env.Conflicts})
	if err != nil {
		return fmt.Errorf("open the %s: %w (needs root)", spec.Kind, err)
	}
	defer closeFn()
	return Sound(ctx, b, *freq, *ms, env.Wait)
}

const unreadableNote = "not readable on this box (GPIO registers read as 0xFF; the BIOS hides them)"
// runResetNetworkWatch performs a one-shot network reset only after a continuous 13-second Reset hold and release.
// It is restricted to the user-confirmed DXP8800 Plus and remains off unless explicitly enabled.
func runResetNetworkWatch(ctx context.Context, env Env, args []string, out io.Writer) error {
	if len(args) != 0 {
		return usageError("unexpected argument " + args[0])
	}
	model := product(env)
	// The registry marks this panel partially confirmed because its power path is doubtful; the Reset pad itself
	// was observed by the user and is the only input used by this separate network-reset action.
	if model != "DXP8800 Plus" {
		return fmt.Errorf("network reset is only available on the user-confirmed DXP8800 Plus")
	}
	if _, err := os.Stat(env.ResetEnabled); err != nil {
		return fmt.Errorf("network reset is disabled")
	}
	spec := ButtonsFor(model)
	if spec == nil || spec.Reset == nil {
		return fmt.Errorf("reset button is not mapped")
	}
	m, err := env.OpenMem(false)
	if err != nil {
		return fmt.Errorf("open /dev/mem: %w", err)
	}
	defer func() { _ = m.Close() }()
	state, err := spec.ReadState(m)
	if err != nil {
		return err
	}
	if state.ResetUnreadable {
		return ErrUnreadable
	}
	if state.Reset {
		say(out, "Reset held at monitor start; waiting for release before arming")
		for state.Reset {
			if err := waitResetPoll(ctx, env); err != nil {
				return err
			}
			state, err = spec.ReadState(m)
			if err != nil {
				return err
			}
			if state.ResetUnreadable {
				return ErrUnreadable
			}
		}
	}
	say(out, "Network reset armed: hold Reset for 13 seconds, then release")
	tick, stop := env.Tick()
	defer stop()
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	triggered := false
	return spec.WatchEvents(watchCtx, m, out, tick, env.Now, func(button, event string, polls int) {
		if button != "reset" || event != EvRelease || polls < resetPressAfter || triggered {
			return
		}
		triggered = true
		if err := armSafeMode(env.SafeModeFlag); err != nil {
			say(out, "ERROR: network reset was not performed: cannot request one-time Safe Mode boot: %v", err)
			cancel()
			return
		}
		backupDir, moved, err := resetNetworkFiles(env)
		if err != nil {
			_ = os.Remove(env.SafeModeFlag)
			say(out, "ERROR: network reset was not performed: %v", err)
			cancel()
			return
		}
		// Disarm before reboot so a failed restart cannot trigger a second reset on the next boot.
		if err := os.Remove(env.ResetEnabled); err != nil && !os.IsNotExist(err) {
			restoreNetworkFiles(backupDir, moved, filepath.Dir(env.NetworkCfg))
			_ = os.Remove(env.SafeModeFlag)
			say(out, "ERROR: network reset was not performed: cannot disarm the one-shot setting: %v", err)
			cancel()
			return
		}
		say(out, "Saved %s in %s; requesting a one-time Safe Mode boot", strings.Join(moved, " and "), backupDir)
		cmd := exec.Command(env.Powerdown, "-r")
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			say(out, "ERROR: orderly reboot failed: %v; restoring network files", err)
			restoreNetworkFiles(backupDir, moved, filepath.Dir(env.NetworkCfg))
			_ = os.Remove(env.SafeModeFlag)
		}
		cancel()
	})
}

func waitResetPoll(ctx context.Context, env Env) error {
	ch, stop := env.Tick()
	defer stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}

// armSafeMode preserves an existing request and reports failure before moving network configuration.
func armSafeMode(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return err
	}
	return f.Close()
}

func resetNetworkFiles(env Env) (string, []string, error) {
	if st, err := os.Stat(env.NetworkCfg); err != nil {
		return "", nil, fmt.Errorf("network.cfg is unavailable: %w", err)
	} else if !st.Mode().IsRegular() {
		return "", nil, fmt.Errorf("network.cfg is not a regular file")
	}
	stamp := time.Now().Format("20060102-150405.000000000")
	dir := filepath.Join(env.NetworkBackups, stamp)
	if err := os.MkdirAll(env.NetworkBackups, 0o700); err != nil {
		return "", nil, err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", nil, err
	}
	var sources []string
	for _, src := range []string{env.NetworkCfg, env.NetworkRules} {
		st, err := os.Stat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		if !st.Mode().IsRegular() {
			return "", nil, fmt.Errorf("%s is not a regular file", filepath.Base(src))
		}
		sources = append(sources, src)
	}
	var moved []string
	for _, src := range sources {
		name := filepath.Base(src)
		if err := os.Rename(src, filepath.Join(dir, name)); err != nil {
			restoreNetworkFiles(dir, moved, filepath.Dir(src))
			_ = os.Remove(dir)
			return "", nil, err
		}
		moved = append(moved, name)
	}
	if len(moved) == 0 {
		_ = os.Remove(dir)
		return "", nil, fmt.Errorf("no Unraid network configuration files found")
	}
	return dir, moved, nil
}

func restoreNetworkFiles(backupDir string, names []string, targetDir string) {
	for i := len(names) - 1; i >= 0; i-- {
		_ = os.Rename(filepath.Join(backupDir, names[i]), filepath.Join(targetDir, names[i]))
	}
}

func buttonModels() string {
	var n []string
	for _, s := range ButtonSpecs {
		n = append(n, s.Name)
	}
	return strings.Join(n, "; ")
}

func beeperModels() string {
	var n []string
	for _, s := range BeeperSpecs {
		n = append(n, s.Name)
	}
	return strings.Join(n, "; ")
}

// say prints one line; a failed write to the terminal is not worth an error.
func say(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format+"\n", a...) }

// syslogf logs to the syslog under the tag of the plugin's daemons, to stderr when there is none.
func syslogf() func(string, ...any) {
	logger, _ := syslog.New(syslog.LOG_INFO|syslog.LOG_DAEMON, "ugreen-nas-control")
	return func(format string, a ...any) {
		msg := fmt.Sprintf(format, a...)
		if logger != nil {
			_ = logger.Info(msg)
			return
		}
		_, _ = fmt.Fprintln(os.Stderr, msg)
	}
}
