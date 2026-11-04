// Package conflict finds other tools that can drive the same fan chip or LED controller as UGREEN NAS Control:
// rival plugins, kernel modules that own the chip, hwmon pwm outputs in manual mode and running rival daemons.
// Everything is read only. Every path is a field of Roots, so tests run against a temp directory.
package conflict

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/testenv"
)

// Kinds of finding.
const (
	KindPlugin = "plugin" // /boot/config/plugins/<name>.plg
	KindModule = "module" // kernel module that owns one of our chips
	KindHwmon  = "hwmon"  // hwmon device of our Super-I/O chip with pwm outputs in manual mode
	KindDaemon = "daemon" // a rival process is running
)

// Severities. Block: a rival writer is running, the plugin must not write the same chip. Warn: installed or
// loaded, but nothing is seen writing; shown, nothing yields.
const (
	SeverityBlock = "block"
	SeverityWarn  = "warn"
)

// What a finding controls.
const (
	Fan      = "fan"
	LED      = "led"
	Watchdog = "watchdog" // the IT8613E watchdog timer, owned by it87_wdt while that module is loaded
)

// Conflict is one finding.
type Conflict struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`   // plugin name, module name, process name, hwmon name
	Name     string   `json:"name"` // what the user knows it as
	Controls []string `json:"controls"`
	Severity string   `json:"severity"`
	Detail   string   `json:"detail,omitempty"` // e.g. "pwm1, pwm2 manual"
}

// Roots are the places detection reads; the defaults are the real system paths.
type Roots struct {
	PluginsBoot string // /boot/config/plugins
	PluginsWeb  string // /usr/local/emhttp/plugins
	ProcModules string // /proc/modules
	SysModule   string // /sys/module
	Hwmon       string // /sys/class/hwmon
	Proc        string // /proc
	Self        int    // our own pid, never reported
}

// Default returns the real paths; each can be moved with the UGREEN_* variable of the shell tools.
func Default() Roots {
	env := func(key, def string) string {
		if v := testenv.Get(key); v != "" {
			return v
		}
		return def
	}
	return Roots{
		PluginsBoot: env("UGREEN_PLUGINS_BOOT", "/boot/config/plugins"),
		PluginsWeb:  env("UGREEN_PLUGINS_WEB", "/usr/local/emhttp/plugins"),
		ProcModules: env("UGREEN_PROC_MODULES", "/proc/modules"),
		SysModule:   env("UGREEN_SYS_MODULE", "/sys/module"),
		Hwmon:       env("UGREEN_SYS_HWMON", "/sys/class/hwmon"),
		Proc:        env("UGREEN_PROC", "/proc"),
		Self:        os.Getpid(),
	}
}

type plugin struct {
	id, name string
	controls []string
}

// Plugin file names under /boot/config/plugins (without .plg), as the plugins install themselves.
var plugins = []plugin{
	{"fanctrlplus", "FanCtrl Plus", []string{Fan}},
	{"fanctrlplus2", "FanCtrl Plus 2", []string{Fan}},
	{"dynamix.system.autofan", "Dynamix Auto Fan Control", []string{Fan}},
	{"ugreen-it87", "UGREEN it87 driver (IT-Kuny)", []string{Fan}},
	{"ugreen-led-control", "UGREEN LED Control", []string{LED}},
	{"ugreenleds-driver", "UGREEN LEDs driver", []string{LED}},
	{"UGREEN-LEDs-unRAID", "UGREEN LEDs for Unraid", []string{LED}},
	{"UGREEN-DXP4800GT-LED-Driver", "UGREEN DXP4800 GT LED driver", []string{LED}},
	{"UGREEN-DXP4800Pro-LEDs", "UGREEN DXP4800 Pro LEDs", []string{LED}},
	{"ugreen-idx6011-pro", "UGREEN iDX6011 Pro", []string{Fan, LED}},
	{"unraid-ugreen-idx6011-led-fan", "UGREEN iDX6011 LED and fan", []string{LED}},
}

type module struct {
	id, name string
	controls []string
}

// Kernel modules that own one of our chips.
var modules = []module{
	{"it87", "it87 kernel driver", []string{Fan}},
	{"led_ugreen", "led_ugreen kernel driver", []string{LED}},
	{"leds_sio_201x", "UGREEN CS201x LED driver", []string{LED}},
	{"it87_wdt", "it87_wdt kernel driver", []string{Watchdog}},

	// UGREEN's own vendor modules (drivers/ugreen, names as loaded: dashes become underscores). Fan and watchdog
	// registers live behind the same Super-I/O or EC ports we use; the MCU LED drivers own the LED controller.
	{"ug_it86x_sio", "UGREEN IT8613E driver (ug_it86x_sio)", []string{Fan}},
	{"ug_it86x_sio_dh2600", "UGREEN DH2600 IT8613E driver", []string{Fan}},
	{"ug_it86x_cpufan", "UGREEN DXP480T CPU fan driver", []string{Fan}},
	{"ug_idx6011_sio", "UGREEN iDX6011 EC driver (ug_idx6011_sio)", []string{Fan, Watchdog}},
	{"ug_idx6011pro_sio", "UGREEN iDX6011 Pro EC driver (ug_idx6011pro_sio)", []string{Fan, Watchdog}},
	{"ug_201x", "UGREEN CS201x driver (ug_201x)", []string{Fan, LED, Watchdog}},
	{"ug_sio201", "UGREEN CS201x driver (ug_sio201)", []string{Fan, LED, Watchdog}},
	{"leds_mcu", "UGREEN LED MCU driver (leds_mcu)", []string{LED}},
	{"leds_mcu_28a48", "UGREEN LED MCU driver (leds_mcu_28a48)", []string{LED}},
	{"leds_mcu_68a88", "UGREEN LED MCU driver (leds_mcu_68a88)", []string{LED}},
	{"leds_mcu_n76e003", "UGREEN LED MCU driver (leds_mcu_n76e003)", []string{LED}},
	{"leds_mcu_n76e003_dh2600", "UGREEN LED MCU driver (leds_mcu_n76e003_dh2600)", []string{LED}},
}

type daemon struct {
	arg      string // basename of the program (argv[0], or the script behind an interpreter)
	name     string
	controls []string
}

// Processes that write the chips, matched by the program they run, never by a later argument.
var daemons = []daemon{
	{"fanctrlplus2_loop.sh", "FanCtrl Plus 2", []string{Fan}},
	{"fanctrlplus_loop.sh", "FanCtrl Plus", []string{Fan}},
	{"autofan", "Dynamix Auto Fan Control", []string{Fan}},
	{"ugreen-fan-control", "UGREEN fan control (IT-Kuny)", []string{Fan}},
	{"ugreen-gt-leds", "UGREEN DXP4800 GT LED driver", []string{LED}},
	{"ugreen-pro-leds", "UGREEN DXP4800 Pro LEDs", []string{LED}},
	{"ugreen-leds", "UGREEN LEDs driver", []string{LED}},
	{"panel_dash", "UGREEN iDX6011 Pro", []string{Fan, LED}},
}

// hwmon names of the Super-I/O chips this plugin drives.
var chipNames = []string{"it8613", "it5571"}

// Detect lists everything found; rivals that run come first within their kind.
func (r Roots) Detect() []Conflict {
	var out []Conflict
	out = append(out, r.plugins()...)
	out = append(out, r.modules()...)
	out = append(out, r.hwmon()...)
	out = append(out, r.daemons()...)
	return out
}

func (r Roots) plugins() (out []Conflict) {
	for _, p := range plugins {
		if exists(filepath.Join(r.PluginsBoot, p.id+".plg")) || isDir(filepath.Join(r.PluginsWeb, p.id)) {
			out = append(out, Conflict{Kind: KindPlugin, ID: p.id, Name: p.name, Controls: p.controls, Severity: SeverityWarn})
		}
	}
	return out
}

func (r Roots) modules() (out []Conflict) {
	b, _ := os.ReadFile(r.ProcModules)
	loaded := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			loaded[f[0]] = true
		}
	}
	for _, m := range modules {
		if loaded[m.id] || isDir(filepath.Join(r.SysModule, m.id)) {
			out = append(out, Conflict{Kind: KindModule, ID: m.id, Name: m.name, Controls: m.controls, Severity: SeverityWarn})
		}
	}
	return out
}

// hwmon: a device of our chip with pwmN_enable = 1 (manual). The BIOS leaves manual mode behind too, so this
// alone never makes a plugin yield.
func (r Roots) hwmon() (out []Conflict) {
	dirs, _ := filepath.Glob(filepath.Join(r.Hwmon, "hwmon*"))
	for _, dir := range dirs {
		name := readTrim(filepath.Join(dir, "name"))
		if !slices.Contains(chipNames, name) {
			continue
		}
		enables, _ := filepath.Glob(filepath.Join(dir, "pwm[0-9]*_enable"))
		var manual []string
		for _, e := range enables {
			if readTrim(e) == "1" {
				manual = append(manual, strings.TrimSuffix(filepath.Base(e), "_enable"))
			}
		}
		if len(manual) == 0 {
			continue
		}
		slices.Sort(manual)
		out = append(out, Conflict{Kind: KindHwmon, ID: name, Name: name + " hwmon", Controls: []string{Fan}, Severity: SeverityWarn, Detail: strings.Join(manual, ", ") + " manual"})
	}
	return out
}

func (r Roots) daemons() (out []Conflict) {
	dirs, _ := filepath.Glob(filepath.Join(r.Proc, "[0-9]*"))
	seen := map[string]bool{}
	for _, dir := range dirs {
		if pid, err := strconv.Atoi(filepath.Base(dir)); err != nil || pid == r.Self {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil {
			continue
		}
		for _, d := range daemons {
			if seen[d.arg] || program(string(b)) != d.arg {
				continue
			}
			seen[d.arg] = true
			out = append(out, Conflict{Kind: KindDaemon, ID: d.arg, Name: d.name, Controls: d.controls, Severity: SeverityBlock})
		}
	}
	slices.SortFunc(out, func(a, b Conflict) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// interpreters whose first non-option argument is the script that runs.
var interpreters = []string{"sh", "bash", "dash", "ash", "zsh", "php", "perl", "ruby", "bun", "node", "python", "python3"}

// program: base name of argv[0], or of the script when argv[0] is an interpreter. A tool that only has a rival's
// name among its arguments (pgrep -f, grep, tail, an editor) is not that rival.
func program(cmdline string) string {
	words := strings.Split(cmdline, "\x00")
	if len(words) == 0 || words[0] == "" {
		return ""
	}
	prog := filepath.Base(words[0])
	if !slices.Contains(interpreters, prog) {
		return prog
	}
	for _, w := range words[1:] {
		if w != "" && !strings.HasPrefix(w, "-") {
			return filepath.Base(w)
		}
	}
	return prog
}

// Blocking says whether a running rival writes what is: Fan or LED.
func Blocking(list []Conflict, what string) []Conflict {
	var out []Conflict
	for _, c := range list {
		if c.Severity == SeverityBlock && slices.Contains(c.Controls, what) {
			out = append(out, c)
		}
	}
	return out
}

// Present says whether any finding, of any severity, controls what. The watchdog petter yields on a loaded
// it87_wdt, which is a warning for the fans and never blocks them.
func Present(list []Conflict, what string) bool {
	return slices.ContainsFunc(list, func(c Conflict) bool { return slices.Contains(c.Controls, what) })
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

func readTrim(p string) string { b, _ := os.ReadFile(p); return strings.TrimSpace(string(b)) }
