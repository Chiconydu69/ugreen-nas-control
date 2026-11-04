package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/chip"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/led/ledctl"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/mqtt"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/nasctl"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/panel"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

// runFan: the fan tool reads os.Args itself and exits on its own. `publish` is the MQTT publisher (own package, which
// uses the fan package, so the fan tool cannot start it itself).
func runFan(args []string) int {
	if len(args) > 0 && args[0] == "publish" {
		if err := mqtt.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			return 1
		}
		return 0
	}
	os.Args = append([]string{"ugreen-nas-fan"}, args...)
	fan.Main()
	return 0
}

func runLEDI2C(args []string) int {
	return chip.Run(args, os.Stdout, os.Stderr)
}

func runLEDCtl(name string, args []string) int {
	// Only the daemon ends cleanly on SIGTERM/SIGINT; every other command dies on them (default signal action).
	ctx := context.Background()
	if len(args) >= 2 && args[0] == "daemon" && args[1] == "run" {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
		defer stop()
	}

	self := selfPath()
	env := ledctl.EnvFromGetenv(testenv.Get, self)
	// The daemon, the timers and the effects are this binary again, started as `ugreen-nas led-ctl ...`.
	// A binary that is itself named after a tool needs no prefix.
	if _, named := tools[filepath.Base(self)]; !named {
		env.SelfArgs = []string{"led-ctl"}
	}
	if !env.ToolFromEnv {
		env.Tool = self
		if _, named := tools[filepath.Base(self)]; !named {
			env.ToolArgs = []string{"led-i2c"}
		} else {
			env.Tool = filepath.Join(filepath.Dir(self), "ugreen-led-i2c")
		}
	}
	var chp ledctl.Chip = ledctl.ExecChip{Path: env.Tool, Prefix: env.ToolArgs}
	if testenv.Get("UGREEN_LED_CHIP") == "inproc" { // opt-in, not verified on hardware
		chp = ledctl.InProcChip{Fallback: chp, Product: env.Product}
	}
	c := ledctl.New(env, chp, ledctl.ExecRunner{}, os.Stdout, os.Stderr)
	c.Name = name
	c.Detect = func() []conflict.Conflict { return conflict.Default().Detect() }
	return c.Main(ctx, args)
}

// runPanel: buttons and beeper; SIGINT/SIGTERM end `--watch` and cut a beep short, and the beeper is switched off either way.
func runPanel(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return panel.Main(ctx, panel.Env{}, args, os.Stdout, os.Stderr)
}

func runNasCtl(args []string) int {
	here := filepath.Dir(selfPath())
	return nasctl.New(nasctl.EnvFromGetenv(testenv.Get, here), os.Stdout, os.Stderr).Main(args)
}

// selfPath is this executable, resolved. Linux appends " (deleted)" when a plugin update replaced the
// file under a running process; the name without it is the new binary.
func selfPath() string {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	self = strings.TrimSuffix(self, " (deleted)")
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return self
}
