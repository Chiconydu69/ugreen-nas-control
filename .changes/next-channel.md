---
type: chore
---

Next channel: every push to `next` publishes a rolling test build `X.(Y+1).0-next.NNNNNN` (install URL `https://ugreen-nas-control.github.io/ugreen-nas-control/next/ugreen-nas-control.plg`), replacing the beta channel; it installs over stable in place, going back to a lower version needs `plugin install <url> forced`.
