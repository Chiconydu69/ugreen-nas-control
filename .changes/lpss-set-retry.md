---
type: fix
---

DXP2800 SUB2: a chip id of 0x0000 after the controller wakes is retried once after a fresh bring-up, the setup is checked before every transfer, and `ugreen-led-i2c --debug` prints a trace of the path.
