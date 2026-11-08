# Front panel: buttons and beeper

`ugreen-nas panel` reads the power and reset button and sounds the beeper. Both come from UGREEN's open-source drivers (GPL-2.0-only, `drivers/ugreen/` at c75c5ab); the code is our own, only the facts are taken ([third-party.md](third-party.md)). **Every entry below is vendor source. Which model users confirmed is the `panel` check of the model in `web/models/` ([hardware.md](hardware.md)); the DXP8800 Plus (issue #165) and, for the beeper only, the DXP4800 Plus (issue #266).** `panel buttons` prints that state behind the model name, `diag` shows it on the `panel.buttons` and `panel.beeper` lines, the Front panel page and the About card show it. Needs root. No press triggers an action unless you switch on the shutdown on a held power button ([below](#shut-down-on-a-held-power-button)); that is off by default and the reset button is never used.

## Commands

| Command                                      | What it does                                                                                                                                                           |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ugreen-nas panel buttons`                   | Prints whether power and reset are held.                                                                                                                               |
| `ugreen-nas panel buttons --watch`           | Polls every 50 ms and prints `down`, `press`, `up`, `release` with the hold time, until Ctrl-C.                                                                        |
| `ugreen-nas panel beep [--freq Hz] [--ms N]` | Sounds the beeper for N ms (default 200, 20 to 2000). `--freq` (100 to 8000) only on PC speaker models.                                                                |
| `ugreen-nas panel power-action [--dry-run]`  | Runs until the power button has been held for the hold time, then shuts Unraid down (the watcher of the setting below). `--dry-run` only prints `would shut down now`. |

An unknown model is refused before anything is opened.

## Buttons

Read-only: `/dev/mem` is opened read-only and one page-aligned page is mapped per register; the code has no write path. A button is bit 1 of a 32-bit pad register (bit 16 on the GT models). Power is active high and reset active low, except on the DXP2800 GT where both are active low. Source: `ug_gpio_btn.c:248-471`.

| DMI product_name                                     | Power register                 | Reset register     |
| ---------------------------------------------------- | ------------------------------ | ------------------ |
| `DXP2800`, `DXP2800S`, `DXP4800`, `DXP4800S` (exact) | 0xFD6A0980                     | 0xFD6D0B50         |
| `DXP4800 Plus*`, `DXP4800 Pro*`                      | 0xFD6A0980                     | 0xFD6D0A10         |
| `DXP6800*`, `DXP8800*`, `FORT 6*`                    | 0xFD6A0B40                     | 0xFD6D0A10         |
| `DXP480T Plus*`                                      | 0xFD6E0810                     | 0xFD6E0770         |
| `iDX6011*`, `iDX6012*`                               | 0xE0D20860                     | 0xE0D508A0         |
| `DH2600*`                                            | 0xFD6D0B20                     | 0xFD6E0800         |
| `DX4600*`, `DX4700*`                                 | 0xFD6D0690                     | 0xFD6D0890         |
| `DXP4800 GT` (exact)                                 | 0xFED81524, bit 16             | 0xFED81520, bit 16 |
| `DXP2800 GT` (exact)                                 | 0xFED81510, bit 16, active low | 0xFED8150C, bit 16 |

`*` = prefix match, as the vendor driver does. `DXP6800 Ultra` and `DXP8800 Ultra` are newer than the vendor source and are refused. The vendor reads the DXP4800 GT at the unaligned addresses 0xFED81526 and 0xFED81522, bit 0; the tool reads the aligned word, which is the same bit.

Unreadable registers: a pad register that reads as 0xFFFFFFFF is not a pad value (real pad registers have reserved bits that read 0); it is what a bus read nobody answers returns. The tool then reports that button as unreadable instead of released: `panel buttons` prints `power: not readable on this box (GPIO registers read as 0xFF; the BIOS hides them)` and `power unreadable, reset unreadable`, `--watch` exits with an error, the API (`panel_buttons`) returns `"unreadable"` for that button, the Front panel page shows "Not readable on this box" and `diag` marks `panel.buttons` as not readable. On the DXP8800 Plus (BIOS 5.27, Unraid 7.3.4) both registers read as 0xFF on some boots; on others the same addresses return real values (issue #165: 0x84000100 from 0xFD6A0B40, power and reset events seen). The cause is unknown. The vendor driver maps and reads the same fixed addresses without any P2SB setup that `/dev/mem` lacks (`ug_gpio_btn.c:129@c75c5ab`). On boots where the registers read 0xFF the button state is not available. Confirmed by a user on 5.4.0 (issue #165): the pads read power 0x84000100 at 0xFD6A0B40 and reset 0x84000102 at 0xFD6D0A10, and `panel buttons --watch` showed power down, press and up (held 0.20 s) twice and reset down and up (held 0.40 s).

Vendor press rules, which `--watch` follows:

- Reset: a press is reported after 260 polls (13 s; the vendor comment says 5 s), the release only if the press was reported.
- Power: a press is reported on the first poll (the embedded controller forwards it), on the DXP2800 GT after 100 polls (5 s). A release is reported only after the button was down for 100 polls, so a short press has `down` and `up` but no `release`.
- The DXP2800 GT driver clears bit 23 of each register at start (pin to input). The tool does not write, so it reads the pins as the firmware left them.
- iDX6011/6012: the vendor comment says the power key sits on ACPI and cannot be read by address, yet it lists an address. Treat the power reading there as doubtful.

## Shut down on a held power button

Opt-in, off by default (issue [#165](https://github.com/ugreen-nas-control/ugreen-nas-control/issues/165)). On the Front panel page the card "Shut down on a held power button" has a switch ("Shut down Unraid when the power button is held for X s") and a hold time of 1 to 3 s. Switching it on asks first and shows a warning. The card appears only for a model whose registry `panel` check is `community` or `verified` (today the DXP8800 Plus); a `partial` or `expected` model does not get it, and the watcher refuses to start there as well.

Settings in `fan.cfg` (the page writes them, `api.php` action `panel_power`): `panel_power_shutdown="1"` switches it on, `panel_power_hold="1.5"` is the hold in seconds (1 to 3; a missing or out-of-range value is 1.5).

### Watcher

`ugreen-nas panel power-action` is the watcher. `ugreen-nas-ctl start` and `power-start` run it detached (pid file `/var/run/ugreen-nas-control-power.pid`, log `/var/log/ugreen-nas-control-power.log`) only when the setting is on, the same way as the MQTT publisher: a small process of its own, not part of the fan daemon, because the fan daemon does not run where there is no fan controller and the LED daemon only runs if LEDs are switched on, while the buttons exist independently of both. The plugin's start script (`start_daemons` in the `.plg`) calls `power-start` also when the fan probe fails. `ugreen-nas-ctl stop` and `power-stop` end it. It reads the pad registers read-only through the panel `Tracker` (`NewPowerTracker` with the hold in polls as press threshold) and takes no hardware lock: pad reads touch no chip that the fan or LED code writes.

### Guards

- Off unless `panel_power_shutdown="1"`; on a model without confirmed buttons the command refuses to run.
- Only after a hold: the Tracker must see the power pad held on every 50 ms poll for the whole hold time (1.0 to 3.0 s); a release or an unreadable poll starts the count again. It fires while the button is still held, not on release.
- An unreadable read (a pad word of 0xFFFFFFFF on power or reset, or a read error) is no action and restarts the count.
- The first 60 s after the start are ignored (boot glitches on the pads). A hold that began inside that minute does not count either; the button has to be seen released first.
- One shot: after the action the process ends; nothing is queued, a second hold does nothing. If the shutdown command fails the failure is logged and the process ends.
- Syslog (tag `ugreen-nas-control`) gets a line, and an Unraid notification "Shutdown requested by front power button" is raised right before the shutdown command is called.
- The reset button is never read for this.

`ugreen-nas panel power-action --dry-run` runs the same logic (also with the setting off, but not on an unconfirmed model) and only prints `would shut down now`:

```
model: DXP8800 Plus (confirmed by users, #165)
dry run, nothing is shut down: a hold of 1.5 s ends with the line below (the first 60 s after the start are ignored)
12:01:23.450 power held 1.5 s
would shut down now
```

### Hold time

The vendor driver reports a power press on the first 50 ms poll after the embedded controller forwards it (`seen == 1`, `ug_gpio_btn.c:88-93@c75c5ab`; on the DXP2800 GT at `seen == 100`, 5 s). A release is reported only after 100 polls (`:108-112`), and a reset press only after 260 polls (`:77-79`; the `seen == 100` branch at `:75` is empty). So the vendor treats a tap as a press; a deliberate hold has no vendor meaning below 5 s. The driver reports them only as key codes of its own input device `ug gpiokey`: KEY_F20 (192, "power") and KEY_F24 (194, "reset") (`:163-164,170-171`), not KEY_POWER, and it emits no uevent (`uevent_next_seqnum` is declared at `:41` and never called). What UGOS does with those keys is not in the source. A tap in #165 was down for 0.20 s (4 polls).

Many boards force the power off at about 4 s of hold (the ACPI power button override). The clean shutdown has to start well before that, and well after a tap: the default is 1.5 s (30 polls); the range is 1 s (20 polls, five times a tap) to 3 s (60 polls, a second of margin to 4 s). The "5 to 10 s" a user reported in #165 is unverified and is not used; at that length the board would already have cut the power. Polls can be late but never early, so a late poll only lengthens the hold.

### Shutdown command

The command is `/sbin/poweroff -n`, the same call the Unraid web interface makes for its Shutdown button (`/usr/local/emhttp/plugins/dynamix/include/Boot.php:48`, posted from `ArrayOperation.page:900-903`, Unraid 7.3.4). `/sbin/poweroff` is `halt`; it hands over to init, whose runlevel 0 script `/etc/rc.d/rc.6` stops the services and the md driver, syncs and unmounts (`rc.6`, "Clean shutdown" when `mdState=STOPPED`). Unraid's ACPI power-button handler ends in `/sbin/init 0` (`/etc/acpi/acpi_handler.sh`), which is the same path. `/usr/local/sbin/powerdown` still exists but logs "has been deprecated" and runs `/sbin/init 0`; it is not used. If Settings > Power Options > Power Button is set to Shutdown and the board delivers the button as an ACPI event, a tap already shuts down; this setting is for boxes where the front button does not reach the OS that way.

Not yet tested on hardware: the hold on a DXP8800 Plus, and whether the box has its registers readable at that boot (see below).

## Beeper

| DMI product_name                                                                                                           | Backend                 | Source                                                                     |
| -------------------------------------------------------------------------------------------------------------------------- | ----------------------- | -------------------------------------------------------------------------- |
| `DXP2800`, `DXP2800S`, `DXP4800`, `DXP4800S`, `DXP4800 Plus*`, `DXP4800 Pro*`, `DXP6800*`, `DXP8800*`, `FORT 6*`, `DX4700` | PC speaker              | `ug_sataio_beeper.c:63-79,305-311`, `dx4700/ug_sata_beep-dx4700.c:164-189` |
| `iDX6011`, `iDX6012` (exact)                                                                                               | EC register 0xA5        | `ug_sataio_beeper.c:26-35,126-171,299-304`                                 |
| `iDX6011 Pro` (exact)                                                                                                      | EC register 0x9E        | `ug_sataio_beeper.c:33,301-303`                                            |
| `DH2600*`                                                                                                                  | pad register 0xFD6E08B0 | `dh2600/ug_sata_beep-dh2600.c:42,164-182`                                  |
| `DXP480T Plus*`                                                                                                            | pad register 0xFD6E07E0 | `dxp480t/ug-beeper-dxp480t.c:41-49,118-134`                                |

No other model has a beeper path (DXP4800 GT, DXP2800 GT, DX4600, Ultra models): refused.

The beeper test is confirmed on the DXP8800 Plus by a user, who heard the sound right after the click (issue [#165](https://github.com/ugreen-nas-control/ugreen-nas-control/issues/165)) and on the DXP4800 Plus ([#266](https://github.com/ugreen-nas-control/ugreen-nas-control/issues/266)).

- PC speaker: timer command 0xB6 to port 0x43, divisor low and high byte to port 0x42, bits 0-1 of port 0x61 set to start and cleared to stop (through `/dev/port`). The vendor divisor is 0x0944 (about 503 Hz) and is the default; `--freq` sets 1193182 / Hz instead.
- EC register: ACPI EC ports 0x62 (data) and 0x66 (command); write command 0x81, read command 0x80; the tool writes 1 or 0, reads back, retries up to 5 times and accepts a read-back of 2 after writing 1 (as the vendor does). It only ever writes the one register of the model. On the iDX6011 Pro 0xA5 is the backlight, so the Pro uses 0x9E and the tool never writes 0xA5 there.
- EC beep guards (same read-only checks as the fan code): refused while a rival fan writer runs (`conflict` package, severity block), while the kernel's ACPI EC driver is bound (`PNP0C09:*/driver` under `/sys/bus/acpi/devices`), and when the chip id behind ports 0x4E/0x4F is not 0x5571 (on the plain iDX6011/6012 a foreign id is only noted, as in the fan code). Every EC exchange (the chip id read, each write plus read-back) runs under the flock shared with the fan daemon and the LED backend, `UGREEN_FAN_LOCK` (default `/var/run/ugreen-nas-control.lock`, same in every binary). The lock is taken per exchange: on, off and each retry, never held while the beep sounds. The PC speaker ports (0x42, 0x43, 0x61) and the pad words are used by no other tool here and take no lock.
- Pad register (`/dev/mem` read-write, one word): bits 10..8 forced to 010, bit 0 set to start and cleared to stop, other bits kept. DXP480T comment: whole register 0x84000201 on, 0x84000200 off.
- The tone of the EC and pad beepers is fixed. `--freq` is refused there.
- The beeper is switched off after the time, on error, and on SIGINT/SIGTERM. A SIGKILL cannot.
- Not touched: the BIOS boot-beep switch (CMOS register 0x66 through ports 0x70/0x71), because it changes a BIOS setting.

## Open on hardware

- DXP4800 Plus buttons (issue #266): the pad registers read real values (power 0xFD6A0980 = 0x04000100, reset 0xFD6D0A10 = 0x84000102) but the owner saw no button response; a `panel buttons --watch` test is requested.
- Why the DXP8800 Plus pad registers read 0xFF on some boots only (issue #165), whether other models do the same, and whether they can be mapped on the running kernel (`STRICT_DEVMEM`, a driver that claimed the range) and whether the bit meaning and levels above hold on each model.
- The shutdown on a held power button on a real DXP8800 Plus: that the 1.5 s hold fires with the button still down and the array stops.
- Whether a speaker is wired on each PC speaker model, and whether the EC and pad beepers sound.
- The power button on iDX6011/6012 and the 5 s press on the DXP2800 GT.
