---
type: fix
---

Network LED: the gateway check looks up the router in the system's address table instead of pinging it, so a router that ignores pings no longer turns the LED red. Thanks to [Chiconydu69](https://github.com/Chiconydu69).
