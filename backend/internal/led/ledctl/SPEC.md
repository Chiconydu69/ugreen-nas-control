# ugreen-led-ctl: behaviour the Go port keeps

The Go binary (`ugreen-led-ctl`, a name of `backend/cmd/ugreen-nas`) must print and leave behind what the goldens in `backend/internal/led/parity/testdata/golden/*.json` record for each scenario (stdout, stderr, exit code, chip registers, state files, calls of the chip tool and the stubs); `parity/` proves it (see the end). Where the Go port differs on purpose, the scenario says why and must keep differing (`intended`).

The web UI (`web/plugin/led/api.php`) runs `ugreen-led-ctl <args> 2>&1` and uses the exit code, the JSON of `status`,
`all`, `list`, `bays`, `get`, and the plain text of everything else as an error message. So stdout, stderr and the
exit code all count.

## Commands

| Command                                                                           | Output (stdout)                                                                                                                   | Exit                                                              |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| `help`, `-h`, `--help`, no args                                                   | usage text                                                                                                                        | 0                                                                 |
| unknown command                                                                   | stderr `ERROR: Unknown command 'x'.`, usage on stdout                                                                             | 2                                                                 |
| `status`                                                                          | JSON object (fields in the table below)                                                                                           | 0                                                                 |
| `list`                                                                            | `["power", "netdev", "disk1", ...]` (empty `[]` when the chip does not answer `get power`)                                        | 0                                                                 |
| `get <led>`                                                                       | JSON `{name, brightness, max_brightness, color, effect, on_ms, off_ms[, sent_ms, now_ms]}`                                        | 1 unknown LED or chip error                                       |
| `all`                                                                             | JSON object `{"<led>": {...}, ...}` of the LEDs that answer                                                                       | 0                                                                 |
| `apply`                                                                           | `Saved settings applied.` or `No saved settings (<file>).`                                                                        | 0                                                                 |
| `bays`                                                                            | JSON array, one object per bay (disk, serial, Unraid slot/status, temp, limits, SMART state)                                      | 0                                                                 |
| `on` / `off` / `stop <led>`                                                       | nothing                                                                                                                           | rc of the chip tool; 1 unknown LED                                |
| `brightness <led> <0-255>`                                                        | nothing; `0` = off, other values switch an off LED on                                                                             | 1 on bad value                                                    |
| `color <led> <R> <G> <B>`                                                         | nothing                                                                                                                           | 1 on bad value                                                    |
| `blink` / `breath <led> <on_ms> <off_ms>`                                         | nothing                                                                                                                           | rc of the chip tool                                               |
| `daemon start\|stop\|restart`                                                     | `Status daemon started.` / `stopped.` / `is already running.`                                                                     | 1 if the foreign monitor runs (start)                             |
| `daemon status`                                                                   | `running (pid N)` or `stopped`                                                                                                    | 3 when stopped                                                    |
| `daemon run`                                                                      | foreground loop until SIGTERM/SIGINT                                                                                              | 0                                                                 |
| `daemon <other>`                                                                  | stderr `Usage: <argv0> daemon start\|stop\|restart\|status\|run`                                                                  | 2                                                                 |
| `schedule [--force]`                                                              | night/day switch, then the alert tick; `0` without work                                                                           | rc of the alert tick                                              |
| `alerts [--force]`                                                                | alert tick                                                                                                                        | 0, but 1 for `--force` with nothing to clear (script quirk, kept) |
| `shutdown [--test]`                                                               | `--test`: `Power LED signals shutdown for 6 s.`                                                                                   | 0                                                                 |
| `power-restore`                                                                   | nothing (internal: the timer of `shutdown --test`)                                                                                | rc of the alert tick                                              |
| `fx start\|restart`                                                               | `Effect <name> started on <leds>.` / `No effect configured.`                                                                      | 1 `ERROR: Effect '<n>' did not start: <first log line>` on stderr |
| `fx stop`                                                                         | `Effect stopped.`                                                                                                                 | 0                                                                 |
| `fx status`                                                                       | `running: <name> on <leds>` or `stopped`                                                                                          | 3 when stopped                                                    |
| `identify <diskN> [s]`                                                            | `Identifying <led> for <s> s.`                                                                                                    | 1 on bad LED/seconds                                              |
| `identify-stop <diskN> [token]`                                                   | `Stopped identifying <led>.` (silent when nothing to stop or token differs)                                                       | 0                                                                 |
| `smbus`                                                                           | `SMBus adapter: <grep -H lines or the /dev/mem note>`                                                                             | 1 + dmesg lines on stderr                                         |
| `dxp480t`                                                                         | nothing (`dxp480t white` on the chip tool)                                                                                        | 1 on other models                                                 |
| wrong argument count of `get/on/off/stop/brightness/color/blink/breath/identify*` | stderr `Usage: <argv0> ...`                                                                                                       | 2                                                                 |
| `_after <seconds> <command...>`                                                   | Go only, internal: the detached timer behind `identify` and `shutdown --test` (the shell used `setsid bash -c 'sleep; exec ...'`) |                                                                   |

`status` fields, in order: ok, foreign_monitor, model, bays, kernel, mapping, mode, daemon, fx, fx_leds, identify,
sync_action, sync_percent, sync_enabled, sync_color, standby_mode, standby_level, smart_enabled, error_enabled,
activity_enabled, smart_color, disk_color_mode, netdev_status, alert_enabled, alert_level, alert_color,
shutdown_blink, shutdown_color, shutdown_effect, alert_count, alert_active, fx_speed, fx_reverse, fx_idle, night,
disk_error_color, saved_brightness, network_device, netdev_speed, network_ports, netdev_speed_enabled,
netdev_speed_colors, controller, i2c_bus, i2c_address, i2c_probe, leds.

Before every command except help the script makes sure an SMBus adapter exists (`modprobe i2c-dev`, `i2c_i801`,
waits up to 3 s), unless `UGREEN_LED_I2C` is set.

## Environment (tests only; the release build ignores all of it)

| Variable                                                                                                                         | Default                                                                                                                                                        |
| -------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `UGREEN_LED_I2C`                                                                                                                 | the multicall binary itself (`ugreen-nas led-i2c`); when set, no SMBus preflight                                                                               |
| `UGREEN_LED_CONFIG`                                                                                                              | `/boot/config/plugins/ugreen-nas-control/leds.cfg`                                                                                                             |
| `UGREEN_DISKS_INI`, `UGREEN_VAR_INI`                                                                                             | `/var/local/emhttp/disks.ini`, `var.ini`                                                                                                                       |
| `UGREEN_SMART_DIR`                                                                                                               | `/var/local/emhttp/smart`                                                                                                                                      |
| `UGREEN_SYS_BLOCK`, `_SYS_NET`, `_SYS_ATA`, `_SYS_PCI`                                                                           | `/sys/block`, `/sys/class/net`, `/sys/class/ata_port`, `/sys/bus/pci/devices`                                                                                  |
| `UGREEN_UDEV_DATA`                                                                                                               | `/run/udev/data`                                                                                                                                               |
| `UGREEN_DYNAMIX_CFG`                                                                                                             | `/boot/config/plugins/dynamix/dynamix.cfg`                                                                                                                     |
| `UGREEN_NOTIFY_DIR`                                                                                                              | notify path of dynamix.cfg, else `/tmp/notifications`                                                                                                          |
| `UGREEN_PIDFILE`, `_FX_PIDFILE`, `_FX_LOG`, `_SCHEDULE_STATE`, `_ALERT_STATE`, `_SHUTDOWN_MARK`, `_BUS_CACHE`, `_MAXSPEED_CACHE` | `/var/run/ugreen-nas-control.led*...`                                                                                                                          |
| `UGREEN_IDENTIFY_PREFIX`, `UGREEN_PHASE_PREFIX`                                                                                  | `/var/run/ugreen-nas-control.led.identify-`, `.led.phase-`                                                                                                     |
| `UGREEN_PRODUCT`                                                                                                                 | `/sys/class/dmi/id/product_name`, else `UGREEN NAS`                                                                                                            |
| `UGREEN_NOW`                                                                                                                     | clock, `HHMM`, for night mode tests                                                                                                                            |
| Go only: `UGREEN_DMI_PRODUCT`, `UGREEN_OSRELEASE`, `UGREEN_PROC_IOMEM`, `UGREEN_PROC_MODULES`, `UGREEN_SYS_I2C`                  | the fixed paths the script hard-codes (`/sys/class/dmi/id/product_name`, `/proc/sys/kernel/osrelease`, `/proc/iomem`, `/proc/modules`, `/sys/bus/i2c/devices`) |

## Settings file (`leds.cfg`)

`key="value"` lines, the last of a key wins, anything else is ignored, nothing is sourced. Read again only when the
content changed.

Per LED (`power`, `netdev`, `disk1..N`): `<led>_color` ("R G B"), `_brightness`, `_effect` (blink|breath|none), `_on`, `_off`.
Global: `mode`, `fx_name`, `fx_leds`, `fx_speed`, `fx_reverse`, `fx_idle`, `sync_enabled`, `sync_color`, `standby_mode`
(normal|dim|off|pulse), `standby_level`, `smart_enabled`, `smart_color`, `error_enabled`, `disk_error_color`,
`activity_enabled`, `disk_color_mode` (own|temperature), `netdev_status` (activity|link|manual), `netdev_speed_enabled`,
`netdev_speed_<10|100|1000|2500|5000|10000>`, `alert_enabled`, `alert_level`, `alert_color`, `shutdown_blink`,
`shutdown_color`, `shutdown_effect`, `night_enabled`, `night_start`, `night_end`, `night_action` (dim|off),
`night_brightness`, `bay<N>_serial`, `smart_ack_<Unraid id with non-alphanumerics as _>`.

## State files (tmpfs)

| File                | Content                                                              |
| ------------------- | -------------------------------------------------------------------- |
| pidfile             | pid of the status daemon                                             |
| fx pidfile / fx log | pid of `ugreen-led-i2c effect`, its stderr                           |
| schedule state      | `day` / `night`                                                      |
| alert state         | `alert` / `none`                                                     |
| shutdown mark       | empty; while it exists `schedule` and `alerts` do nothing            |
| bus cache           | last `i2c_bus` value that answered (valid 10 min)                    |
| maxspeed cache      | `<port> <Mbit/s>` lines from ethtool                                 |
| phase-`<led>`       | `epoch_ms on off` of the last slow blink/breath, for the web preview |
| identify-`<led>`    | line 1 token, line 2 the LED's state before (`get --plain` line)     |

## External commands

| Command                                        | Used for                                                                                                                           |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `ugreen-led-i2c` (`UGREEN_LED_I2C`)            | all LED reads/writes: `probe`, `--plain get`, `set`, `dxp480t white`, `[--speed N] [--reverse] [--idle-only] effect <name> <leds>` |
| `ip route show default`, `ping -c 1 -W 2 <gw>` | network device, link check                                                                                                         |
| `ethtool <port>`                               | maximum port speed                                                                                                                 |
| `udevadm info -q property -n /dev/<dev>`       | disk serial when the udev database file is missing                                                                                 |
| `pgrep -f '^/bin/bash /usr/bin/ugreen-leds'`   | foreign LED monitor                                                                                                                |
| `runlevel`                                     | shutdown (0/6) vs. array stop                                                                                                      |
| `logger -t ugreen-led-control`                 | log, falls back to stdout                                                                                                          |
| `modprobe -q i2c-dev / i2c_i801`, `dmesg`      | SMBus preflight, `smbus`                                                                                                           |

The Go code reaches them through the `Runner` interface (`Chip` for the LED tool), roots through `Env`.

## Daemon loop (0.2 s tick)

Every 5 s (tick 0, 25, ...): reload settings, disks.ini, device paths (and at most every 60 s the table), var.ini
sync state, night mode; decide each bay `off | steady | breath | pulse` (error > SMART > parity check > temperature
or own colour; standby dims/pulses); write only what differs from the last write; netdev by link/gateway/speed or
hands off when `manual`. Every 30 s (tick 150, not at 0): compare with `get all` and rewrite LEDs that differ,
foreign-monitor check (also at tick 0). Every tick: blink a bay/netdev while its counters move. SIGTERM: log
`status daemon stopped`, exit 0. The network check (ping) runs in its own goroutine in Go; in the script it blocks the loop.

## Not covered by parity

- The 30 s paths (compare with `get all` and repair, foreign-monitor check after start, network re-probe): need
  30 s of runtime; unit tests drive them with a 2 ms tick (`TestDaemonLoop`).
- Order of the calls inside `daemon run`: timing dependent, so daemon scenarios compare the sorted multiset of chip
  and stub calls (counts included), the final registers and the state files. Each scenario must also show an effect
  (a call, a register or a state file, or printed output) and the daemon ones a chip `set`, so two sides that fail the
  same way cannot pass. pid files, tokens and timestamps are normalised to placeholders: their content is not compared.
- `go test -tags testhooks` runs a fast subset of parity (15 scenarios, no daemon) when bash >= 4 is on PATH (`nix develop`, the stubs and the fake chip are bash); the
  full set needs `LEDCTL_PARITY=1` (`just parity`, part of `just check`). The environment variables below only work in a binary built with `-tags testhooks`; the release build ignores them.
- The goldens were recorded against the bash fake chip (`testdata/bin/led-chip`), which the Go side uses too; `ExecChip`, `InProcChip` and the register protocol are
  not part of parity. `internal/led/chip` (`TestExecAndInProcChipAgreeOnTheBus`) compares the tool and the in-process
  chip byte for byte (stdout, stderr, exit code, frames) on a fake bus.
- SMBus preflight success path (`/sys/bus/i2c`, `/proc/modules`, `/proc/iomem` are hard-coded in the script):
  only the failing path is compared; the success path is unit tested (`TestEnsureSMBus`).
- Real `setsid`, process groups, `pgrep` on a real `/proc`, `ethtool`/`ip` real output: stubs only.
- Malformed values in `leds.cfg` (non-numeric brightness, two-number colours): the script aborted (`set -u`,
  arithmetic errors); Go checks every value that reaches the chip tool (colours `R G B` 0..255, durations up to 65535 ms, `fx_name`, `fx_speed` 0..4, `fx_leds` known LEDs), logs a rejected one once and uses the default.
- Pid files: the script signalled any live pid in the file; Go does it only for a process of the plugin (`internal/proc`), so a stale file never kills a reused pid.
- Rival LED tools: the script only looked at the `ugreenleds-driver` monitor with `pgrep`; Go also asks the rival detector (`internal/fan/conflict`) at the start of `daemon start` / `daemon run` and every 30 s, and yields to a running rival.
- `readlink -f` over several arguments (a vanished device shifts the lines) and `find -mmin` rounding: Go resolves
  per path and uses an exact 10 minutes.
- Octal reading of numbers with leading zeros (`$((hot - 15))` with `hot="08"`): Go reads decimal.
- The controller is still driven by running `ugreen-led-i2c` once per write (`ExecChip`, the default). `InProcChip`
  calls `internal/led/chip` in the same process instead (`UGREEN_LED_CHIP=inproc`, opt-in); it is tested on a fake bus
  but not verified on hardware. A panic in the chip code becomes exit code 1. The layout (`iDX6011`) follows
  `UGREEN_PRODUCT` or the DMI file of `Env`, read lazily. Effects always go to the exec tool.
- Signals: only `daemon run` handles SIGTERM/SIGINT (clean exit 0); every other command, including `_after`, dies on
  them like the script (exit 143/130).
- A failed start of a background process (`daemon start`, `fx start`, `identify`, `shutdown --test`) prints
  `ERROR: ...` and exits 1; the script printed success regardless.
