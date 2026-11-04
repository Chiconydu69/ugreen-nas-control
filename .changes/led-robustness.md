---
type: fix
---

LEDs: DXP2800 boards that reject all LED commands are reported once and no longer retried in a loop, the DXP2800 picks its LED frame by the BIOS system version, and the network LED stops blinking when the LED daemon stops.
