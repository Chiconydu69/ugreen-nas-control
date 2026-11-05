# Command line

All Go code is one binary, `ugreen-nas`, that runs as the tool its name says (multicall). The archive holds it once and the tool names as symbolic links. `ugreen-nas <tool> ...` works too (`ugreen-nas led-ctl status`).

| Tool             | Installed at                                                              | Code                                            |
| ---------------- | ------------------------------------------------------------------------- | ----------------------------------------------- |
| `ugreen-nas-fan` | `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan`     | `backend/internal/fan`, `backend/internal/mqtt` |
| `ugreen-nas-ctl` | `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl`     | `backend/internal/nasctl`                       |
| `ugreen-led-ctl` | `/usr/local/emhttp/plugins/ugreen-nas-control/led/backend/ugreen-led-ctl` | `backend/internal/led/ledctl`                   |
| `ugreen-led-i2c` | `/usr/local/emhttp/plugins/ugreen-nas-control/led/backend/ugreen-led-i2c` | `backend/internal/led/chip`                     |

Run them as root on the NAS.

## ugreen-nas-fan

Talks to the fan chip, runs the curves every 2 s, reads sensors. Subcommands: `info`, `hardware`, `conflicts`, `cs201x`, `probe`, `probe-fans`, `backlight`, `power-on-ac`, `status`, `history`, `set`, `calibrate`, `restore`, `daemon`, and `publish` (the MQTT publisher, [mqtt.md](mqtt.md)). Background: [fan.md](fan.md), [conflicts.md](conflicts.md), [watchdog.md](watchdog.md).

```
/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan status
```

`status` prints sensors, chip state, presets and the daemon's state file (`/var/run/ugreen-nas-control.json`). `watchdog` is the petter's last state: `state` is `off`, `armed`, `action_needed`, `yielded` (IT8613E: the `it87_wdt` module is loaded, nothing is accessed) or `not_counting` (IT8613E: the value is non-zero but stands still, nothing is written); `counter` is the timer value read back (IT8613E: the 16-bit value register). See [watchdog.md](watchdog.md).

`power-on-ac` reads, `power-on-ac set on|off|last` writes what the NAS does when AC power returns (JSON); from a value it cannot decode (IT8613E, older BIOS) it needs `--force`. Only this command ever touches it: not `status`, the daemon, `restore` or the probe. A write refuses while a fan rival runs. Models, registers and caveats: [power-on-ac.md](power-on-ac.md).

`cs201x` (DXP2800 GT only):

- no argument: reads chip id, 16-bit revision, host interface state, fans, temperatures (cpu = NTC2, board = NTC4, unsigned), the temperature input registers and the watchdog registers (counter read twice, 1.5 s apart). Writes only the Super-I/O unlock and device select.
- `confirm [fan1] [fan3]`: switches fan control on for the named fans (default both; fan1 = cpu, fan3 = case) by saving `fan_map.json` (`chip: CS201x`). Refused while the host interface is off, or when a fan reads 0 rpm or more than 30000. Reads only; the host interface is never switched on.
- `forget`: removes the map; fan control is off again.

Without the map the daemon does not start on this model.

## ugreen-nas-ctl

- `start`, `stop`, `restart`, `status`: the fan daemon (pid file, error log).
- `mqtt-start`, `mqtt-stop`, `mqtt-status`: the MQTT publisher.
- `led-status`, `led-enable`, `led-disable`, `led-import`, `led-boot`, `led-stop`: the LED switch ([led.md](led.md)).
- `report`: anonymised JSON for a model report (below).
- `diag`: one anonymised, read-only diagnostics block for a bug report ([Diagnostics](#diagnostics)).
- `event stopping`: called by the Unraid event script at shutdown.

```
/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl status
```

## ugreen-nas panel

Front-panel buttons and beeper, from UGREEN's driver facts, not yet confirmed on hardware ([panel.md](panel.md)).

- `buttons [--watch]`: power and reset button state; `--watch` prints press and release events. Read only.
- `beep [--freq Hz] [--ms N]`: one tone, 20 to 2000 ms; the speaker is always switched off afterwards.

## Support requests

Paste the output of [`diag`](#diagnostics), plus the NAS model and what you expected. It replaces the `status` commands and every other command we used to ask for.

## Diagnostics

Run it as root on the Unraid box and paste the whole output into the bug report or the forum thread:

```
/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl diag
```

The output is Markdown: a header (versions, kernel, model, BIOS), a _Checks_ list with one line per problem found (a rival fan or LED tool that is running comes first as `CONFLICT: ...`), then collapsed sections. Every section has its own time limit; a section that fails prints its error and the rest continues.

Collected: DMI model strings, the report summary, LED state (settings, per-LED state, bay to SATA port to slot name, an I2C scan of all buses, the CS201x registers on the DXP2800 GT, `ugreen-led-ctl status`), fan state (chip, speeds, source temperatures, failsafe, watchdog, fan caps and map, calibration, tach inputs), other fan and LED tools (installed, running with pid and uptime, cron entries and User Scripts that write fans or LEDs, kernel modules, hwmon outputs in manual mode, whether the plugin yields), drives by slot name (type, spin state, temperature), a _Features_ block (one line per feature: state and effective settings, `n/a for this model` where the model lacks it), every known setting of `fan.cfg` and `leds.cfg` with its value, plugin state, loaded modules, hwmon entries, I/O port ranges, PCI IDs of SMBus, storage and network controllers, matching `dmesg` lines and the last 50 `ugreen-*` syslog lines.

Never collected: serial numbers, UUIDs, asset tags, MAC and IP addresses, the host name, drive serials, ids, models and labels, pool, share and user names, MQTT host, user, password and topics. Addresses, the host name, `serial=`/`user=`/`share=` values, `/mnt` paths and serial-looking tokens in log lines are cut out of the whole text. Settings: the keys are the ones the code reads (`fan.KnownSetting`, `ledctl.KnownKey`, `mqtt.Keys`, the UI keys), per-bay and per-LED keys included, and they print with their value. Hidden: unknown keys (`<key>: <hidden>`), credentials and addresses (any key containing `pass`, `token`, `secret`, `user` or `host`, the MQTT name), the excluded drives, drive serials (`bayN_serial`, `smart_ack_*`) and the names behind script and network inputs (only the kind prints; scripts show as a count with the interpreter of their shebang). Hidden known keys print `<hidden> (set)` or `(empty)`. Read the output before you paste it.

The _Features_ block has one line per feature, `<id>: state and effective values`; a feature the model does not have reads `n/a for this model`. The ids: `led.control`, `led.daemon`, `led.live`, `led.status_mode`, `led.disk_activity`, `led.smart`, `led.disk_errors`, `led.standby`, `led.netdev`, `led.night`, `led.alerts`, `led.sync`, `led.shutdown`, `led.effects`, `led.identify`, `led.import`, `fan.control`, `fan.daemon`, `fan.mode`, `fan.curves`, `fan.inputs`, `fan.excluded_drives`, `fan.limits`, `fan.failsafe`, `fan.quiet`, `fan.jobs`, `fan.shaping`, `fan.calibration`, `fan.history`, `fan.map`, `fan.profiles`, `fan.scripts`, `watchdog`, `power_on_ac`, `panel.buttons`, `panel.beeper`, `lcd.backlight`, `mqtt`, `conflicts`, `ui.dashboard`, `ui.language_theme`. A new feature gets its line in `featureTable` (`backend/internal/nasctl/diag_features.go`), its id in `documentedFeatures` (`diag_test.go`, which fails until both exist) and its id here.

The _Model_ section compares what was detected with the model registry (`web/models/`): per area the registry state and what is still open, and expected against detected for bays, M.2 slots, LAN ports, fan chip and headers, LED controller and watchdog; a difference shows in _Checks_ as `MISMATCH`. The last block is the same result as JSON (`"schema": "diag/1"`) and can be attached as evidence for a model; the visual checks (Identify blinks the right bay, the case view matches) still come from the _Confirm my device_ form.

No hardware writes: no LED, fan, EC or Super-I/O register is written. The only system change is `modprobe i2c-dev` when the module is missing, because the I2C scan needs `/dev/i2c-*`; the tools it runs keep their own lock and cache files as in normal use. The output says so (`loaded i2c-dev for the scan`). `diag` does not load `i2c_i801`: without the SMBus adapter it skips `ugreen-led-ctl status` and says why.

## Report a model

Run the report on the Unraid box and paste the JSON into the [model report form](https://github.com/ugreen-nas-control/ugreen-nas-control/issues/new?template=model-report.yml):

```
/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl report
```

It prints the DMI product name, the chips found, the bus probe, the LED and fan self-test results and the plugin version as JSON, without serial numbers, IP addresses or drive names. Then tick in the form what works on your device. Without a shell, **About, Confirm my device** in the plugin opens a prepared issue with what you ticked. Check with _Identify_ on the LEDs page that the right bay blinks.

A report turns an _expected_ capability into _community_ ([how verification works](hardware.md#how-verification-works)). To add the model yourself: [development.md](development.md#add-a-model).
