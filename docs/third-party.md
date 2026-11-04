# Third-party notices

## Credits

Maintainer: [Alexander Schubert](https://github.com/alexanderschubert). Contributor (merged code): [j3n57h0m45](https://github.com/j3n57h0m45): DXP4800 GT support (fans, LEDs, case and System view), UX overhaul, fan measuring, CI.

Prior work this project builds on; licenses and what is used follow below.

- [miskcoo/ugreen_leds_controller](https://github.com/miskcoo/ugreen_leds_controller): LED protocol and bay mapping.
- [noodlemctwoodle/unraid-ugreen-idx6011-pro](https://github.com/noodlemctwoodle/unraid-ugreen-idx6011-pro) and [omgapuppy/unraid-ugreen-idx6011-led-fan](https://github.com/omgapuppy/unraid-ugreen-idx6011-led-fan): iDX6011 bay order, EC registers, LED frames.
- [flybrys/UGREEN-DXP4800GT-LED-Driver](https://github.com/flybrys/UGREEN-DXP4800GT-LED-Driver) and [EllipticSet/UGREEN-DXP4800Pro-LEDs](https://github.com/EllipticSet/UGREEN-DXP4800Pro-LEDs): LED controller buses.
- [IT-Kuny/UGREEN-DXP-FAN-NAS-Driver](https://github.com/IT-Kuny/UGREEN-DXP-FAN-NAS-Driver) and [smashah/ugreen-dxp-fan-cli](https://github.com/smashah/ugreen-dxp-fan-cli): fan chips and PWM channels.
- [UGREEN open-source drivers](https://github.com/ugreen-opensource) (`kernel-6.12` `drivers/ugreen`): register facts for fans, LEDs, watchdog, buttons and beeper (see UGREEN open-source drivers).
- Geoff Davis, [UGREEN Published Their GPL Sources](https://geoffdavis.com/posts/ugreen-gpl-sources-verified/#the-part-nobody-asked-for-oem-hardware-drivers): requested the sources from UGREEN under the GPL, checked the release and pointed to the OEM hardware drivers in it.
- UGREEN LED Control (Unraid plugin by ivolution): the Spanish LED texts.
- UGREEN stock driver `ug_4800gt-sio.ko` (DXP4800 GT): where the EC watchdog registers were first derived from; the published source `ug_it55_4800gt_functions.c` now confirms them (see UGREEN open-source drivers). Design: pet-only, never arm ([watchdog.md](watchdog.md)).

## UGREEN open-source drivers

Primary source for register facts; the community projects below are secondary. UGREEN publishes its GPL kernel sources in the GitHub account [ugreen-opensource](https://github.com/ugreen-opensource). Read at `kernel-6.12` commit `c75c5abd60dd45f45966720293ce1ee505fb8615`, folder `drivers/ugreen`. Files carry `SPDX-License-Identifier: GPL-2.0-only` (the repository `COPYING` is `GPL-2.0 WITH Linux-syscall-note`; GitHub's GPL-3.0 badge is a classifier error).

| Model                                       | Chip                           | Files                                                                               |
| ------------------------------------------- | ------------------------------ | ----------------------------------------------------------------------------------- |
| DXP2800, DXP4800, DX4600, DX4700            | ITE IT8613 (`0x8613`)          | `ug_it86x-sio.c`                                                                    |
| DXP4800 GT                                  | ITE IT5571                     | `ug_it55_4800gt_functions.c`, `it55_helper.c`                                       |
| iDX6011 Pro                                 | ITE IT5571                     | `ug_it55pro_functions.c`                                                            |
| iDX6011, iDX6012                            | ITE IT5571                     | `ug_it55_functions.c`                                                               |
| DXP2800 GT                                  | Chipsea CS201x (`0x2011`)      | `ug_201x.c` (fan, temperature, watchdog), `leds-sio-201x.c`                         |
| DXP480T                                     | IT86x + N76E003 LED MCU        | `dxp480t/ug_it86x-cpufan.c`, `leds-mcu-n76e003.c`                                   |
| DH2600                                      | IT86x + N76E003 LED MCU        | `dh2600/ug_it86x-sio-dh2600.c`, `leds-mcu-n76e003-dh2600.c`                         |
| DXP6800, DXP8800 (LEDs)                     | LED MCU                        | `dxp68a88/leds-mcu-68a88.c`                                                         |
| DXP2800, DXP4800, Plus, DXP6800, iDX (LEDs) | LED MCU                        | `leds-mcu.c`, `leds-mcu-28a48.c`                                                    |
| all                                         | buttons, beeper, register dump | `ug_gpio_btn.c`, `ug_sataio_beeper.c`, `tools/it55_regs_dump.sh` (IT5571 registers) |

- Product list: `include/misc/ugreen_product.h` (also in the repository `linux-headers-6.12.30`).
- ARM models (DH2300, DH4300 Plus): repositories `rk3576_kernel` and `rk3588_kernel`, folder `drivers/ugreen`. Not supported by this plugin.
- Independent check of the release: Geoff Davis, [UGREEN Published Their GPL Sources](https://geoffdavis.com/posts/ugreen-gpl-sources-verified/) (2026-07-03): the LED protocol of miskcoo matches `leds-mcu-28a48.c`; UGOS uses the stock `it87_wdt` watchdog driver on IT8613E models; no byte-for-byte comparison of the published drivers with the shipped modules exists.

## Hardware facts from community plugins

The iDX6011 / iDX6012 LED and fan handling uses facts (bay order, model names, the power LED start-up sequence, EC fan and backlight registers) read from these MIT-licensed
repositories. No code is copied; the source comments in `backend/internal/led/` and `backend/internal/fan/` name the file, line and commit.

| Repository                                                                                                                                                                                 | Commit               |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------- |
| [noodlemctwoodle/unraid-ugreen-idx6011-pro](https://github.com/noodlemctwoodle/unraid-ugreen-idx6011-pro) (iDX6011 Pro)                                                                    | `d202a7c`            |
| [omgapuppy/unraid-ugreen-idx6011-led-fan](https://github.com/omgapuppy/unraid-ugreen-idx6011-led-fan) (iDX6011 / iDX6012)                                                                  | `6196dc0`            |
| [flybrys/UGREEN-DXP4800GT-LED-Driver](https://github.com/flybrys/UGREEN-DXP4800GT-LED-Driver), [EllipticSet/UGREEN-DXP4800Pro-LEDs](https://github.com/EllipticSet/UGREEN-DXP4800Pro-LEDs) | `32c06cf`, `ebed5bd` |

All four are MIT licensed ("MIT License", Copyright (c) 2026 noodlemctwoodle (both iDX repositories), flybrys, EllipticSet respectively). The MIT terms
require the copyright and permission notice only for copies of the software or substantial portions of it; if code from them is ever
copied, add their full license text here first.

## miskcoo/ugreen_leds_controller

The LED protocol and the bay mapping follow [miskcoo/ugreen_leds_controller](https://github.com/miskcoo/ugreen_leds_controller) (MIT).

Code or data copied from miskcoo would need this notice, which applies to the protocol description in `backend/internal/led/chip/protocol.go`:

```
MIT License

Copyright (c) 2024 miskcoo@gmail.com

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

The miskcoo issue threads (#6, #22, #93, #100, #121, #122) are cited in `web/models/*.ts` and `backend/internal/led/chip/protocol.go` as evidence that models work; user reports only.

## Facts only, no code

This project is MIT. From UGREEN's GPL drivers we take facts only: chip IDs, ports, register addresses, bit meanings, scaling, channel roles, sequences, timeouts. Each fact is cited as `file:line@commit` in the Go comment and in this document. We do not copy or line-by-line port code; that would make the binary GPL-2.0. No clean-room process is needed: reading the vendor source and writing our own Go is the method. `backend/internal/fan/cs201x.go` and `backend/internal/fan/ec.go` already work this way.

| Project                                                                                   | License                         | What we use                                                                                                                                                                                         |
| ----------------------------------------------------------------------------------------- | ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [IT-Kuny/UGREEN-DXP-FAN-NAS-Driver](https://github.com/IT-Kuny/UGREEN-DXP-FAN-NAS-Driver) | GPL-3.0                         | chip id `0x5571` for the iDX6011 Pro (`README.md:21-22`), IT8613E at `0xa30` on the DXP2800 (`README.md:262`, @5c2d850); `backend/internal/fan/ec.go`, `backend/internal/fan/fanmodels.go`; no code |
| [smashah/ugreen-dxp-fan-cli](https://github.com/smashah/ugreen-dxp-fan-cli)               | MIT (Copyright (c) 2026 M SHAH) | DXP4800 Plus drives its fans on `pwm2` and `pwm3` of the IT8613E; `backend/internal/fan/fanmodels.go`; no code                                                                                      |
| `vendor/` kernel modules in EllipticSet and flybrys, GPL parts of noodlemctwoodle         | GPL-2.0                         | nothing; not read for code                                                                                                                                                                          |

## Licenses and reuse

| Project                                                                                        | License                | What may be reused                                                                      |
| ---------------------------------------------------------------------------------------------- | ---------------------- | --------------------------------------------------------------------------------------- |
| miskcoo, ich777 drivers, Elliptic, flybrys (own parts), noodlemctwoodle (own parts), omgapuppy | MIT                    | Code, with the copyright and license notice kept (see [third-party.md](third-party.md)) |
| FanCtrl Plus 2, IT-Kuny, 0x556c79                                                              | GPL-2.0-only / GPL-3.0 | Facts only (register addresses, protocol layouts, file formats); no code                |
| `vendor/` kernel modules in Elliptic, flybrys; GPL parts of noodlemctwoodle                    | GPL-2.0                | Facts only                                                                              |
| FanCtrl Plus, Dynamix, andrewle8                                                               | none stated            | Facts only; no code (no license means all rights reserved)                              |

Facts such as addresses, formats and chip behaviour are not copyrightable; the entries below are re-derived from the hardware or from MIT code and credited.

## Facts read from other projects

Sources are file:line at the commit named after `@`.

| Fact                                                                                                                                                                                                                                                                                    | Source                                                                                                                                                                                                                                                                                       | Our code                                                                                            |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| DXP4800 GT LED MCU: DesignWare I2C adapter, address `0x3a`, register `0x5a` reads `0xc5b2`                                                                                                                                                                                              | flybrys `src/ugreen-gt-leds:69,79-80`@32c06cf; UGREEN: `leds-mcu.c:41,59,719,770`@c75c5ab                                                                                                                                                                                                    | `backend/internal/led/chip/protocol.go:120-131`, `dwmem_linux.go:17-22`                             |
| DXP4800 Pro LED MCU on the Intel I801 SMBus, same chip id, legacy 12-byte frames                                                                                                                                                                                                        | EllipticSet `src/ugreen-pro-leds:129-135`, `src/preflight.sh:46-49`@ebed5bd; UGREEN: `leds-mcu-28a48.c:17,179,211,867,920`@c75c5ab                                                                                                                                                           | `backend/internal/led/chip/protocol.go:106-117`                                                     |
| Bays on `ata1`-`ata4` for GT and Pro                                                                                                                                                                                                                                                    | flybrys `src/ugreen-gt-leds:29`@32c06cf                                                                                                                                                                                                                                                      | `backend/internal/fan/hardware.go:82-85`, `web/models/dxp4800-gt.ts`                                |
| Non-Pro iDX: power LED register 0, LAN 1, bays 2-7; checksum `0xa1` + command + parameters, big-endian                                                                                                                                                                                  | omgapuppy `src/monitor.sh:118-127`@6196dc0; UGREEN: `leds-mcu.c:98-108,211-214`@c75c5ab; the Pro has a second LAN LED at id 2 (`netdev2` in `ugreen-led-i2c`)                                                                                                                                | `backend/internal/led/chip/protocol.go:133-146`                                                     |
| IT5571 EC at ports `0x62`/`0x66`, read command `0x80`, write command `0x81`, IBF bit 1, OBF bit 0                                                                                                                                                                                       | UGREEN `it55_helper.h:9-18`, `it55_helper.c:93-123`@c75c5ab (also noodlemctwoodle `src/panel/ec.h:21,29`@d202a7c; omgapuppy `src/idx6011-fan-status.c:25-28`@6196dc0)                                                                                                                        | `backend/internal/fan/ec.go:14-30`                                                                  |
| iDX6011 Pro fans: tach `0x34/35` (cpu1), `0x36/37` (cpu2), `0x38/39` (sys1), `0x3A/3B` (sys2); mode/duty `0xB0/B1`, `0xB2/B3`, `0xB4/B5`, `0xB6/B7`; 198 = 100 percent; fan4 shows fan3's value while it reads 0                                                                        | UGREEN `ug_it55pro_functions.c:24-40,61,64,290-291`, `tools/it55_regs_dump.sh:5-12,30-37`@c75c5ab (chip id `0x5571` only in IT-Kuny `README.md`@5c2d850; UGREEN names "ITE5571", `ug_it55pro_functions.c:7`)                                                                                 | `backend/internal/fan/ec.go`                                                                        |
| Plain iDX6011/iDX6012 fans (two system fans): mode `0x9C`/`0x9E`, duty `0x9D`/`0x9F`, tach `0x96/97`, `0x98/99`; read only here                                                                                                                                                         | UGREEN `ug_it55_functions.c:24-31`@c75c5ab                                                                                                                                                                                                                                                   | `backend/internal/fan/ec.go` (`ecSpecIdx`)                                                          |
| iDX6011 Pro SATA order: Intel PCH (`0x8086`) = bays 3,4; ASMedia ASM1164 (`0x1b21`) = bays 1,2,5,6. Non-Pro: ASMedia = 1-4, Intel = 5,6                                                                                                                                                 | noodlemctwoodle `src/monitor.sh:70-75`@d202a7c; omgapuppy `src/monitor.sh:70-75`@6196dc0                                                                                                                                                                                                     | `backend/internal/led/ledctl/disks.go:263-312` (not yet checked on a real Pro)                      |
| iDX6012 is accepted next to iDX6011; no "iDX6012 Pro" exists (the Pro driver matches `iDX6011 Pro` exactly)                                                                                                                                                                             | UGREEN `ug_it55_functions.c:584`, `ug_it55pro_functions.c:704`, `leds-mcu.c:76-78,88-90`@c75c5ab (also omgapuppy `src/idx6011-fan-status.c:74-77`@6196dc0); UGREEN: `leds-mcu.c:77-78,171-173,562-563,586`@c75c5ab                                                                           | `backend/internal/led/chip/protocol.go:311-317`, `web/models/idx6011.ts:7`                          |
| Power LED stays dark after boot although the MCU reports on: write brightness 0, wait, then the value; iDX: mode reset (command `0x04`, checksum `0xa5`) before steady on                                                                                                               | flybrys `src/ugreen-gt-leds:357-361`@32c06cf; EllipticSet `src/ugreen-pro-leds:593-597`@ebed5bd; omgapuppy `src/monitor.sh:164-181`@6196dc0; UGREEN: `leds-mcu.c:595-609`@c75c5ab (legacy: `leds-mcu-28a48.c:708-766`)                                                                       | `backend/internal/led/ledctl/powerboot.go`                                                          |
| Non-Pro iDX blink to steady on must pass through off                                                                                                                                                                                                                                    | omgapuppy `src/monitor.sh:156-160`@6196dc0; UGREEN: `leds-mcu-28a48.c:479-483`@c75c5ab, legacy frame only; the new frames send on with 0xFF (`leds-mcu.c:368-376`)                                                                                                                           | `backend/internal/led/chip`                                                                         |
| iDX6011 Pro fan floor 25 percent, critical CPU 88 / disk 60 (no UGREEN source); Pro detected by "Pro" in `product_name` (UGREEN: DMI name `iDX6011 Pro` exactly, `ug_it55pro_functions.c:704`@c75c5ab)                                                                                  | noodlemctwoodle `src/panel/fan.h:25-38,102-105`@d202a7c                                                                                                                                                                                                                                      | `backend/internal/fan/ec.go` (floor), `backend/internal/fan/fanmodels.go` (limits)                  |
| IT8613E at `0xa30` also on DXP2800 (hwmon `it8613-isa-0a30`)                                                                                                                                                                                                                            | IT-Kuny `README.md:262`@5c2d850                                                                                                                                                                                                                                                              | `backend/internal/fan/ite.go:42-46,60-102` (chip found; per-model channel table)                    |
| IT8613E fan wiring by DMI name: DXP2800 and DXP4800 drive header 3 (case); DXP4800 Plus/Pro cpu 2, case 3; DXP8800 Plus cpu 2, case 3+4; DXP480T Plus cpu 3, case 2+4. Header 4 control register `0x1e` (ours `0x7f`, unconfirmed). Tach count `0x0fff` = no reading                    | UGREEN kernel-6.12 (GPL-2.0-only) `drivers/ugreen/ug_it86x-cpufan.c:82-83,221-275,382-388`, `ug_it86x-sio.c:226-236`@c75c5ab                                                                                                                                                                 | `backend/internal/fan/fanmodels.go` (`probeHints`), `ite.go`, `ite_headers.go`; facts only, no code |
| iDX6011 Pro backlight: EC register `0xA5`, range 0..198, 1 = brightest, 198 = dimmest, 0 = vendor default; on the plain iDX `0xA5` is the beeper; touch panel needs the BIOS watchdog off (community only)                                                                              | UGREEN `ug_it55pro_functions.c:640,652,675-676`, `tools/it55_regs_dump.sh:25`, `ug_sataio_beeper.c:35`@c75c5ab; noodlemctwoodle `README.md` (watchdog section)@d202a7c; the plain iDX6011 uses 0xA5 as beeper, the Pro 0x9E (UGREEN `ug_it55_functions.c`, `ug_it55pro_functions.c`@c75c5ab) | `backend/internal/fan/ec_backlight.go`                                                              |
| EC watchdog: DXP4800 GT and iDX6011 Pro enable `0x98`, timeout `0xA2/A3` (big-endian seconds, 1..10000, default 600), read-back as counter; plain iDX6011/6012 enable `0x55`, timeout `0x56/57`; armed only when userspace opens `/dev/watchdog`, keepalive rewrites timeout and enable | UGREEN `ug_it55_4800gt_functions.c:529-536,550-560,557-584,615-640`, `ug_it55pro_functions.c:521-527`, `ug_it55_functions.c:466-472`@c75c5ab                                                                                                                                                 | `backend/internal/fan/ec_watchdog.go`, `backend/internal/fan/ec.go` (`ecWdt`)                       |
| DXP2800: UGREEN's driver serves exactly `DXP2800` with the no-ID SMBus frame; we try the legacy frame first and fall back to it when it is not acked (miskcoo#128)                                                                                                                      | UGREEN `leds-mcu.c:69-92,558-559,627-633`@c75c5ab                                                                                                                                                                                                                                            | `backend/internal/led/chip/frames.go`                                                               |
| Blink on/off >= 100 ms, breath on >= 1000 ms and off >= 500 ms                                                                                                                                                                                                                          | UGREEN `leds-mcu-28a48.c:485-500`, `leds-mcu.c:378-386`@c75c5ab                                                                                                                                                                                                                              | `backend/internal/led/chip/protocol.go`                                                             |
| Front-panel power/reset buttons: per-model pad registers, polled every 50 ms                                                                                                                                                                                                            | UGREEN `ug_gpio_btn.c:39,73-115,248-471`@c75c5ab                                                                                                                                                                                                                                             | `backend/internal/panel/buttons.go`                                                                 |
| Beeper: PC speaker (0x43/0x42/0x61), iDX6011/6012 EC 0xA5, iDX6011 Pro EC 0x9E, DH2600/DXP480T Plus pad register                                                                                                                                                                        | UGREEN `ug_sataio_beeper.c:23-35,63-79,126-171,299-311,535-583`, `dx4700/ug_sata_beep-dx4700.c:164-189`, `dh2600/ug_sata_beep-dh2600.c:42,164-182`, `dxp480t/ug-beeper-dxp480t.c:41-49,118-134`@c75c5ab                                                                                      | `backend/internal/panel/beeper.go`                                                                  |

## ITE and Linux it87 register layout

Header 1 of the IT8613E (tach `0x0d`/`0x18`, duty `0x63`, control `0x15`) follows the pattern of the headers 2 to 4 verified on the DXP6800 Pro, in `backend/internal/fan/ite_headers.go`. The Linux `it87` driver (GPL-2.0) is not cited in the code and is not a source.

Other projects surveyed in [competition.md](competition.md) (FanCtrl Plus, Dynamix, andrewle8, ich777, 0x556c79) supply no facts or code to this repository.

## Development tooling

`.claude/skills/frontend-design/` (Apache-2.0) is developer tooling and not part of the plugin; its `LICENSE.txt` stays with it.

## Fonts

[Archivo](https://github.com/Omnibus-Type/Archivo) (Copyright 2020 The Archivo Project Authors, SIL Open Font License 1.1), the variable woff2 of the latin subset from `@fontsource-variable/archivo` 5.3.0, self-hosted by the Pages site in `docs/site/fonts/`; the licence text is `docs/site/fonts/OFL.txt`.

## Trademarks

UGREEN and Unraid are trademarks of their owners. This project is not affiliated with either.
