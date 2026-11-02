# MQTT

The plugin can publish the status of the NAS to an MQTT broker, for displays and Home Assistant. It is off by default and has no settings page yet: edit `fan.cfg`, then run `ugreen-nas-ctl restart` (or `mqtt-start`).

The publisher is `ugreen-nas-fan publish` (`backend/internal/mqtt`, a small hand-written MQTT 3.1.1 client, no dependency). It only reads, the way the status page does, and never writes a fan register. It runs apart from the fan daemon, so it also runs where fan control is off. `ugreen-nas-ctl start` and `stop` start and stop it with the daemon; `mqtt-start`, `mqtt-stop` and `mqtt-status` handle it alone. Pid file `/var/run/ugreen-nas-control-mqtt.pid`, log `/var/log/ugreen-nas-control-mqtt.log` (replaced on every start).

## Settings

In `/boot/config/plugins/ugreen-nas-control/fan.cfg`. The keys work in a v1 and a v2 file; the plugin never rewrites the file on its own.

| Key                       | Meaning                                                                                              |
| ------------------------- | ---------------------------------------------------------------------------------------------------- |
| `mqtt_enabled`            | `1` to run the publisher                                                                             |
| `mqtt_host`               | broker (required)                                                                                    |
| `mqtt_port`               | default 1883, 8883 with TLS                                                                          |
| `mqtt_tls`                | `1` for TLS (1.2 or newer)                                                                           |
| `mqtt_user`, `mqtt_pass`  | login; the password stays in `fan.cfg` on the flash drive, like the other settings                   |
| `mqtt_name`               | name in the topics, letters, digits, `_` and `-` (default: the host name)                            |
| `mqtt_interval`           | seconds between two states, 2 to 60 (default 5)                                                      |
| `mqtt_control`            | `1` lets a display restart and shut down the NAS (off by default)                                    |
| `mqtt_control_updates`    | `1` lets a display update plugins (off by default)                                                   |
| `mqtt_control_containers` | `1` lets a display update Docker containers that Unraid lists as having a new image (off by default) |

## Topics

Below `ugreen-nas/<name>/`, payloads are JSON:

| Topic           | Direction | Content                                                                                                                                                                                                                                    |
| --------------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `state`         | out       | model, Unraid version, uptime, mode, temperatures, fans, bays, NVMe, network rates with each port's IPv4 address, free space of the array (and each data disk) and the pools, a running parity check, alarms (retained, only when changed) |
| `notifications` | out       | Unraid's unread notifications, newest first, at most 20 (retained)                                                                                                                                                                         |
| `updates`       | out       | updates Unraid already knows of: plugins, containers, Unraid OS (retained, checked once a minute)                                                                                                                                          |
| `availability`  | out       | `online` / `offline` (last will, retained)                                                                                                                                                                                                 |
| `cmd`           | in        | commands; subscribed only when `mqtt_control`, `mqtt_control_updates` or `mqtt_control_containers` is on                                                                                                                                   |
| `cmd_result`    | out       | the answer to each command, then its progress                                                                                                                                                                                              |

`updates` reads what Unraid wrote itself: the fresh `.plg` copies in `/tmp/plugins`, the container status in `/var/lib/docker/unraid-update-status.json` and the OS check in `/tmp/unraidcheck/result.json`. It checks and downloads nothing.

Alarms carry a `code` (`cpu_hot`, `cpu_unreadable`, `disk_hot`, `nvme_hot`, `fan_stalled`) for a display to translate. They use the limits the fan daemon uses, so they also show while the daemon is not running.

## Commands

A fixed list, nothing else is run:

| `cmd`      | Needs                     | Runs                                                                                                                                                                                       |
| ---------- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `reboot`   | `mqtt_control`            | `powerdown -r`                                                                                                                                                                             |
| `shutdown` | `mqtt_control`            | `powerdown`                                                                                                                                                                                |
| `update`   | `mqtt_control_updates`    | `plugin update <file>`, `target` `plugin` and `name` from `updates`                                                                                                                        |
| `update`   | `mqtt_control_containers` | `update_container <name>` for each container created from the image, `target` `container` and `name` from `updates`; `target` `containers` (no `name`) updates every container on the list |

```json
{ "cmd": "update", "target": "plugin", "name": "ugreen-nas-control", "ts": 1767000000, "nonce": "a1b2c3" }
```

The first `cmd_result` is `started` or the reason for a refusal. While a command runs, more answers follow: one per step (`step`, `total`, `name`, text `running`, or `failed: ...`) and a last one with `done` set (`done`, or `N of M failed`). A container is found with `docker ps`; the image from `updates` and the one `docker ps` shows are compared with `:latest`, `docker.io/` and `library/` made equal.

A command is refused when:

- `ts` is more than 30 s from the NAS clock, or `nonce` is missing, longer than 64 characters or already used by an accepted command in the last minute (at most 256 are remembered);
- another command was accepted less than 10 s before;
- the previous command has not finished (one program at a time, each ends after 30 minutes at the latest);
- the plugin or container is not on the `updates` list, or no container was created from the image. Unraid itself is refused.

The program starts after `cmd_result` is sent. Give the broker a user that may write `cmd` only where you want it, and keep the broker on your LAN.
