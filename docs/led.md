# LEDs

How the plugin drives the LEDs of DX, DXP and iDX models. Off until the user switches them on in the plugin.

## LED controller

All DX, DXP and iDX models share the same LED controller protocol, after [miskcoo/ugreen_leds_controller](https://github.com/miskcoo/ugreen_leds_controller) (credits: [third-party.md](third-party.md)). Bays map to LEDs by SATA port.

- The LED part is offered when the DMI product name starts with `DX`, `DXP` or `iDX`, and not while the plugin _ugreenleds-driver_ or the kernel module `led_ugreen` holds the controller.
- Transport: SMBus (`/dev/i2c-N`), or `/dev/mem` on the DXP4800 GT (AMD DesignWare I2C). Protocol and frames: `backend/internal/led/chip/protocol.go`.
- DXP480T Plus: no controller at 0x3a, only a power LED on a simpler chip (0x26) that flashes white until it gets the steady-white init (`ugreen-led-ctl dxp480t`, includes a short orange flash as in ich777's driver). `ugreen-nas-ctl led-*` boot sends it once instead of probing, only while LED control is on; with it off the LED stays untouched, as on every model. The model has 0 bays.
- Frames (`chip/frames.go`): DX/DXP models take the 12-byte I2C block write with the LED ID first; the iDX models and the DXP4800 GT an SMBus block without the ID. The DXP2800 tries the DX/DXP frame first, so boards that work today stay as they are, and when the controller does not acknowledge it (register 0x80) falls back to the iDX frame, which is the one UGREEN's own driver uses for that model (V1.2 board, [miskcoo#128](https://github.com/miskcoo/ugreen_leds_controller/issues/128)). The frame that worked last is tried first for the rest of the process. `UGREEN_LAYOUT=idx` forces the iDX frame by hand.
- Timing: blink on and off at least 100 ms; breath on at least 1000 ms, off at least 500 ms (the limits UGREEN's drivers apply). Shorter values are raised.
- iDX6011 Pro: DMI name exactly `iDX6011 Pro` (UGREEN lists no other Pro; any other name with "Pro" gets no iDX layout). The second LAN LED is register 2, name `netdev2` in `ugreen-led-i2c` (also part of `all`); `ugreen-led-ctl` accepts it on that product only (`list`, `all`, per-LED commands; the help text stays model-independent); the plugin pages do not offer it.
- DXP2800, DXP6800, DXP6800S (exact DMI names): the DX/DXP frame first, the iDX frame as fallback, as UGREEN's driver serves the three together (`leds-mcu.c:171-173,558-561@c75c5ab`; 4 LEDs on the DXP2800, 8 on the other two). `DXP6800 Pro` and `Plus` stay on the DX/DXP frame.
- DXP2800 GT: no I2C controller, see below.
- Facts from UGREEN's GPL kernel drivers are cited as `file:line@c75c5ab` in the code and in [third-party.md](third-party.md); nothing is copied.
- If the standalone UGREEN LED Control plugin is installed too, both write the same LEDs and the last write wins ([conflicts.md](conflicts.md)).

## DXP2800 GT (CS201x)

No I2C controller: four ARGB channels (power, network, disk1, disk2) sit in the Chipsea CS201x embedded controller, reached through the host-to-EC window the fan driver uses (`backend/internal/hw/cs201x`). Registers after `ug_201x.c:102-120,375-407@c75c5ab`, channels after `leds-sio-201x.c:93-98`. UGREEN's LED driver is not in its Makefile and marks every write call TODO, so nothing here is verified on a device.

- Off by default. The backend answers only with `cs201x_leds="1"` in `leds.cfg` (the key is not in the web UI); without it every `ugreen-led-i2c` command except the dump fails with a hint. It also refuses while a UGREEN kernel module (`leds_sio_201x`, `ug_201x`, `ug_sio201`) is loaded.
- Read only: `ugreen-led-i2c cs201x` (or `ugreen-led-ctl cs201x`) prints chip id, host interface, base address, flow control and per channel mode, brightness, colour, period and duty. It never switches the host interface on and needs no opt-in.
- Writes: colour (B, R, G bytes), brightness, mode (0 off, 1 on, 2 blink, 3 breath). Blink and breath store the cycle and the on part in 50 ms units (period, duty; at most 255 units = 12.75 s), the mode last. A write switches the host interface on if it is off (`cfg 0x30 = 1`, as UGREEN does on load) and sets `ARGB_FLOW_CONTROL` (0x20005440) to 1. No effects (rainbow, temperature).
- Snapshot: before its first write the backend saves flow control and all four channels to `/var/run/ugreen-nas-control.led.cs201x` (an existing file is kept); `ugreen-led-i2c cs201x restore` writes it back and deletes it. `ugreen-led-ctl daemon stop`, a SIGTERM of the daemon and a yield to another LED tool restore it; the saved manual settings are not applied afterwards on this model.
- Locking: every register access runs under the flock `/var/run/ugreen-nas-control.lock`, the one the fan tool and daemon use per operation, so the fan driver and the LED backend (separate processes) never interleave an address latch. The lock is never nested.
- Other LED tools: the status daemon's rival detection applies as on every model (it exits and restores).

## LED tool and backend

- `ugreen-led-i2c` (`backend/internal/led/chip`): writes colour, brightness and effects to the controller.
- `ugreen-led-ctl` (`backend/internal/led/ledctl`): decides what each LED shows (status mode from `disks.ini`, SMART and link speed, night mode, alerts, shutdown signal, effects) and calls the LED tool. Its runtime state is `/var/run/ugreen-nas-control.led*`, its settings `leds.cfg`. The behaviour it must keep is [SPEC.md](../backend/internal/led/ledctl/SPEC.md).
- `web/plugin/led/api.php` runs `ugreen-led-ctl` per request.
- `ugreen-nas-ctl led-*` switches the LED part on and off and imports the settings of UGREEN LED Control ([cli.md](cli.md)).
- Shutdown: the Unraid event script `plugin/event/stopping` runs `ugreen-nas-ctl event stopping`, which tells the LED backend so the power LED can signal it.

## Open questions for device owners

| Question                                                                                 | Why                                                                                                                                                                                   | Test                                                                                                                                        |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| DXP4800 GT: little-endian or big-endian write checksum?                                  | We write little-endian (verified on a GT); UGREEN's driver writes big-endian (`leds-mcu.c:211-214@c75c5ab`) and the status read is big-endian. The MCU may ignore the write checksum. | `ugreen-led-i2c set power color 255 0 0` with each order, then `get power` and check the colour                                             |
| iDX, DXP2800, GT blink: second field on-time or off-time?                                | We send the on time, UGREEN's `leds-mcu.c:392-393@c75c5ab` sends the off time (the older driver sends on). Same result when on equals off.                                            | `blink 200 800` and watch the duty cycle                                                                                                    |
| DXP2800 V1.2: does the fallback to the iDX frame light the LEDs?                         | Untested, no V1.2 board at hand.                                                                                                                                                      | `ugreen-led-i2c set power color 0 255 0`, then `get power`                                                                                  |
| iDX6011 Pro: does `netdev2` (register 2) light the second LAN LED?                       | From UGREEN's LED table (`leds-mcu.c:104-108@c75c5ab`), untested.                                                                                                                     | `ugreen-led-i2c set netdev2 color 0 255 0`                                                                                                  |
| DXP2800 GT: what does the ARGB dump show at boot, and does a colour write light the LED? | Everything above is from UGREEN's unfinished driver.                                                                                                                                  | `ugreen-led-i2c cs201x`, then set `cs201x_leds="1"` in leds.cfg and `ugreen-led-i2c set power color 255 0 0`, `get power`, `cs201x restore` |
| DXP2800 GT: flow control 1 or 0 to take over the LEDs?                                   | UGREEN writes 1 at load and 0 at unload with the comment "disable argb led flow" (`ug_201x.c:790,817@c75c5ab`).                                                                       | Compare `argb_flow_control` in the dump before and after a colour write                                                                     |
| DXP480T Plus: address 0x26 or 0x31?                                                      | UGREEN's driver lists both (`dxp480t/leds-mcu-n76e003.c:40-43@c75c5ab`), we use 0x26.                                                                                                 | `ugreen-led-i2c scan`                                                                                                                       |
