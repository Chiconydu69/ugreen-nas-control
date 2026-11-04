# plugin/

`ugreen-nas-control.plg` is generated. Do not edit it, and do not touch it in a feature PR.

- `ugreen-nas-control.plg.tmpl`: the .plg with placeholders, including the install and remove scripts.
- `files.txt`: every shipped file (`source dest mode`; `release:<name>` = binary from `.build/bin/`). The one Go binary is `backend/ugreen-nas`; the four tool names are links to it, lines of the form `release:<tool> <dest> -> <target>`. The packer, the harness and the template read the same list. Sources under `web/plugin` that are not tracked (bundles, PNG icons) come from `just web-build`.
- `bun plugin/tools/plg.ts pack [--version X.Y.Z] [--next --run N] [--out DIR]` builds the web files and writes `ugreen-nas-control-<version>.tar.xz`: every file of `files.txt` at its install-relative path, binaries from `.build/bin/`; the default output is `.build/dist/`. The archive is reproducible (sorted entries, fixed mtime, owner root): the same sources give the same SHA256.
- `bun plugin/tools/plg.ts render [--version X.Y.Z] [--next --run N] [--out DIR]` writes `ugreen-nas-control.plg` with SHA256 and MD5 of that archive, the only download. `just plg` runs both.
- `bun plugin/tools/plg.ts check`: manifest and tree agree, the archive has exactly the manifest's files, links and modes, the tool names and the event hook keep their install paths, it is reproducible, both channels render, XML is well-formed.
- The change list comes from `../CHANGELOG.md` (latest 20 releases); fragments in `../.changes/` feed it at release.

## Versions

Unraid compares plugin versions as text (`strcmp` in `dynamix.plugin.manager/scripts/plugin`), so `5.0.10` sorts below `5.0.9`. The scheme lives in `tools/version.ts` (tests: `tools/version.test.ts`); policy in [docs/releasing.md](../docs/releasing.md#versions).

- Stable: `X.Y.Z`, one digit each, always three parts. After `5.0.9` comes `5.1.0`, after `5.9.x` comes `6.0.0`. Headings up to `4.x` are two-part and count as `X.Y.0`.
- Next: `X.(Y+1).0-next.NNNNNN`, X.Y.Z = newest stable in `CHANGELOG.md`, N = CI run number, zero-padded to six digits. With `5.0.3` newest: `5.1.0-next.000017` sorts above every `5.0.Z` and below `5.2.0`, and next builds sort by run.
- Local test build: `X.Y.Z.tYYYYMMDDHHMM` (Z always written).
- Both channels use the plugin `name` `ugreen-nas-control` (the Plugins page looks up icon and README by it); both install as `ugreen-nas-control.plg` (Unraid names the file after the URL basename) and switch in place; `-next.` in the version marks the next channel. The settings page reads the version from that file.
- Stable installs from `main`'s `.plg` (raw URL). Next installs `ugreen-nas-control.plg` from the GitHub Pages folder `/next/` (`website.yml`).

## Install and remove

- The archive is cached per version in `/boot/config/plugins/ugreen-nas-control/<version>/`. Its listing is checked first (only files, folders and links; no absolute path, no `..`), then it is unpacked (`tar -xJm --no-same-owner --no-same-permissions`, so files get the install time) into a hidden staging folder, checked against the expected file and link list, and swapped in with `mv`. Any error fails the install and leaves the running version untouched. Daemons are stopped before the swap and started after. No Slackware package tools are used.
- Only cache folders named like a version are pruned.
- Stable and next refuse to install while the other is installed. Switch: remove one on the Plugins page (settings stay), then install the other.
- Remove keeps `fan.cfg`, `leds.cfg`, `fan_rpm.json`, `led.enabled` in `/boot/config/plugins/ugreen-nas-control/`. Wipe them with `rm -rf /boot/config/plugins/ugreen-nas-control`.
- `min="7.2.0"`. The loader supports `min` on the plugin and `Min`/`Max` on a `<FILE>`.
