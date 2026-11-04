# Front panel: buttons and beeper

`ugreen-nas panel` reads the power and reset button and sounds the beeper. Both come from UGREEN's open-source drivers (GPL-2.0-only, `drivers/ugreen/` at c75c5ab); the code is our own, only the facts are taken ([third-party.md](third-party.md)). **Every entry below is vendor source, not yet confirmed on hardware.** Needs root. No press triggers an action; nothing reboots or shuts down.

## Commands

| Command                                      | What it does                                                                                            |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `ugreen-nas panel buttons`                   | Prints whether power and reset are held.                                                                |
| `ugreen-nas panel buttons --watch`           | Polls every 50 ms and prints `down`, `press`, `up`, `release` with the hold time, until Ctrl-C.         |
| `ugreen-nas panel beep [--freq Hz] [--ms N]` | Sounds the beeper for N ms (default 200, 20 to 2000). `--freq` (100 to 8000) only on PC speaker models. |

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

Vendor press rules, which `--watch` follows:

- Reset: a press is reported after 260 polls (13 s; the vendor comment says 5 s), the release only if the press was reported.
- Power: a press is reported on the first poll (the embedded controller forwards it), on the DXP2800 GT after 100 polls (5 s). A release is reported only after the button was down for 100 polls, so a short press has `down` and `up` but no `release`.
- The DXP2800 GT driver clears bit 23 of each register at start (pin to input). The tool does not write, so it reads the pins as the firmware left them.
- iDX6011/6012: the vendor comment says the power key sits on ACPI and cannot be read by address, yet it lists an address. Treat the power reading there as doubtful.

## Beeper

| DMI product_name                                                                                                           | Backend                 | Source                                                                     |
| -------------------------------------------------------------------------------------------------------------------------- | ----------------------- | -------------------------------------------------------------------------- |
| `DXP2800`, `DXP2800S`, `DXP4800`, `DXP4800S`, `DXP4800 Plus*`, `DXP4800 Pro*`, `DXP6800*`, `DXP8800*`, `FORT 6*`, `DX4700` | PC speaker              | `ug_sataio_beeper.c:63-79,305-311`, `dx4700/ug_sata_beep-dx4700.c:164-189` |
| `iDX6011`, `iDX6012` (exact)                                                                                               | EC register 0xA5        | `ug_sataio_beeper.c:26-35,126-171,299-304`                                 |
| `iDX6011 Pro` (exact)                                                                                                      | EC register 0x9E        | `ug_sataio_beeper.c:33,301-303`                                            |
| `DH2600*`                                                                                                                  | pad register 0xFD6E08B0 | `dh2600/ug_sata_beep-dh2600.c:42,164-182`                                  |
| `DXP480T Plus*`                                                                                                            | pad register 0xFD6E07E0 | `dxp480t/ug-beeper-dxp480t.c:41-49,118-134`                                |

No other model has a beeper path (DXP4800 GT, DXP2800 GT, DX4600, Ultra models): refused.

- PC speaker: timer command 0xB6 to port 0x43, divisor low and high byte to port 0x42, bits 0-1 of port 0x61 set to start and cleared to stop (through `/dev/port`). The vendor divisor is 0x0944 (about 503 Hz) and is the default; `--freq` sets 1193182 / Hz instead.
- EC register: ACPI EC ports 0x62 (data) and 0x66 (command); write command 0x81, read command 0x80; the tool writes 1 or 0, reads back, retries up to 5 times and accepts a read-back of 2 after writing 1 (as the vendor does). It only ever writes the one register of the model. On the iDX6011 Pro 0xA5 is the backlight, so the Pro uses 0x9E and the tool never writes 0xA5 there.
- EC beep guards (same read-only checks as the fan code): refused while a rival fan writer runs (`conflict` package, severity block), while the kernel's ACPI EC driver is bound (`PNP0C09:*/driver` under `/sys/bus/acpi/devices`), and when the chip id behind ports 0x4E/0x4F is not 0x5571 (on the plain iDX6011/6012 a foreign id is only noted, as in the fan code). Every EC exchange (the chip id read, each write plus read-back) runs under the flock shared with the fan daemon and the LED backend, `UGREEN_FAN_LOCK` (default `/var/run/ugreen-nas-control.lock`, same in every binary). The lock is taken per exchange: on, off and each retry, never held while the beep sounds. The PC speaker ports (0x42, 0x43, 0x61) and the pad words are used by no other tool here and take no lock.
- Pad register (`/dev/mem` read-write, one word): bits 10..8 forced to 010, bit 0 set to start and cleared to stop, other bits kept. DXP480T comment: whole register 0x84000201 on, 0x84000200 off.
- The tone of the EC and pad beepers is fixed. `--freq` is refused there.
- The beeper is switched off after the time, on error, and on SIGINT/SIGTERM. A SIGKILL cannot.
- Not touched: the BIOS boot-beep switch (CMOS register 0x66 through ports 0x70/0x71), because it changes a BIOS setting.

## Open on hardware

- Whether the pad registers can be mapped on the running kernel (`STRICT_DEVMEM`, a driver that claimed the range) and whether the bit meaning and levels above hold on each model.
- Whether a speaker is wired on each PC speaker model, and whether the EC and pad beepers sound.
- The power button on iDX6011/6012 and the 5 s press on the DXP2800 GT.
