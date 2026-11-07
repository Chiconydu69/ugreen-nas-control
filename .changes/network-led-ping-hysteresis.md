---
type: fix
---

Network LED: a single lost gateway ping no longer turns it red. The probe pings twice, and the LED turns red only after three failed probes in a row (about 90 s); a lost link or carrier is still red at once. Reported by [Chiconydu69](https://github.com/Chiconydu69).
