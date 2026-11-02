---
type: fix
---

Fans: stopping or restoring now waits for a busy tool instead of giving up, and a slow controller, a hung helper or a broker restart no longer leaves fans, LEDs, MQTT status or state files in a bad state.
