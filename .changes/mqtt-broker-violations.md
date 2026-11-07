---
type: fix
---

MQTT: the plugin closes the connection and reconnects when the broker sends a packet the protocol forbids, and answers a QoS 2 message with PUBREC and PUBCOMP.
