# Rival fan and LED tools

Two programs that write the same fan or LED chip give no stable result. The plugin detects the known ones and yields. The projects and their licences: [competition.md](competition.md).

## Which tools reach the same hardware

- **FanCtrl Plus (2) or Dynamix Auto Fan with an it87 hwmon driver** (the ich777 it87 plugin, or `ugreen-it87.plg`): they write `pwmN` through the kernel driver, this plugin writes the IT8613E registers through `/dev/port`. Both reach the same chip, so the speed alternates between two targets. Super-I/O index/data access (`0x2e`/`0x2f`) can also interleave with the kernel driver's own access.
- **UGREEN-DXP-FAN-NAS-Driver on a GT or iDX6011 Pro**: its it5571 backend uses the same EC as `backend/internal/fan/ec.go`.
- **Kernel-module LED plugins, the DXP4800 Pro / GT LED plugins, the iDX6011 plugins, or the standalone UGREEN LED Control**: all write the LED controller; the last write wins.
- FanCtrl Plus 2 states in its README that it does not run beside FanCtrl Plus for the same reason.

## Detection

`backend/internal/fan/conflict` finds other tools that can write the same chips; read only, every path injectable (`UGREEN_PLUGINS_BOOT`, `UGREEN_PROC_MODULES`, `UGREEN_SYS_MODULE`, `UGREEN_SYS_HWMON`, `UGREEN_PROC`). `ugreen-nas-fan conflicts` prints the list; `status` carries it as `conflicts[]` plus `conflict`.

| Kind     | Source                                                                                                                                      | Severity |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------- | -------- |
| `daemon` | `/proc/*/cmdline`: FanCtrl Plus (2) loop, `autofan`, `ugreen-fan-control`, `ugreen-gt-leds`, `ugreen-pro-leds`, `ugreen-leds`, `panel_dash` | block    |
| `plugin` | `/boot/config/plugins/<name>.plg` or `/usr/local/emhttp/plugins/<name>/` of the known rival plugins                                         | warn     |
| `module` | `it87`, `led_ugreen`, `leds_sio_201x`, `it87_wdt` and UGREEN's own modules (below) in `/proc/modules` or `/sys/module`                      | warn     |
| `hwmon`  | `it8613`/`it5571` device with `pwmN_enable` = 1 (manual)                                                                                    | warn     |

UGREEN's vendor modules (`drivers/ugreen`, names as loaded), all `warn`:

| Module                                                                                        | Controls           |
| --------------------------------------------------------------------------------------------- | ------------------ |
| `ug_it86x_sio`, `ug_it86x_sio_dh2600`, `ug_it86x_cpufan`                                      | fan                |
| `ug_idx6011_sio`, `ug_idx6011pro_sio`                                                         | fan, watchdog      |
| `ug_201x`, `ug_sio201`                                                                        | fan, led, watchdog |
| `leds_mcu`, `leds_mcu_28a48`, `leds_mcu_68a88`, `leds_mcu_n76e003`, `leds_mcu_n76e003_dh2600` | led                |

`ug_sataio_beep` and `ug_gpio_btn` (beeper, buttons) write nothing we write and are not listed.

## What the plugin does

- **Watchdog:** a loaded `it87_wdt` (the kernel's IT8613E watchdog driver, `controls: ["watchdog"]`) is not a fan rival: it never blocks. The IT8613E watchdog petter yields to it, with no Super-I/O access at all (module load rewrites the watchdog config, and `/dev/port` would race its muxed-region lock); `status.watchdog.state` is `yielded`. The EC petters ignore it.
- **Fans:** a `block` finding makes the daemon read only: no `SetPWM`, no BIOS restore at stop, `set` and `calibrate` refuse. Temperatures, rpm and `daemon.conflict` stay current. When the rival ends, the curves resume on the next tick.
- **Emergency:** the daemon does not answer an emergency with 100 % while a rival runs, since that would overwrite the rival every 2 s. It logs it, sends one Unraid alert per episode and sets `daemon.unapplied`; the web page shows the reason in the banner. Only a running writer counts: an installed plugin, a loaded `it87` or manual pwm mode (the BIOS leaves it too) is shown and never makes the daemon yield.
- **LEDs:** `api.php` sets `led.blocker = rival` while a rival LED tool runs and ours is off. A running LED part is not stopped by itself; the banner offers to switch it off. `ugreen-nas-ctl` (switch on, boot) and the LED status daemon (at start and every 30 s) call the detector: a rival LED plugin or a running rival LED process blocks the switch, and the daemon exits and leaves the LEDs to it.
- **Web:** a banner on every page per running rival with "Use UGREEN NAS Control" (link to the Plugins page; other plugins are never removed from here) and "Keep <rival>, turn off our fan/LED control" (the existing switches). Settings lists every finding. Harness: `UNC_SCENARIO=conflict` (or `conflict-led`) for `web/tools/harness/serve.ts` and `shots.ts`.
