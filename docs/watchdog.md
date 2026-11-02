# Hardware watchdog

A watchdog (the IT5571 EC's, or the IT8613E's) resets the box when nothing feeds it. UGREEN's own driver arms the EC one only when userspace opens `/dev/watchdog` (registering the device does not arm it); the kernel's `it87_wdt` does the same on the IT8613E. Whether the BIOS arms it at boot is not proven; community reports say some UGREEN boxes reset every few minutes under another OS, and the usual fix there is switching it off in the BIOS. The plugin feeds a timer that is already armed (by the BIOS or another tool) and never arms it: a frozen box then still reboots itself.

## Behaviour

- Models: DXP4800 GT and iDX6011 Pro (both IT5571 EC, [fan.md](fan.md#fan-chips)); DXP2800, DXP4800, DXP4800 Plus, DXP4800 Pro, DXP6800 Pro and DXP8800 Plus (IT8613E, see below). Other models run no watchdog code. The plain iDX6011/iDX6012 keeps its own registers (`0x55`-`0x57`), read only: no petter, no write. There `0x98`/`0x99` is a fan tachometer, not the watchdog.
- Recommended BIOS setting: leave the watchdog armed.
- The plugin never arms it. While it is armed, the fan daemon rewrites the timeout at a third of the timeout, at least every 60 seconds. UGREEN's keepalive also sets the enable byte again; the plugin does not, and that a timeout rewrite alone reloads the counter is not proven on hardware.
- A tick that cannot get the hardware lock within 5 s skips its writes, including the feed; the next tick retries. The stop path (disarming the timer) waits up to 30 s instead. The feed gap stays far below the timeout (a third of it, 60 s at most).
- On stop the plugin returns the fans to the EC curve and clears the enable register, only if this process fed the timer. A timer somebody else armed, and `restore` (which never feeds), leave the enable byte alone.
- After a feed the counter is read again. If it does not show the value written (within 5 s), `state` is `not_effective`: the rewrite did not reload this unit's timer. Feeding goes on; the state clears when the timer is disarmed or a feed takes.
- `ugreen-nas-fan status` reports `watchdog`, with `counter` (the timeout registers read back before the feed, read only; whether it counts down is not stated by UGREEN) while armed ([cli.md](cli.md)).
- Fan code writes only fan mode and duty, the watchdog timeout, the enable byte with 0, and (iDX6011 Pro) the backlight register. The wake registers `0x99`/`0x9A` (GT/Pro), the EUP and beeper registers (`0x9C`, `0x9E`) and `0xA5` on the plain iDX (beeper) are refused in code.
- Code: `backend/internal/fan/ec_watchdog.go` (both petters), `ite_watchdog.go` (IT8613E registers).

## IT8613E

On this chip a keepalive write of a non-zero value is also the start: there is no enable bit, so rewriting a stopped timer would arm it. The plugin therefore writes only after proof that the timer counts.

- Detect (read only, one Super-I/O session under the fan lock): unlock, chip id `0x8613`, logical device 7, read `0x71`-`0x74`. Value `0` = off, nothing more happens.
- Proof: the first non-zero value is kept as a sample (with its time); on a later daemon tick, at least `1.5 s` later (seconds unit, `0x72` bit 7 set) or `61 s` later (minutes unit, bit 7 clear), the value must be lower. Nothing sleeps under the fan lock. The proof latches until the value reads 0. A value that stands still (a stopped timer with a stale value, or a register that does not count when read) is never written and never reported as armed (a stale non-zero value with the BIOS watchdog off, such as 56 minutes on a DXP6800 Pro, is normal): `state` is `not_counting` and a new sample is taken, so it is retried at every gap. A value that rose replaces the sample. Whether a read returns the live count is not stated by any source and not yet confirmed on hardware.
- Feed (same cadence as the EC: a third of the timeout, at most 60 s, default timeout 300 s): rewrite `0x72` with the value read (keeps the BIOS's reset bits), then the value LSB `0x73`, then MSB `0x74` (MSB first when the new MSB is larger than the current one, so the timer never reads 0 in between), in the unit `0x72` bit 7 selects (set = seconds, clear = minutes, rounded up). `0x71` is never written; the only writable registers are `0x72`-`0x74` of logical device 7 (`runSIO` allowlist).
- Stop: a clean daemon stop writes value 0 (config untouched), only if this process fed the timer, and first writes a marker (`/var/run/ugreen-nas-control.watchdog-armed`: config and value, tmpfs, so a reboot clears it). The next daemon start that reads the timer at 0 with the marker present writes config and value back, removes the marker and feeds as before, so a restart or upgrade does not end the protection. A marker met while the timer runs is removed unused. The marker is written only when the timer still reads non-zero right before the disarm, and the disarm happens even if the marker cannot be written; a marker met while the plugin yields to `it87_wdt` is removed. Plugin removal stops the daemon, which disarms a fed timer and deletes the marker (with the other `/var/run/ugreen-nas-control.*` files); the timer stays off until the next reboot, when the BIOS arms it again. `probe-fans restore` never touches the watchdog.
- Hard kill: if the daemon dies without a clean stop (`kill -9`, out of memory) while the BIOS timer is armed, nothing feeds it and the box resets after the timeout. Code cannot prevent that; a normal stop, restart or upgrade is safe.
- Yield: while the `it87_wdt` module is loaded the plugin accesses nothing and reports `yielded` ([conflicts.md](conflicts.md)). Loading the module rewrites the config and control registers, and the module holds the Super-I/O ports.
- Without a confirmed fan map (probe models) the daemon runs as a watchdog petter only and writes no fan register.
- Status states beyond the EC's: `yielded`, `not_counting` (also while the first proof is pending); `counter` is the 16-bit value.

## EC registers

| Model                      | Enable | Timeout (big-endian seconds, 1..10000, UGREEN default 600) |
| -------------------------- | ------ | ---------------------------------------------------------- |
| DXP4800 GT, iDX6011 Pro    | `0x98` | `0xA2`, `0xA3`                                             |
| iDX6011, iDX6012 (no feed) | `0x55` | `0x56`, `0x57`                                             |

The IT8613E models use logical device 7 of the Super-I/O instead: no enable bit, `0x73` LSB and `0x74` MSB (16-bit, seconds or minutes by `0x72` bit 7), `0x72` config, `0x71` control (read only).

Source: UGREEN's published drivers at `kernel-6.12` `c75c5ab`: `ug_it55_4800gt_functions.c:529-536,550-560,615-640`, `ug_it55pro_functions.c:521-527`, `ug_it55_functions.c:466-472` ([third-party.md](third-party.md)). IT8613E: mainline `it87_wdt.c:86-96,168-187,213-221,352-363` (GPL-2.0, identical in UGREEN's tree per Geoff Davis; facts only, no code), same commit. The GT registers were first derived from the stock driver `ug_4800gt-sio.ko`. Facts and design, no code. The design is pet-only, never arm.

## Sources

- [TrueNAS forum thread](https://forums.truenas.com/t/6910): UGREEN boxes reboot every few minutes under another OS while the BIOS watchdog is armed; the fix there is disabling it in the BIOS.
- [aloli/ugreen-hal](https://github.com/aloli/ugreen-hal) (BSD-2-Clause): the BIOS-armed watchdog and the reboot symptom; facts only.
- [miskcoo/ugreen_leds_controller issue #123](https://github.com/miskcoo/ugreen_leds_controller/issues/123) (no license stated): an independent EC watchdog map for the DXP2800 GT MCU (other registers, arms and kicks); reference only, nothing used.
- The iDX6011 Pro touch LCD needs the BIOS watchdog off; the plugin does not drive the LCD ([decisions.md](decisions.md#idx6011-pro-touch-lcd-dashboard)).
