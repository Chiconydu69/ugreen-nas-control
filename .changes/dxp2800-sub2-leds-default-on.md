---
type: feat
---

DXP2800 SUB2 boards: LED control through the Intel I2C controller is on by default, no `lpss_leds="1"` needed; the LEDs also came back by themselves after a cold reboot. `lpss_leds="0"` in `leds.cfg` turns it off. Thanks to [DanielRuppen](https://github.com/DanielRuppen).
