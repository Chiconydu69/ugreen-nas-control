---
type: feat
---

MQTT: a display or Home Assistant can update Docker containers with the setting `mqtt_control_containers="1"` (off by default, separate from `mqtt_control_updates`); the publisher finds the container by the image Unraid lists, checks its name and starts Unraid's own update_container script.
