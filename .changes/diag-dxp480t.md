---
type: fix
---

Diagnosis on the DXP480T Plus: no false LED controller mismatch (the power LED chip at 0x26 is checked instead of 0x3a), no LED or fan chip hints for hardware the model does not have, "power LED only" instead of an LED count, and an unknown M.2 slot order is no longer a mismatch. Thanks to [datarepos76](https://github.com/datarepos76) for the report.
