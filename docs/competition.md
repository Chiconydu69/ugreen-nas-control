# Related projects

Other community projects cover parts of what this plugin does. `?` means not verified. Which of them write the same chips: [conflicts.md](conflicts.md). Licences and what may be reused: [third-party.md](third-party.md#licenses-and-reuse).

## Projects

| Project                                                                                                   | Scope and hardware                                                                       | License                           |
| --------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | --------------------------------- |
| [FanCtrl Plus 2](https://github.com/andrebrait/fanctrlplus) (fork of ck9393)                              | Unraid fan control, any hwmon `pwm`, no UGREEN-specific data                             | GPL-2.0-only                      |
| [FanCtrl Plus](https://github.com/ck9393/fanctrlplus)                                                     | Unraid fan control, any hwmon `pwm`                                                      | none stated                       |
| [Dynamix Auto Fan Control](https://github.com/bergware/dynamix)                                           | Unraid, hottest disk to one hwmon fan                                                    | none stated                       |
| [UGREEN-DXP-FAN-NAS-Driver](https://github.com/IT-Kuny/UGREEN-DXP-FAN-NAS-Driver)                         | it87 fan daemon: DXP2800/4800/8800, iDX6011 (Pro); README lists GT models as unsupported | GPL-3.0                           |
| [ich777 it87-driver](https://github.com/ich777/unraid-it87-driver)                                        | Unraid it87 hwmon driver, no policy                                                      | MIT                               |
| [ich777 ugreenleds-driver](https://github.com/ich777/unraid-ugreenleds-driver) (forks by hanssep, guyp2k) | LEDs on DX4600, DX4700, DXP2800/4800/6800/8800, 480T; kernel module per Unraid release   | MIT                               |
| [EllipticSet UGREEN-DXP4800Pro-LEDs](https://github.com/EllipticSet/UGREEN-DXP4800Pro-LEDs)               | LEDs, DXP4800 Pro only                                                                   | MIT (GPL-2.0 module in `vendor/`) |
| [flybrys UGREEN-DXP4800GT-LED-Driver](https://github.com/flybrys/UGREEN-DXP4800GT-LED-Driver)             | LEDs, DXP4800 GT only                                                                    | MIT (GPL-2.0 module in `vendor/`) |
| [noodlemctwoodle unraid-ugreen-idx6011-pro](https://github.com/noodlemctwoodle/unraid-ugreen-idx6011-pro) | iDX6011 Pro: touch LCD, LEDs, four fan curves                                            | MIT, with GPL-2.0 parts           |
| [omgapuppy unraid-ugreen-idx6011-led-fan](https://github.com/omgapuppy/unraid-ugreen-idx6011-led-fan)     | iDX6011 / iDX6012: LEDs, read-only fan tach                                              | MIT                               |
| [miskcoo ugreen_leds_controller](https://github.com/miskcoo/ugreen_leds_controller)                       | LED protocol, CLI, kernel module and services for Debian, TrueNAS, Proxmox               | MIT                               |
| [0x556c79 install_ugreen_leds_controller](https://github.com/0x556c79/install_ugreen_leds_controller)     | Installer for miskcoo's controller (TrueNAS/Proxmox side)                                | GPL-3.0                           |
| [andrewle8 ugreen-dxp4800-thermal-fix](https://github.com/andrewle8/ugreen-dxp4800-thermal-fix)           | Guide: BIOS SmartFan values, repaste, governor for DXP4800+                              | none stated                       |

## Features

Columns: **US** this plugin; **FC2** FanCtrl Plus 2; **FC1** FanCtrl Plus; **DAF** Dynamix Auto Fan; **ITK** UGREEN-DXP-FAN-NAS-Driver; **KM** kernel-module LED plugins (ich777, hanssep, guyp2k); **EG** Elliptic and flybrys; **IDX** the two iDX6011 projects; **MSK** miskcoo. `y` yes, `n` no, `~` partial, `?` unverified, `-` not in scope.

| Feature                                                | US      | FC2 | FC1 | DAF | ITK | KM  | EG  | IDX | MSK |
| ------------------------------------------------------ | ------- | --- | --- | --- | --- | --- | --- | --- | --- |
| Multi-point fan curves                                 | y       | n   | n   | n   | ~   | -   | -   | ~   | -   |
| Drives UGREEN fan chips without an extra kernel driver | y       | n   | n   | n   | n   | -   | -   | y   | -   |
| Several fans with separate inputs                      | ~       | y   | y   | y   | ~   | -   | -   | ~   | -   |
| Disk temperatures without waking disks                 | y       | y   | y   | y   | ?   | -   | -   | ~   | -   |
| NVMe as fan input                                      | y       | y   | y   | y   | ?   | -   | -   | n   | -   |
| GPU, HBA, NIC or custom script as fan input            | y       | y   | n   | n   | n   | -   | -   | n   | -   |
| Failsafe to 100 percent, BIOS values restored          | y       | ~   | n   | n   | y   | -   | -   | y   | -   |
| Stall watchdog and notification                        | y       | ~   | n   | n   | y   | -   | -   | y   | -   |
| Fan calibration (rpm per duty)                         | y       | n   | n   | n   | n   | -   | -   | n   | -   |
| Fan speed history graph                                | y       | y   | n   | n   | n   | -   | -   | ~   | -   |
| Fan settings export and import                         | y       | y   | n   | n   | ~   | n   | n   | n   | -   |
| Fan schedules, PID, mover or parity override           | ~       | n   | n   | n   | n   | -   | -   | n   | -   |
| Per-LED colour, brightness, effects                    | y       | -   | -   | -   | -   | ~   | y   | y   | y   |
| Disk-activity LEDs                                     | y       | -   | -   | -   | -   | y   | y   | y   | y   |
| SMART, standby and parity states on LEDs               | y       | -   | -   | -   | -   | ~   | ~   | ~   | ~   |
| Network LED (speed, traffic, gateway)                  | y       | -   | -   | -   | -   | y   | y   | ~   | y   |
| Night mode (LEDs)                                      | y       | -   | -   | -   | -   | ~   | y   | ~   | n   |
| Identify a bay                                         | y       | -   | -   | -   | -   | n   | y   | n   | ~   |
| LCD, touch screen, backlight                           | n       | -   | -   | -   | -   | n   | n   | y   | n   |
| Dashboard tile on Unraid                               | y       | y   | y   | n   | n   | n   | n   | y   | n   |
| Case view per model                                    | y       | n   | n   | n   | n   | n   | ~   | y   | n   |
| No kernel module built per Unraid release              | y       | y   | y   | y   | n   | n   | n   | n   | n   |
| Languages                                              | 7       | n   | n   | n   | n   | n   | n   | n   | n   |
| CLI or API                                             | ~       | n   | n   | n   | ~   | n   | ~   | n   | y   |
| In Community Apps                                      | not yet | ?   | y   | y   | n   | y   | ?   | ?   | n   |
