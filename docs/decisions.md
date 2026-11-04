# Decisions

One open next step, then the ideas that were looked at and not taken. Reopen a declined idea only with new facts.

## Open: one daemon (ugreend) that owns all hardware

Today the fan daemon, the LED daemon and the PHP APIs reach the hardware through separate processes of the one `ugreen-nas` binary and take locks to avoid clashing. A single daemon, `ugreend`, would own the fan chip and the LED controller and answer a local API to the web UI and the CLIs. It maps to Unraid 8's declarative services ([unraid-8.md](unraid-8.md#preparation-we-can-do-now)). Needs verification on hardware (the DXP6800 Pro and the DXP4800 GT) before any code moves; `backend/internal/hw` and `backend/internal/led/chip` are the shared pieces it would build on.

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

Verdict: read `drivers/ugreen` in [ugreen-opensource](https://github.com/ugreen-opensource) first, community projects second ([third-party.md](third-party.md#ugreen-open-source-drivers)). Take facts (chip IDs, addresses, bits, scaling, sequences) and cite `file:line@commit`; copy or port no code, because that would make the MIT binary GPL-2.0. A clean-room process adds nothing: facts are not copyrightable and our Go is written from them.
