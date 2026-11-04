package panel

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// Env is what the commands touch; the zero value of a field means the real thing.
type Env struct {
	DMIDir    string
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
}

func (e Env) withDefaults() Env {
	if e.DMIDir == "" {
		e.DMIDir = hw.DMIDir
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
	return e
}

const usage = `Usage: ugreen-nas panel <command>
  buttons [--watch]            read the power and reset button (read only); --watch prints presses until Ctrl-C
  beep [--freq Hz] [--ms N]    sound the beeper for N ms (default 200, at most 2000); --freq only on PC speaker models
Needs root. Vendor source for every model, not yet confirmed on hardware (docs/panel.md).`

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
	say(stdout, "model: %s (vendor source, not yet confirmed on hardware)", model)
	if spec.Power == nil {
		say(stdout, "power: not readable by address")
	}
	if !*watch {
		st, err := spec.ReadState(m)
		if err != nil {
			return err
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
