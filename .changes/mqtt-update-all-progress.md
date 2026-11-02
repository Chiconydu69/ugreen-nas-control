---
type: feat
---

MQTT: a display can update all containers with an update in one go (`target` `containers`) and every command reports its progress in `cmd_result` (`step`, `total`, `name`, a last answer with `done`); an updating container is also found when `docker ps` shows its image without a tag, or with `docker.io/` or `library/` in front.
