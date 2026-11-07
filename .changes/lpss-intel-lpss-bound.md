---
type: feat
---

LEDs on DXP2800 SUB2: the LPSS path also works when Unraid's intel-lpss driver holds the controller without an I2C driver; the plugin keeps the controller awake while it talks to it. Reported by DanielRuppen.
