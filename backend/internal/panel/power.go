package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
)

// The opt-in shutdown on a held power button (docs/panel.md#shut-down-on-a-held-power-button). fan.cfg keys:
// panel_power_shutdown="1" switches it on, panel_power_hold="<seconds>" sets the hold (1 to 3, default 1.5).
const (
	CfgPowerShutdown = "panel_power_shutdown"
	CfgPowerHold     = "panel_power_hold"
)

// PowerKeys are the settings this file reads (the diagnostics list them as known keys).
var PowerKeys = []string{CfgPowerShutdown, CfgPowerHold}

// The hold window. The vendor driver reports a power press on the first poll (ug_gpio_btn.c:88-95@c75c5ab), and boards
// force the power off at about 4 s of hold, so the clean shutdown has to come well before that.
const (
	PowerHoldLow     = 1 * time.Second
	PowerHoldHigh    = 3 * time.Second
	PowerHoldDefault = 1500 * time.Millisecond
	// PowerGrace: readings in the first minute after the start are ignored (boot glitches on the pads).
	PowerGrace = 60 * time.Second
)

// ShutdownCommand is what the Unraid web GUI runs for its Shutdown button (/usr/local/emhttp/plugins/dynamix/include/Boot.php:48).
var ShutdownCommand = []string{"/sbin/poweroff", "-n"}

const notifyScript = "/usr/local/emhttp/webGui/scripts/notify"

// PowerConfig is the shutdown setting as read from fan.cfg.
type PowerConfig struct {
	Enabled bool
	Hold    time.Duration
}

// ParsePowerConfig reads the two keys; a hold that is missing, not a number or outside 1 to 3 s is the default.
func ParsePowerConfig(v map[string]string) PowerConfig {
	c := PowerConfig{Enabled: v[CfgPowerShutdown] == "1", Hold: PowerHoldDefault}
	if s, err := strconv.ParseFloat(v[CfgPowerHold], 64); err == nil && !math.IsNaN(s) {
		if d := time.Duration(math.Round(s*1000)) * time.Millisecond; d >= PowerHoldLow && d <= PowerHoldHigh {
			c.Hold = d
		}
	}
	return c
}

// Polls is the hold in polls of Poll.
func (c PowerConfig) Polls() int { return int(math.Round(c.Hold.Seconds() / Poll.Seconds())) }

// powerWatch is one run of the watcher.
type powerWatch struct {
	spec     *ButtonSpec
	m        MemReader
	cfg      PowerConfig
	tick     <-chan time.Time
	now      func() time.Time
	out      io.Writer // dry-run lines
	logf     func(string, ...any)
	notify   func(subject, text string)
	shutdown func() error
	dry      bool
}

// run polls until the hold is met (then it acts once and returns), the context ends or the action fails.
//
// Guards: an unreadable read (0xFFFFFFFF, or a read error) is no action and restarts the hold; the first PowerGrace
// after the start is ignored, and so is a hold that began before it ended (the button has to be seen released first);
// the Tracker counts only polls in which the button was held, one after the other, and a release resets it; reset is
// never looked at; after the action the run ends, so nothing is queued.
func (w *powerWatch) run(ctx context.Context) error {
	start := w.now()
	tr := NewPowerTracker(w.cfg.Polls())
	armed, blind := false, false
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-w.tick:
		}
		st, err := w.spec.ReadState(w.m)
		if err != nil || st.Unreadable() || w.spec.Power == nil {
			tr.Poll(false)
			armed = false
			if !blind {
				w.logf("power button shutdown: the buttons cannot be read, no action until they can")
				blind = true
			}
			continue
		}
		if blind {
			w.logf("power button shutdown: the buttons can be read again")
			blind = false
		}
		if w.now().Sub(start) < PowerGrace {
			tr.Poll(false)
			continue
		}
		if !armed {
			if st.Power {
				continue
			}
			armed = true
		}
		for _, e := range tr.Poll(st.Power) {
			if e.Name == EvPress {
				return w.fire(float64(e.Polls) * Poll.Seconds())
			}
		}
	}
}

func (w *powerWatch) fire(held float64) error {
	if w.dry {
		say(w.out, "%s power held %.1f s", w.now().Format("15:04:05.000"), held)
		say(w.out, "would shut down now")
		return nil
	}
	w.logf("power button held %.1f s: shutting down Unraid (%s)", held, strings.Join(ShutdownCommand, " "))
	w.notify("Shutdown requested by front power button", fmt.Sprintf("The front power button was held for %.1f s. Unraid stops the array and shuts down.", held))
	if err := w.shutdown(); err != nil {
		w.logf("power button shutdown: %s failed: %v", ShutdownCommand[0], err)
		return err
	}
	return nil
}

func runPowerAction(ctx context.Context, env Env, args []string, stdout, stderr io.Writer) error {
	fs := flags("power-action", stderr)
	dry := fs.Bool("dry-run", false, "only print \"would shut down now\" when the hold is met")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 {
		return usageError("unexpected argument " + fs.Arg(0))
	}
	model := product(env)
	spec := ButtonsFor(model)
	if spec == nil || spec.Power == nil {
		return fmt.Errorf("no readable power button for model %q (known: %s)", model, buttonModels())
	}
	if !env.Confirmed(model) {
		return fmt.Errorf("the buttons of the %s are not confirmed by users yet (%s); the shutdown is not offered", model, env.Status(model))
	}
	cfg := ParsePowerConfig(env.Settings())
	if !*dry && !cfg.Enabled {
		say(stdout, "shutdown on a held power button is off (%s in fan.cfg); nothing to do", CfgPowerShutdown)
		return nil
	}
	m, err := env.OpenMem(false) // read-only
	if err != nil {
		return fmt.Errorf("open /dev/mem: %w (needs root)", err)
	}
	defer func() { _ = m.Close() }()
	tick, stop := env.Tick()
	defer stop()
	w := &powerWatch{spec: spec, m: m, cfg: cfg, tick: tick, now: env.Now, out: stdout, logf: env.Logf, notify: env.Notify, shutdown: env.Shutdown, dry: *dry}
	if *dry {
		say(stdout, "model: %s (%s)", model, env.Status(model))
		say(stdout, "dry run, nothing is shut down: a hold of %.1f s ends with the line below (the first %.0f s after the start are ignored)", cfg.Hold.Seconds(), PowerGrace.Seconds())
		w.logf = func(f string, a ...any) {
			say(stdout, "%s "+f, append([]any{env.Now().Format("15:04:05.000")}, a...)...)
		}
	} else {
		w.logf("power button shutdown on: hold %.1f s, first %.0f s after the start ignored", cfg.Hold.Seconds(), PowerGrace.Seconds())
	}
	return w.run(ctx)
}

// defaultNotify raises an Unraid notification and waits for the script (at most 10 s) before the shutdown starts.
func defaultNotify(subject, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, notifyScript, "-e", "UGREEN NAS Control", "-s", subject, "-d", text, "-i", "warning").Run()
}

func defaultShutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, ShutdownCommand[0], ShutdownCommand[1:]...).CombinedOutput(); err != nil {
		return errors.Join(err, errors.New(string(out)))
	}
	return nil
}

func defaultSettings() map[string]string { return fan.ReadValues(fan.ConfigPath()) }
