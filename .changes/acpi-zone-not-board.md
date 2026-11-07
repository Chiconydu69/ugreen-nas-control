---
type: fix
---

Sensors: the ACPI thermal zone, a fixed BIOS value of about 28 °C on UGREEN boards, is no longer shown as the mainboard temperature; the Sensors page lists it as "ACPI zone, not a measurement", diag marks it unreliable, and diag no longer sends users to a "Confirm my device" button that moved to the Feedback page. A SPI flash controller (00:1f.5) is no longer reported as an I2C controller without driver. Reported by [ivolution](https://github.com/ivolution).
