// Package fan is the ugreen-nas-fan tool and daemon: it reads and drives the fans of UGREEN DXP NAS devices through
// the ITE IT8613E environment controller or the IT5571 embedded controller, without a kernel module.
package fan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/ugreen-nas-control/ugreen-nas-control/internal/fan/conflict"
	"github.com/ugreen-nas-control/ugreen-nas-control/internal/hw"
)

// lockPath overrides the shared lock file (tests); empty means hw.LockPath, the one every process resolves alike.
var lockPath string

// withLock keeps the index/data port pair to one process at a time
// (daemon, web page, command line, LED backend, front panel).
func withLock(fn func() error) error {
	if lockPath != "" {
		return hw.WithLockAt(lockPath, fn)
	}
	return hw.WithLock(fn)
}

// withLockStop is withLock for the paths that hand the fans back or disarm the watchdog (daemon stop, restore, end of
// a calibration): they wait up to hw.StopLockTimeout instead of hw.LockTimeout, so a busy CLI never leaves the fans at
// their last duty.
func withLockStop(fn func() error) error {
	if lockPath != "" {
		return hw.WithLockAtFor(lockPath, hw.StopLockTimeout, fn)
	}
	return hw.WithLockFor(hw.StopLockTimeout, fn)
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: ugreen-nas-fan <command>

  probe               fan controller found? (JSON)
  status              fans, temperatures and system info (JSON)
  history [1h|24h]    fan speeds, temperatures and what drove them, kept in the daemon's RAM (JSON)
  info                system info only, no hardware access (JSON)
  conflicts           other fan and LED tools that can drive the same chips (JSON, read only)
  hardware            RAM, NVMe, disk bays, network ports; read only (JSON)
  inputs              fan input choices with readings (JSON, scripts are not run)
  input-test <spec>   read one input spec once, scripts in the sandbox (JSON)
  profile export|sources|preview|apply
                      fan profile as JSON; rival configs; preview or apply (stdin or --source) (JSON)
  set <fan|cpu|case|all> <0-100>
                      set a fan speed by hand (the daemon overrides it)
  restore             write back the fan settings the BIOS made
  calibrate           measure rpm per speed for every fan, about 4 minutes (the daemon must run)
  probe-fans run|status|confirm <pwmN=cpu|case>...|forget|restore|tachs|regs
                      find which output drives which fan on an IT8613E model without a known map (JSON, run: about 1 minute)
  backlight [set <0-100>]
                      iDX6011 Pro LCD brightness: read, or set (JSON)
  power-on-ac [set on|off|last [--force]]
                      power-on behaviour after AC loss: read, or set (JSON; only ever written by this command)
  cs201x              DXP2800 GT only: read the chip and its fan registers, writes nothing to a fan (JSON)
  daemon              run the fan curves until SIGTERM
  publish             send the status to an MQTT broker (settings: mqtt_* in fan.cfg), until SIGTERM
`)
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func openChip() (Fans, error) {
	if err := requireSupportedModel(); err != nil {
		return nil, err
	}
	return openChipUnchecked()
}

func openChipUnchecked() (Fans, error) {
	chip, _, err := openChipPorts()
	return chip, err
}

// openChipPorts is openChipUnchecked that also hands out the opened ports, for callers that outlive one command.
func openChipPorts() (Fans, hw.Ports, error) {
	p, err := hw.OpenPorts()
	if err != nil {
		return nil, nil, err
	}
	var chip Fans
	model := productName()
	err = withLock(func() (err error) { chip, err = detectFans(p, model); return })
	if err != nil {
		_ = p.Close()
		return nil, nil, err
	}
	return chip, p, nil
}

// closableFans is a chip that closes its ports after a one-shot read. It still answers BoardTemper (nil: none).
type closableFans struct {
	Fans
	ports hw.Ports
}

func (c closableFans) Close() error { return c.ports.Close() }

func (c closableFans) BoardTemp() *int {
	if bt, ok := c.Fans.(BoardTemper); ok {
		return bt.BoardTemp()
	}
	return nil
}

// openChipOnce is openChip for readers that run repeatedly in one process (the MQTT publisher): addChipStatus
// closes what it opened.
func openChipOnce() (Fans, error) {
	if err := requireSupportedModel(); err != nil {
		return nil, err
	}
	chip, p, err := openChipPorts()
	if err != nil {
		return nil, err
	}
	return closableFans{chip, p}, nil
}

type FanStatus struct {
	ID   string `json:"id"`
	Role string `json:"role"`
	RPM  int    `json:"rpm"`
	Pct  *int   `json:"pct"` // null: speed unknown (EC in auto mode)
	Auto bool   `json:"auto"`
	Err  string `json:"error,omitempty"` // the controller could not be read
}

func Main() {
	if len(os.Args) < 2 {
		usage()
	}

	if runInputsProfile(os.Args[1:]) {
		return
	}

	switch os.Args[1] {
	case "info":
		printJSON(ReadInfo())

	case "conflicts":
		printJSON(conflictList())

	case "history":
		historyCommand(os.Args[2:])

	case "hardware":
		printJSON(ReadHardware())

	case "cs201x":
		cs201xCommand()

	case "probe-fans":
		ProbeCommand(os.Args[2:])

	case "backlight":
		BacklightCommand(os.Args[2:])

	case "power-on-ac":
		PowerACCommand(os.Args[2:])

	case "probe":
		chip, err := openChip()
		if err != nil {
			printJSON(map[string]any{"ok": false, "error": err.Error()})
			os.Exit(1)
		}
		out := map[string]any{"ok": true, "chip": chip.Name()}
		for k, v := range chip.Details() {
			out[k] = v
		}
		printJSON(out)

	case "status":
		printJSON(statusAnswer(openChip))

	case "set":
		if len(os.Args) != 4 {
			usage()
		}
		pct, err := strconv.Atoi(os.Args[3])
		if err != nil || pct < 0 || pct > 100 {
			fail(fmt.Errorf("speed must be 0-100"))
		}
		refuseWhileRival()
		chip, err := openChip()
		if err != nil {
			fail(err)
		}
		var picked []Channel
		for _, ch := range chip.Outputs() {
			if os.Args[2] == "all" || os.Args[2] == ch.ID || os.Args[2] == roles[ch.ID] {
				picked = append(picked, ch)
			}
		}
		if len(picked) == 0 {
			fail(fmt.Errorf("unknown fan %q", os.Args[2]))
		}
		err = withLock(func() error {
			if _, err := saveBIOS(chip); err != nil {
				return err
			}
			for _, ch := range picked {
				if err := chip.SetPWM(ch, percentToDuty(pct, chip.MaxDuty())); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			fail(err)
		}
		fmt.Printf("%d fan(s) at %d%%.\n", len(picked), pct)

	case "calibrate":
		refuseWhileRival()
		chip, err := openChip()
		if err != nil {
			fail(err)
		}
		if err := runCalibrate(chip); err != nil {
			fail(err)
		}
		fmt.Println("Fan speeds measured.")

	case "restore":
		b, err := os.ReadFile(biosPath)
		if err != nil {
			fmt.Println("Nothing to restore: the fans still run as the BIOS set them.")
			return
		}
		var s Snapshot
		if err := json.Unmarshal(b, &s); err != nil {
			fail(err)
		}
		// Not model-checked: the snapshot only exists if this tool saved it, and
		// putting the BIOS values back must always work.
		chip, err := openChipUnchecked()
		if err != nil {
			fail(err)
		}
		if err := withLockStop(func() error { return chip.Restore(s) }); err != nil {
			fail(err)
		}
		fmt.Println("BIOS fan settings restored.")

	case "daemon":
		if !LoadConfig(configPath).FanControl {
			fail(fmt.Errorf("fan control is switched off in the settings"))
		}
		chip, err := openChip()
		if err != nil {
			if wd, werr := openWatchdogOnly(); werr == nil {
				if err := runWatchdogOnly(wd); err != nil {
					fail(err)
				}
				return
			}
			fail(err)
		}
		if err := runDaemon(chip); err != nil {
			fail(err)
		}

	default:
		usage()
	}
}

// statusAnswer is `status`: the answer web/plugin/api.php adds its own keys to (running, version, led, settings).
// open is a parameter so that a test can stand in for the hardware.
func statusAnswer(open func() (Fans, error)) map[string]any {
	sensors := ReadSensors()
	out := map[string]any{"ok": true, "info": ReadInfo(), "sensors": &sensors, "presets": presets, "net_io": ReadNetIO()}

	// Switched off: not a single read of the fan chip, another plugin may be using it. The temperatures
	// from sysfs and Unraid are still reported.
	fanOn := LoadConfig(configPath).FanControl
	out["fan_control"] = fanOn
	list := conflictList()
	out["conflicts"] = list
	out["conflict"] = len(conflict.Blocking(list, conflict.Fan)) > 0

	if fanOn {
		addChipStatus(out, &sensors, open)
	}
	if caps, ok := CapsFor(productName()); ok {
		out["fan_caps"] = caps
		out["fan_config"] = LoadConfig(configPath).View()
	}
	out["calibration"] = readCalibStatus()
	out["watchdog"] = ReadWatchdogStatus()

	if b, err := os.ReadFile(statePath); err == nil {
		var st State
		if json.Unmarshal(b, &st) == nil {
			out["daemon"] = st
		}
	}
	return out
}

// addChipStatus reads the fan chip: its fans, its own temperatures, and the mainboard temperature if the
// controller has one. Only called while fan control is on.
func addChipStatus(out map[string]any, sensors *Sensors, open func() (Fans, error)) {
	chip, err := open()
	if err != nil {
		out["chip_error"] = err.Error()
		return
	}
	if c, ok := chip.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}
	var fans []FanStatus
	var temps []*int
	_ = withLock(func() error {
		for _, ch := range chip.Outputs() {
			fans = append(fans, readFanStatus(chip, ch))
		}
		temps = chip.Temps()
		if bt, ok := chip.(BoardTemper); ok && sensors.Board == nil {
			sensors.Board = bt.BoardTemp()
		}
		return nil
	})
	out["chip"] = chip.Name()
	out["fans"] = fans
	out["chip_temps"] = temps
}

// readFanStatus reports one fan; pct stays nil where the controller does not
// tell the real output (IT5571 in auto mode keeps the last manual duty).
func readFanStatus(chip Fans, ch Channel) FanStatus {
	rpm, rpmErr := chip.RPM(ch)
	duty, auto, pwmErr := chip.PWM(ch)
	fs := FanStatus{ID: ch.ID, Role: roles[ch.ID], RPM: rpm, Auto: auto}
	if err := errors.Join(rpmErr, pwmErr); err != nil {
		fs.Err = err.Error()
	}
	if pwmErr == nil && (!auto || chip.AutoDutyKnown()) {
		p := dutyToPercent(duty, chip.MaxDuty())
		fs.Pct = &p
	}
	return fs
}

// conflictList is never nil, so that the JSON is [] and not null.
func conflictList() []conflict.Conflict {
	list := detectConflicts()
	if list == nil {
		return []conflict.Conflict{}
	}
	return list
}

// refuseWhileRival: hand-set speeds and the measurement write the chip like the daemon, so the same rule applies.
func refuseWhileRival() {
	if r := conflict.Blocking(detectConflicts(), conflict.Fan); len(r) > 0 {
		fail(fmt.Errorf("%s controls the fans too; switch one of the two off first", rivalNames(r)))
	}
}
