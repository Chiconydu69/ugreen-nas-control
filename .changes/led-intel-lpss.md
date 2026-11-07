---
type: feat
---

DXP2800 SUB2 boards: LED control through the Intel LPSS I2C controller, off until you set `lpss_leds="1"` in `leds.cfg`; `ugreen-led-i2c lpss-probe` checks the board first (not yet confirmed on hardware).
