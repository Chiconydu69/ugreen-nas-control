---
type: fix
---

Sensors: the ACPI thermal zone, a fixed BIOS value of about 28 °C on UGREEN boards, is no longer shown as the mainboard temperature; the Sensors page lists it as "ACPI zone, not a measurement" and diag marks it unreliable. Diag also points to the Feedback page for "Confirm my device" and no longer lists the SPI flash controller as an I2C controller without driver. Reported by [ivolution](https://github.com/ivolution).
