---
type: fix
---

Diagnostics no longer wait 30 s on graphics I2C buses: the scan skips them, lists each as skipped, keeps the SMBus result when it still times out, and `diag --full-scan` scans them too.
