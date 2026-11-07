# Decisions

One open next step, then the ideas that were looked at and not taken. Reopen a declined idea only with new facts.

## Open: one daemon (ugreend) that owns all hardware

Today the fan daemon, the LED daemon and the PHP APIs reach the hardware through separate processes of the one `ugreen-nas` binary and take locks to avoid clashing. A single daemon, `ugreend`, would own the fan chip and the LED controller and answer a local API to the web UI and the CLIs. It maps to Unraid 8's declarative services ([unraid-8.md](unraid-8.md#preparation-we-can-do-now)). Needs verification on hardware (the DXP6800 Pro and the DXP4800 GT) before any code moves; `backend/internal/hw` and `backend/internal/led/chip` are the shared pieces it would build on.

## No kernel module of our own

Decided. The plugin builds and ships no kernel module; it talks to the hardware from user space (I/O ports for the Super-I/O and EC, `/dev/mem` for MMIO, `/dev/i2c-*` and hwmon where a kernel driver exists). In-tree modules Unraid already ships (`i2c_i801`, `i2c-dev`) may be loaded; nothing is compiled.

Why: Unraid has no package manager or DKMS, and every release (point releases too) ships a new kernel, so a module has to be rebuilt and published per release; when that lags, LEDs and fans stop after an Unraid update. A user-space bug ends one process; a module bug can hang the NAS. Removal leaves nothing in the kernel.

Cost: small parts of drivers live in our code (EC protocols, the DesignWare I2C controller over `/dev/mem` on the DXP4800 GT and the DXP2800 SUB2), polled rather than interrupt-driven.

Rule: never race a kernel driver. When a driver owns the device, use its interface; direct access only when no driver is bound, and refuse when one is (`chip/smbus_linux.go` `findBus`; the LPSS path in PR #225 follows the same rule).

Exception (DXP2800 SUB2): the MFD parent `intel-lpss` bound to the PCI function while no I2C driver exists below it (Unraid ships `intel-lpss-pci.ko` but no `i2c_designware`). Nothing uses the controller then, so the rule is not about racing a driver. Accepted only when no MFD child has a driver and no I2C adapter exists below the function; the plugin then holds `power/control` at `on` while it works and restores the saved value (`chip/lpss_pm_linux.go`, [LED chip](led.md)). Any other driver, or any child driver or adapter, is refused as before.

Depends on: no kernel lockdown. Unraid 7.3.4 (kernel 6.18.58-Unraid) has `# CONFIG_STRICT_DEVMEM is not set` and no lockdown LSM (`/usr/src/linux-6.18.58-Unraid/config` on the maintainer box). `STRICT_DEVMEM` alone would still allow our accesses (it blocks RAM, not unclaimed device MMIO); lockdown (integrity or confidentiality) blocks `/dev/mem` and I/O ports and would stop fan, watchdog, EC and the `/dev/mem` LED paths.

Revisit if: Unraid enables lockdown or `IO_STRICT_DEVMEM`; Unraid ships drivers for this hardware in its kernel (then use them); the plugin model changes with Unraid 8.

Watched by: the kernel canary (`just kernel-canary [version]`, `.github/workflows/kernel-canary.yml`, every Monday). It reads `.config` from the newest `<ver>-Unraid` release of ich777/unraid_kernel (Lime Technology publishes no kernel repo) and checks the options in `scripts/kernel-canary-rules.ts`: `DEVMEM`, `X86_IOPL_IOPERM`, `I2C_CHARDEV`, `I2C_I801` present, `IO_STRICT_DEVMEM` and lockdown (forced level or `lockdown` in `CONFIG_LSM`) absent. A broken rule opens one issue per kernel version labelled `bug`; a change of `STRICT_DEVMEM`, the DesignWare I2C and LPSS options or `SENSORS_IT87` opens one labelled `enhancement`. The run also fails when an option name is missing from the Kconfig of the kernel series, or the archive has no `./.config`.

## Rust rewrite of the Go tools

Verdict: none for `backend/internal/fan` and `backend/internal/led/chip`.

- The Go code has no unsafe port I/O: `backend/internal/hw/ports_linux.go` reaches I/O ports through `/dev/port` with `ReadAt`/`WriteAt`, and `backend/internal/led/chip/smbus_linux.go` uses raw ioctl and `flock`. A Rust version would add `unsafe` blocks where Go has none.
- Size and start time do not matter: the daemon is a poll loop, the binaries sit on a flash disk.
- Go already gives `-race`, `go vet`, linting, zero third-party dependencies, and green tests. The bugs found in review were in the shell scripts.
- The tests (about 1.9k lines, built on a `Ports` fake) would all be rewritten, and behaviour on the hardware only a few people own cannot be checked in CI.
- Contributors on Unraid and NAS projects know bash, PHP and Go far more often than Rust; a second toolchain also slows CI.
- Go already covers every backend part (one module, one CI).

## release-please and conventional-commit bots

Needs conventional commits on squash titles and writes the `.plg` badly (entities, MD5 lines). `.changes/` fragments plus `plugin/tools/changes.ts` are smaller and keep the changelog text user-facing.

## Next channel in Community Applications

CA reviews every entry by hand and shows `PluginURL` as the update source; the next URL is a rolling build hosted on GitHub Pages ([releasing.md](releasing.md)). The next channel is published in the forum thread and the README. If it is ever wanted: a separate wrapper XML under `plugins/` with its own PluginURL and `<Beta>true</Beta>` (CA's own flag name).

## Biome, Prettier, tsc, Playwright

The frontend uses Bun for formatting (`bun fmt`), type checking (`bun check`), tests and bundling, and `Bun.WebView` for screenshots. One tool, nothing to install beyond Bun, no Node.

## Python

Scripts and tools are TypeScript on Bun; there are no Python helpers. Icon PNGs are built from the SVGs in `web/assets/` by `just web-build` (see `web/assets/README.md`).

## Paid ads, YouTube outreach, a Discord server

Effort without proof of demand. Support stays in the Unraid forum thread and GitHub issues.

## iDX6011 Pro touch LCD dashboard

Declined for now. Driving the front LCD needs an EFI entry, an i915 overlay and a disabled BIOS watchdog (otherwise the box resets about every 2 minutes); that is too fragile for a plugin that runs as root on a NAS. Only the EC backlight (register 0xA5) is in scope. Source: noodlemctwoodle/unraid-ugreen-idx6011-pro README and src/panel/backlight.h, see [competition.md](competition.md). UGREEN's own drivers (`drivers/ugreen`@c75c5ab, [third-party.md](third-party.md#ugreen-open-source-drivers)) contain no display, touch or graphics code, only the backlight (`ug_it55pro_functions.c:636-711`), so they do not change this. Revisit only if UGREEN publishes the panel software.

## Unraid API or guessed manifest formats as Unraid 8 plugin host

The Unraid API (GraphQL, Nest) is not shown to be the Unraid 8 plugin path, and the manifest format is not public. Revisit when [unraid-8.md](unraid-8.md#what-is-known) lists official plugin docs.

## UGREEN GPL drivers as primary hardware source; facts with citation, no code; no clean room

Verdict: read `drivers/ugreen` in [ugreen-opensource](https://github.com/ugreen-opensource) first, community projects second ([third-party.md](third-party.md#ugreen-open-source-drivers)). Take facts (chip IDs, addresses, bits, scaling, sequences) and cite `file:line@commit`; copy or port no code, because that would make the MIT binary GPL-2.0. A clean-room process adds nothing: facts are not copyrightable and our Go is written from them. On a conflict UGREEN's value wins unless we measured otherwise on hardware; the measurement is cited next to the value.
