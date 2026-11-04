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
- `event stopping`: called by the Unraid event script at shutdown.

```
/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl status
```

## ugreen-nas panel

Front-panel buttons and beeper, from UGREEN's driver facts, not yet confirmed on hardware ([panel.md](panel.md)).

- `buttons [--watch]`: power and reset button state; `--watch` prints press and release events. Read only.
- `beep [--freq Hz] [--ms N]`: one tone, 20 to 2000 ms; the speaker is always switched off afterwards.

## Support requests

Include the output of both `status` commands, the NAS model and the Unraid and plugin versions.

## Report a model

Run the report on the Unraid box and paste the JSON into the [model report form](https://github.com/ugreen-nas-control/ugreen-nas-control/issues/new?template=model-report.yml):

```
/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl report
```

It prints the DMI product name, the chips found, the bus probe, the LED and fan self-test results and the plugin version as JSON, without serial numbers, IP addresses or drive names. Then tick in the form what works on your device. Without a shell, **About, Confirm my device** in the plugin opens a prepared issue with what you ticked. Check with _Identify_ on the LEDs page that the right bay blinks.

A report turns an _expected_ capability into _community_ ([how verification works](hardware.md#how-verification-works)). To add the model yourself: [development.md](development.md#add-a-model).
