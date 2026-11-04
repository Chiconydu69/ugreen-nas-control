# Unraid compatibility

"(unverified)" marks anything not read in an official source or in our code. Update the release table and [What we declare](#what-we-declare) for every Unraid release ([checklist](#release-checklist-per-unraid-release)). Unraid 8: [unraid-8.md](unraid-8.md).

## Supported versions

### What we declare

| Where                                       | Value             | Meaning                                                                                                            |
| ------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------ |
| `plugin/ugreen-nas-control.plg.tmpl:21`     | `min="7.2.0"`     | The plugin manager refuses install and update below it, and at boot moves the `.plg` to `plugins-error`. No `max`. |
| `plugins/ugreen-nas-control.xml` `<MinVer>` | `7.2.0`           | Community Applications hides the app below it. `just plg-check` keeps it equal to `min`.                           |
| `web/models/*.ts`                           | no Unraid version | `fixture.ts:122` (`unraid: '7.3.2'`) is sample data for the harness, not a declared range.                         |
| `web/plugin/UGREENNASDashboard.page`        | no `Cond`         | `min` already excludes every release without the 7.2 tile layout (lines 20-30).                                    |

No `max` and no `Unraid` attribute: the plugin must stay installable on a new release the day it ships. The loader compares `check_version` (the installed Unraid version, for example `7.4.0-rc.2`) with `version_compare`; a `max` below the running version fails the install and moves the `.plg` to `plugins-error` at boot ([plugin script, lines 717-730](https://raw.githubusercontent.com/unraid/webgui/master/emhttp/plugins/dynamix.plugin.manager/scripts/plugin)). `version_compare('7.4.0-rc.2', '7.4.0', '<')` is true, so a `min="7.4.0"` would also reject the release candidates.

`min` is `7.2.0` because the tile header uses the 7.2 flexbox layout ([webGUI changes](#webgui-changes-72-to-74-against-our-code), row 2); 7.0.x and 7.1.x were never tested.

### Releases and PHP

Source: [Unraid News](https://unraid.net/blog) and the release notes at `https://docs.unraid.net/unraid-os/release-notes/<version>/`.

| Release       | Date       | PHP                                                                      | Note                                                                  |
| ------------- | ---------- | ------------------------------------------------------------------------ | --------------------------------------------------------------------- |
| 7.0.0         | 2025-01-09 | 8.3.8                                                                    | older                                                                 |
| 7.1.0         | 2025-05-05 | 8.3.19                                                                   | older                                                                 |
| 7.2.0         | 2025-10-29 | 8.3.26                                                                   | responsive webGUI                                                     |
| 7.2.5 - 7.2.7 | 2026-04-30 | 8.3.29 - 8.3.31                                                          |                                                                       |
| 7.2.8         | 2026-07-29 | 8.4.23                                                                   | PHP 8.3 to 8.4 inside the 7.2 branch                                  |
| 7.2.9         | 2026-10-23 | 8.4.27                                                                   | [security update for 7.2](https://unraid.net/blog/unraid-7-2-9)       |
| 7.3.0         | 2026-05-12 | 8.4.21                                                                   | stable                                                                |
| 7.3.4         | 2026-10-22 | 8.4.27                                                                   | [latest stable](https://unraid.net/blog/unraid-7-3-4)                 |
| 7.4.0-rc.2    | 2026-10-22 | 8.4.27 ([notes](https://docs.unraid.net/unraid-os/release-notes/7.4.0/)) | newest, release candidate; no 7.4.0 stable yet (blog list shows none) |

So the plugin must run on PHP 8.3 (7.2.0 to 7.2.7) and 8.4 (7.2.8 and later). `web/plugin/api.php:16` uses the `never` return type (PHP 8.1 or newer); every row above ships 8.3 or 8.4. The flake lint uses `php84` (`flake.nix:50`). The harness runs the PHP of the dev machine (`php -S`); the 8.3 and 8.4 rows are covered by the [test matrix](#matrix).

7.4.0 stable date: unannounced (unverified). Two PHP RCE fixes (CVE-2026-109618/109619) are in PHP 8.4.27, which is why 7.3.4, 7.2.9 and 7.4.0-rc.2 shipped together; test on 8.4.27 builds, not on older 8.4.x.

### webGUI changes 7.2 to 7.4 against our code

Sources: [7.2.0 notes](https://docs.unraid.net/unraid-os/release-notes/7.2.0/), [7.3.0](https://docs.unraid.net/unraid-os/release-notes/7.3.0/), [7.3.3](https://docs.unraid.net/unraid-os/release-notes/7.3.3/), [7.4.0](https://docs.unraid.net/unraid-os/release-notes/7.4.0/), [rc.1 post](https://unraid.net/blog/unraid-7-4-0-rc1), [responsive migration guide](https://forums.unraid.net/topic/192172-responsive-webgui-plugin-migration-guide/), webgui source on `master`.

| Change                                                                                                                                                                                                                                                                      | Our code                                                                                                                                                                                                                                                            | Result                                                                                                                                                                                                        |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 7.2: responsive webGUI; "some plugins may have visual issues"; opt-out `ResponsiveLayout="false"` forces a 1200 px minimum (last resort)                                                                                                                                    | `UGREENNASControl.page:1-4` has no `ResponsiveLayout` (responsive stays on); own grid with breakpoints `css/app.css:1407` (900 px) and `:1433` (560 px); `.unc-nav` scrolls horizontally on narrow screens                                                          | none found by reading; layout below 768 px and the case view are not checked on a real 7.2+ box (open item: [harness coverage](#what-the-local-harness-covers))                                               |
| 7.2: do not add elements to title bars                                                                                                                                                                                                                                      | the page adds nothing to the title bar; the brand and navigation live inside the page body (`UGREENNASControl.page:49-59`)                                                                                                                                          | none found                                                                                                                                                                                                    |
| 7.2: wide tables in `div.TableContainer`, buttons in `<span>`, no fixed widths or floats                                                                                                                                                                                    | no `<table>` in the page or `led/include/views.php`; `float` appears only as `float: none` (`css/app.css:858`, `:1180`)                                                                                                                                             | none found                                                                                                                                                                                                    |
| 7.2: dashboard tile uses `tile-header`, `tile-header-left`, `tile-header-right(-controls)`, `h3.tile-header-main`; old tiles used `<i>` + `div.section` with a `<br>` title; guide's own fallback keeps `tile-header` and switches only the title markup below `7.2.0-beta` | `UGREENNASDashboard.page:21-31` is the 7.2 markup, no version branch. `.tile-header*` has CSS only in Unraid 7.2+; our CSS does not define it (`grep tile-header web/plugin/css` finds nothing)                                                                     | Not an issue: `min` is 7.2.0 ([What we declare](#what-we-declare)), so 7.0 and 7.1 never install it. On 7.2+ none found                                                                                       |
| 7.2: `Cond` and `$mytiles` mechanism unchanged                                                                                                                                                                                                                              | `UGREENNASDashboard.page:3,18` match `DashStats.page` on `master` (`$mytiles[...]['column2']`, `tile-header` block)                                                                                                                                                 | none found                                                                                                                                                                                                    |
| CSRF: `master` `local_prepend.php` checks every POST (not `login.php`, `auth-request.php`); token from `$_POST['csrf_token']` or header `X-CSRF-Token`; failure answers HTTP 403 JSON `{"error": "<reason> csrf_token"}`; `hash_equals`                                     | token from `/var/local/emhttp/var.ini` (`UGREENNASControl.page:7-9,30`); client sends `csrf_token` in the POST body (`web/src/core/api.ts:38-44`); non-2xx with JSON body is turned into an error message (`api.ts`, `failed`); GET answers 405 (`api.php:173-178`) | none found. `master` reads `state/var.ini`, we read `/var/local/emhttp/var.ini`; both are the same file on 7.x (the symlink is unverified), so keep the harness check `web/tools/harness/prepend.php` aligned |
| 7.2.5/7.3.0: "WebGUI security hardening for authenticated-session request handling" (no detail published)                                                                                                                                                                   | n/a                                                                                                                                                                                                                                                                 | detail unavailable (unverified); covered by the POST test in [Steps and pass criteria](#steps-and-pass-criteria)                                                                                              |
| 7.3.3: webGUI displays text "safely, not treated as HTML"                                                                                                                                                                                                                   | the plugin renders its own page; names from Unraid (`disks.ini`) pass through our JS; not checked whether we escape on the PHP side                                                                                                                                 | open: review `led/api.php` output encoding once (unverified)                                                                                                                                                  |
| 7.4: shared task tray, live install progress (CR-only ticks shown as separate updates), per-plugin update checks, failed validation stays visible, remove hooks run when output contains shell characters                                                                   | install and remove scripts print plain lines (`plugin/ugreen-nas-control.plg.tmpl:61-205`); no carriage returns, no `$` or backticks in messages                                                                                                                    | none found; verify the install log looks right in the tray (step A6 of [Steps and pass criteria](#steps-and-pass-criteria))                                                                                   |
| jQuery / Dynamix JS helpers / `swal`                                                                                                                                                                                                                                        | `grep` over `web/plugin` and `web/src` finds none; plain `fetch` (`api.ts`). `<?autov(...)?>` is used for assets (`UGREENNASControl.page:43`, `UGREENNASDashboard.page:46`)                                                                                         | none found                                                                                                                                                                                                    |
| PHP 8.4 deprecations (implicit nullable parameters and others)                                                                                                                                                                                                              | `php -l` runs in `just php-lint` but only with whatever `php` the shell has; the flake pins `php84`. Runtime deprecations in `api.php` and `led/api.php` are not tested with `error_reporting(E_ALL)`                                                               | none known; open item: run the harness with `E_ALL` and fail on deprecations ([unraid-8.md](unraid-8.md#preparation-we-can-do-now), item 6)                                                                   |
| Process start from PHP                                                                                                                                                                                                                                                      | `api.php:28` `exec(escapeshellarg(...))`; `api.php:437` `nohup ... &` via `shell_exec`; `led/api.php:36` `exec($command)`                                                                                                                                           | none found; 7.4 changes nothing here                                                                                                                                                                          |

Incompatibilities found: none. Nothing in 7.2 to 7.4.0-rc.2 lists a plugin API, theme or CSS removal besides the 7.2 responsive refactor.

## Compatibility test plan

### Matrix

Rows are Unraid builds, columns are scenarios. Every cell is run on a real box or VM unless [the harness](#what-the-local-harness-covers) covers it. Build list: **7.2.9** (last 7.2, PHP 8.4.27), **7.2.7** (last 7.2 on PHP 8.3), **7.3.4** (stable), **7.4.0-rc.N** (newest RC; replace by 7.4.0 stable when it ships).

| Build           | A fresh install | B upgrade from the previous stable `.plg` | C reboot | D remove (and reinstall) | E next to stable and back |
| --------------- | --------------- | ----------------------------------------- | -------- | ------------------------ | ------------------------- |
| 7.2.7 (PHP 8.3) | run             | run                                       | run      | run                      | run                       |
| 7.2.9 (PHP 8.4) | run             | run                                       | run      | run                      | skip                      |
| 7.3.4           | run             | run                                       | run      | run                      | run                       |
| 7.4.0-rc.N      | run             | run                                       | run      | run                      | run                       |

Hardware: the fan and LED paths need a DXP6800 Pro or DXP4800 GT. Everything else runs on any x86 VM; on a VM the plugin must report "no supported fan controller" and keep the page usable (`plugin/ugreen-nas-control.plg.tmpl:76-79`).

### Steps and pass criteria

Preparation on every build: note `cat /etc/unraid-version`, `php -v`, the server clock. Browser console and `/var/log/syslog` open. "Plugins page" = Plugins tab. `/boot/config/plugins/ugreen-nas-control` is the boot folder. Install URL stable: the raw `.plg` on `main`; next: `https://ugreen-nas-control.github.io/ugreen-nas-control/next/ugreen-nas-control.plg` ([releasing.md](releasing.md)).

**A. Fresh install**

1. Plugins, Install Plugin, paste the stable URL, Install.
2. Pass: log ends with `Installation complete.`, exit shown as done, no `ERROR`. Files exist: `ls /usr/local/emhttp/plugins/ugreen-nas-control` shows `api.php`, the two `.page` files, `backend/`, `js/`, `css/`, `led/`. `/boot/config/plugins/ugreen-nas-control/<version>/ugreen-nas-control-<version>.tar.xz` exists. `ls /usr/local/emhttp/plugins/.ugreen-nas-control.*` shows nothing.
3. Settings, Utilities, UGREEN NAS Control opens; the version shown on the page equals the installed one; browser console has no errors and no failed request.
4. POST works: change the language (action `lang`, a write). Reload: the setting stuck; `fan.cfg` changed. A POST with a wrong token returns 403 (`curl -s -o /dev/null -w '%{http_code}' -d 'action=lang&value=de&csrf_token=x' http://<ip>/plugins/ugreen-nas-control/api.php`).
5. Dashboard: the tile appears, can be dragged, collapsed and hidden; header icon, title and cog align at 360, 768 and 1600 px widths (browser dev tools). Settings page: navigation scrolls, no horizontal page scroll at 360 px; dark and light theme.
6. Install log in the 7.4 task tray (7.4 only): lines appear live, no garbled output.
7. On a UGREEN box: fan mode change changes `ugreen-nas-fan status` duty within 4 s; LED switch on shows the LEDs; `pgrep -f ugreen-nas-fan` shows one daemon. On a VM: daemon not started, warning in the log, page still loads.

**B. Upgrade from the previous stable**

1. Install the previous release from its release URL on `main`, then change settings (fan mode, one LED colour, language), start the daemon.
2. Plugins, Check for Updates, Update (or install the new `.plg`).
3. Pass: log ends with `Installation complete.`; settings unchanged (`diff` of `fan.cfg`, `leds.cfg` before and after); old cache folder `/boot/config/plugins/ugreen-nas-control/<old version>/` is gone and the new one exists; other folders created by the user inside the boot folder are still there; fan duty never reads 0 % for more than one poll; one daemon process; the page works after a hard reload (no stale JS: `autov` and `tar -m` give new mtimes).
4. Fail test: delete the archive from the cache folder before the install script runs (or point `<URL>` to a 404 in a local copy). Pass: the install fails with a non-zero exit, the old version keeps running, no half-installed tree.
5. Version order: `5.0.9` then `5.1.0` and next `5.1.0-next.000042` between `5.0.3` and `5.2.0` (the loader uses `strcmp`, see [Versions](releasing.md#versions)); run the comparison in `plugin/tools/plg.ts check` locally.

**C. Reboot**

1. With the plugin installed and settings set, reboot with the array started.
2. Pass: the plugin manager reinstalls from the boot cache without network (unplug the cable on one run); the daemon runs again; LED state as configured; the dashboard tile is present; no entry in `/boot/config/plugins-error`; `syslog` has no `ugreen-nas-control` error.
3. Reboot with the array stopped and auto start off: the settings page loads; no PHP error in `syslog`.
4. Shutdown: `event/stopping` (source `plugin/event/stopping`) runs `ugreen-nas-ctl event stopping` (power LED signal if the LED part is on); after power-on the fans were handed back to the BIOS values before power-off (`/var/run/ugreen-nas-control.bios` is gone after reboot and recreated on start).

**D. Remove and reinstall**

1. Plugins, Remove. Pass: fans return to BIOS values (`ugreen-nas-fan status` is gone; check fan rpm with `sensors` or by ear), no process `ugreen-nas-fan`, `/usr/local/emhttp/plugins/ugreen-nas-control` and the hidden `.new`/`.old` folders are gone, cron file removed, `update_cron` ran, settings (`fan.cfg`, `leds.cfg`, `fan_rpm.json`, `led.enabled`) still in the boot folder (`plugin/README.md`, remove).
2. Menu entries gone; a favourite heart on the Settings page does not leave a broken entry.
3. Reinstall: settings come back as before.
4. On 7.4: a remove whose output contains shell characters still completes (hook runs; the 7.4 notes list the fix).

**E. Next and stable**

1. With stable installed, `plugin install <next url>`. Pass: the install replaces stable in place, the Plugins page shows exactly one entry, the page shows `X.Y.0-next.NNNNNN`, settings kept.
2. `plugin install <stable url> forced`. Pass: as in B; one entry, icon and README shown (same plugin `name`).
3. Update check: with next installed, "Check for updates" shows no update unless a newer next build or a newer stable exists.

Switching rules: [releasing.md](releasing.md).

### What the local harness covers

| Check                                                                                           | Command                                               | Covers                                                                                                                 | Does not cover                                                                            |
| ----------------------------------------------------------------------------------------------- | ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| PHP syntax                                                                                      | `just php-lint`                                       | `*.php` and the PHP part of `*.page`                                                                                   | runtime deprecations, PHP version differences                                             |
| `.plg` and XML well-formed, manifest equals archive, archive reproducible, both channels render | `just plg-check`, `just xml-lint`                     | `.plg` structure, SHA256/MD5, `min` equals `<MinVer>`                                                                  | what the loader does with it; the install script                                          |
| UI, API contract, POST CSRF rejection, all models, themes                                       | `just harness <model>`, `just shots`, `just web-test` | settings page and tile on php 8.4 with a fake Unraid ([harness.md](harness.md)), screenshots per model, page and theme | real Unraid CSS, the 7.2+ responsive layout, the dashboard's drag/collapse, the task tray |
| Go behaviour (fan, LED, parity with the shell backend)                                          | `just go-test`, `just parity`                         | logic without hardware                                                                                                 | `/dev/port`, `/dev/mem`, `/dev/i2c-N` on real chips                                       |
| Install script logic                                                                            | none                                                  | -                                                                                                                      | everything in `INLINE` (stage, swap, prune, remove); needs a box or VM                    |

Needs a real Unraid box or VM, not the harness: all cells of the [matrix](#matrix), the responsive layout with Unraid's CSS, the install log in the task tray, reboot/boot-cache behaviour, `update_cron`, `restore_favorites`, the next/stable switch, and everything touching the chips.

### Release checklist (per Unraid release)

1. Read the new release notes under "WebGUI / System" and "Plugins"; grep for `plugin`, `.page`, `jQuery`, `csrf`, `theme`, `dashboard`.
2. Re-read `local_prepend.php`, `DashStats.page` and `dynamix.plugin.manager/scripts/plugin` on `master` against [What we declare](#what-we-declare) and [webGUI changes](#webgui-changes-72-to-74-against-our-code).
3. Run the matrix rows for the new build (steps A to D).
4. Update the tables under [Supported versions](#supported-versions); remove rows for builds no longer supported.
