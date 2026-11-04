# Documentation

One line per file: what it answers. One topic per file; a topic that appears elsewhere links here.

## Using the plugin

- [README.md](../README.md) — What the plugin does, how to install it, which models are verified.
- [docs/faq.md](faq.md) — Is my model supported, where to ask, what to send with a support request, the disclaimer.
- [docs/hardware.md](hardware.md) — What each verification status means, the per-model evidence, how to add a model.
- [docs/cli.md](cli.md) — Which commands exist (`ugreen-nas-fan`, `ugreen-nas-ctl`), what `status` and `report` print, how to report a model.
- [docs/mqtt.md](mqtt.md) — How to publish status to MQTT or Home Assistant, and which commands a display may send.
- [docs/unraid.md](unraid.md) — Which Unraid and PHP versions are supported, how a release is tested against them.
- [docs/unraid-8.md](unraid-8.md) — What is known about Unraid 8 plugins and what would start the port.

## How it works

- [docs/architecture.md](architecture.md) — Which parts exist (Go binary, PHP API, web UI), how they talk, repo layout, how to add a feature.
- [docs/fan.md](fan.md) — How fan chips are read and driven, fan settings and pipeline order, safety rules, fan history, the wiring probe.
- [docs/led.md](led.md) — How the LED controller is driven and what the LED tool and backend do.
- [docs/panel.md](panel.md) — How the front-panel buttons are read and the beeper is driven, per model, vendor source only.
- [docs/watchdog.md](watchdog.md) — Why the hardware watchdog stays on and what the plugin does with it.
- [docs/power-on-ac.md](power-on-ac.md) — What the power-on-after-AC-loss setting is, per model, and how to read and set it.
- [docs/conflicts.md](conflicts.md) — Which other tools write the same chips and what the plugin does about them.
- [backend/internal/led/ledctl/SPEC.md](../backend/internal/led/ledctl/SPEC.md) — Which LED tool behaviour the Go port must keep (parity contract).
- [plugin/README.md](../plugin/README.md) — What is in the plugin package, how the archive and `.plg` are built, install and remove.
- [web/assets/README.md](../web/assets/README.md) — Where the icon sources are and how the PNGs are built.

## Developing

- [docs/development.md](development.md) — Setup, `just` recipes, generated files, code style, tests, adding a model, testing on a real NAS.
- [docs/harness.md](harness.md) — How to run the fake Unraid, the screenshots, the e2e test and the site preview.
- [docs/translations.md](translations.md) — Which languages exist, the key rule, how to add a language.
- [.github/CONTRIBUTING.md](../.github/CONTRIBUTING.md) — How to send a change: branch, PR, changelog fragment, rules, what CI checks.
- [docs/releasing.md](releasing.md) — How a maintainer releases, which branch is which, how versions are written, the next channel.
- [docs/decisions.md](decisions.md) — The open next step and which ideas were declined, so they are not raised again.

## Reference

- [docs/third-party.md](third-party.md) — Credits, licences, UGREEN's open-source drivers as hardware source, where borrowed facts come from, the MIT notice.
- [docs/competition.md](competition.md) — How other community projects compare.
- [CHANGELOG.md](../CHANGELOG.md) — What changed in each release (generated).
- [.github/SECURITY.md](../.github/SECURITY.md) — How to report a vulnerability.
- [.github/CODE_OF_CONDUCT.md](../.github/CODE_OF_CONDUCT.md) — Community standards.
