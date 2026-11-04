# Fan control

How `ugreen-nas-fan` reads and drives the fan chips. Unraid's kernel has no driver for them, so the tool talks to the chips through `/dev/port` (the DXP4800 GT and iDX6011 Pro through the ACPI EC ports), the way `sensors-detect` finds them. Which model has which fan wiring: `web/models/`. Commands: [cli.md](cli.md).

## Fan chips

### DXP6800 Pro, ITE IT8613E

- Super-I/O config ports `0x2e`/`0x2f` give the chip id (`0x8613`) and the base address of the environment controller (`0x0a30`).
- Fan speed comes from the tachometer counters: rpm = 1 350 000 / (2 x count).
- The BIOS leaves the PWM outputs in manual mode with a fixed duty cycle. The daemon changes it every 2 seconds following the curves.
- CPU fan on header 2, case fans on headers 3 and 4.

### DXP4800 GT, ITE IT5571 embedded controller

- Reached through the ACPI EC ports `0x62` (data) and `0x66` (command/status), under the same lock, with bounded waits and retries.
- CPU fan (fan1): mode register `0xb0`, duty `0xb1`, tachometer `0x34`/`0x35`. Case fan (fan3): `0xb4`, `0xb5`, `0x38`/`0x39`.
- Duty runs 0 to 198; the tachometer counter is the rpm. Mode 0 is the controller's own curve, 1 is manual.
- CPU temperature comes from the AMD sensor (`k10temp`), mainboard temperature from the controller.

### iDX6011 Pro, ITE IT5571 embedded controller

- Same controller as the DXP4800 GT, with four fans: tachometer `0x34`-`0x3b`, enable and duty `0xb0`-`0xb7`.
- Never below 25 % (the case fan stalls near 20 %); emergency limits 88 C CPU and 60 C disk.
- `ugreen-nas-fan backlight [set <0-100>]` reads and sets the LCD brightness (EC register `0xa5`, 1 = full, 198 = off) and touches nothing else of the display.

### DXP2800 GT, Chipsea CS201x embedded controller

- Reached through the Super-I/O window `0x2e`/`0x2f` (host-to-EC device) and the EC's indirect register window; two fans: CPU (fan1) and system (fan3 role case), duty in percent (0-100), tachometer the rpm.
- No EC automatic mode exists. `restore` writes the duty back that each fan had when the daemon started, never below 60 % (a lower or unusable value, or a snapshot taken after this tool had written a duty since boot, gives 60 %), not a curve. A duty write is read back; a mismatch after one retry is an error.
- The 25 % floor is the class default, not measured on this model; the daemon raises a lower `min_pct` to 25. The status reports it as `fan_caps.min_pct_floor`: the minimum-speed field starts there (a saved lower value shows as 25 with a hint) and calibration skips the steps below it.
- The host interface (Super-I/O cfg 0x30 bit 0) is never switched on by the fan driver: if the BIOS left it off, snapshot, writes and `confirm` refuse with "host interface off: the BIOS does not expose the fan controller" and fan control stays off. Reads only look. The enable registers are written only in bit 0 and only where it differs; the speed (two bytes) is read again until the high byte is stable.
- Control stays off until `ugreen-nas-fan cs201x confirm` saved `fan_map.json` (the IT8613E `probe-fans` commands refuse on this model); `cs201x confirm` refuses while the host interface is off (nothing could be checked) or a fan reads 0 rpm or an implausible speed (above 30000); `cs201x forget` hands the fans back (restore) and then removes it, keeping the map if that fails; `cs201x` alone only reads.

### Plain iDX6011 and iDX6012, ITE IT5571 embedded controller

- Readings only: fan1 and fan2 (tachometer `0x96`-`0x99`, mode and duty `0x9c`-`0x9f`) are shown as `readonly` in `fan_caps.kind`. No register is written, the daemon refuses to start and `set` and `calibrate` fail with a read-only error. No EC temperatures, no watchdog feed, no backlight.

The EC watchdog of the DXP4800 GT and iDX6011 Pro: [watchdog.md](watchdog.md).

## Fan settings and pipeline order

- `fan.cfg` is v1 (no `cfg_version`) or v2 (`cfg_version="2"`). A v1 file loads with groups that repeat today's globals (the group curve, `min_run` = `min_pct`, `up_step` 100 = at once, `down_step` 2, no deadband, no kick-start, quiet hours and jobs off), so it decides exactly as before; v2 keys in a v1 file are ignored. The daemon never rewrites it. The first v2 save from the web page copies the file to `fan.cfg.bak` (once) and adds `cfg_version="2"`.
- v2 keys: per group `cpu_`/`case_` + `input`, `deadband`, `start_pct`, `start_ms`, `min_run`, `up_step`, `down_step`, `on_missing` (`fallback`, `curve_low`, `hold`); the group curves stay `cpu_curve`/`case_curve`. A fan can override any of them, and the curve, as `fan.<id>.<key>` (`fan.fan3.deadband`). Quiet hours: `quiet_enabled`, `quiet_from`, `quiet_to` (HH:MM, may cross midnight), `quiet_days` (`mon,tue,...`), `quiet_cap`, `quiet_scope` (`case`, `all`). Jobs: `job_mover`, `job_parity` (`off`, `floor`, `boost`) with `job_mover_pct`, `job_parity_pct`. Invalid values warn and keep the default (`ValidateSetting` in `config_v2.go`; `web/plugin/include/fan_v2.php` checks the same table before `api.php` saves).
- Input specs: `auto` (default: the CPU for `cpu`; the hottest non-ignored disk, or NVMe minus `nvme_offset`, for `case`), `cpu`, `disk_max[:disk1+disk2]`, `disk_avg[:disk1+disk2]`, `nvme`, `gpu`, `hba`, `nic:<iface>`, `script:<name>`, `max:<spec>|<spec>` (2 to 8, no nesting).
- `gpu` reads the GPU hwmon sensor, else `nvidia-smi`. `hba` reads the hottest `mpt3sas` hwmon sensor (IOC, Board; kernel 7.3 and later), else `storcli /call show temperature` (the hottest `ROC temperature`, looked up under `/opt/MegaRAID/storcli`, `/usr/local/{sbin,bin}`, `/usr/{sbin,bin}`). storcli runs in `/var/run/ugreen-nas-control.storcli` (RAM); its `storcli.log` is deleted after every call. Both run at most every 10 s and take no more than 5 s.
- Order per tick: inputs, curve, shape (deadband, kick-start, minimum run speed, ramps), job floor or boost, quiet cap, emergency override. The emergency check runs first and its 100 % is never capped, ramped, held by a deadband or scheduled. A missing input follows `on_missing` and never leaves a group silently off; an unreadable CPU still raises `cpu_unreadable`. Script, GPU, HBA and NIC temperatures never trigger the disk emergency. The quiet cap never goes below `min_run`. Calibration pauses all of it.

## Memory

The daemons run with GC at 25 % and a 24 MiB soft memory limit (`backend/cmd/ugreen-nas/main.go`); `GOGC` and `GOMEMLIMIT` override both.

## Safety logic

- The first start after boot saves the BIOS values to `/var/run/ugreen-nas-control.bios`. Stopping the daemon (also on update or removal) and `restore` write them back; SIGTERM, SIGINT, SIGHUP, SIGQUIT and a panic in the loop all end that way. A failing output never keeps the others from their speed (an emergency 100 % reaches every fan it can).
- All fans go to 100 % when the CPU or a disk reaches its limit, when the CPU temperature cannot be read, and when the hottest non-ignored NVMe minus the NVMe offset reaches the disk limit.
- With fan control switched off in Settings the tool makes no read or write on the fan chip.
- On any model not in `web/models/` with a `fan` entry, the plugin does not touch the fan controller.
- A fan that stops turning raises a notification (Unraid notify script, killed after 10 s, at most one run per alert at a time, never under the lock). Drives can be excluded from fan control.
- Nothing slow runs under the hardware lock. Scripts, `nvidia-smi`, `storcli`, `pgrep` (3 s) and the notify script run before or outside it; the locked part of a tick is register I/O only. An emergency never waits for an input read. The shutdown signal cancels a running input read.
- Every wait for the lock (daemon, CLI, LED backend) ends after 5 s with an error: the daemon skips that tick (no write, retry on the next) and the CLI reports it. An EC that does not answer fails the first register after 3 x 0.5 s and every further call of the next second at once. The hand-back is the exception: a daemon stop, `restore` and the end of a calibration wait up to 30 s for the lock, and Restore and the watchdog disarm ignore that one-second window, so they always get the full retry budget.
- A panic while answering one history request is logged and closes that connection; the daemon and the fans are unaffected.
- Rival tools writing the same chip make the daemon read-only: [conflicts.md](conflicts.md).

## Fan history

- The daemon records every tick (2 s) into two rings in its own RAM (`backend/internal/fan/history.go`): 10 s steps for the last hour (360 points) and 60 s averages for the last 24 hours (1440 points). A point holds the CPU and hottest-drive temperature and, per fan (at most 6), speed, rpm, the driving input, its temperature and the modifiers: 56 bytes, about 100 KB in total, allocated once. Nothing is written to disk; a daemon restart starts it again.
- What drove a fan comes from the pipeline itself (`drive.go`, filled in `shape_pipeline.go`): the source (`cpu`, `disk:<name>` for the hottest drive, `disk_avg`, `nvme`, `gpu`, `hba`, `nic:<iface>`, `script:<name>`, `idle`, `missing`, `hold`, `emergency:<reason>`; `bios`, `calibration`, `rival` while the daemon does not drive) and modifier bits (quiet cap, mover, parity, minimum speed, ramp, emergency, input missing). An emergency is never averaged away.
- Reading: `ugreen-nas-fan history [1h|24h]` asks the daemon over the unix socket `/var/run/ugreen-nas-control.history.sock` (root only; `UGREEN_FAN_HISTORY` in test builds) and prints one JSON object of arrays (`web/src/core/contract-fan-history.ts`). `api.php?action=fan_history&range=1h|24h` calls it. The socket costs nothing while nobody asks; the History page asks only while it is on screen.

## Fan probe for unknown wiring

Models with an IT8613E whose header wiring is not known (DXP2800, DXP4800, DXP4800 Plus, DXP4800 Pro, DXP8800 Plus) keep fan control off until a probe result is confirmed. `ugreen-nas-fan probe-fans run` takes about a minute and only starts on that command: the daemon is paused, one output at a time goes to 80 % for at most 8 seconds while the others hold 50 %, and the tach that responds is attributed to it. It never writes below 25 %, stops on a hot CPU or disk, a fan that stops turning, or SIGTERM, and puts the previous speeds back on every exit (`probe-fans restore` does that after a killed probe). UGREEN's own open-source drivers name the expected wiring per model (`probeHints` in `fanmodels.go`: DXP2800 and DXP4800 case on header 3; Plus, Pro and DXP8800 Plus cpu on header 2; case on 3, on the DXP8800 Plus also on 4); the probe shows whether its finding agrees. That is a hint, not proof: the driver never shows which physical fan sits on which header. Header 4's control register is unconfirmed (UGREEN `0x1e`, ours `0x7f`), so header 4 is only written by the probe, and a tach count of `0x0fff` reads as no fan (like UGREEN's driver). The result is a proposal; `probe-fans confirm pwm2=cpu pwm3=case` saves it as `fan_map.json` next to `fan.cfg`, `probe-fans forget` removes it, `probe-fans tachs` reads the tach inputs without writing.
