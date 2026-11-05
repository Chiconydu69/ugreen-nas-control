# Changelog

User-visible changes per release, newest first. Written by `bun plugin/tools/changes.ts release` from the files in .changes/; the plugin's change list is generated from this file.

## 5.1.1

- Sensors: an unconnected temperature input (for example "Motherboard -21 °C" on the DXP8800 Plus) is shown as not connected and no longer used.

## 5.1.0

- `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl diag` prints one anonymised, read-only diagnostics block to paste into a bug report or the forum.
- Case view for the DXP480T Plus: front with the power LED, rear ports and the four M.2 slots as drives.
- Inside view for the DXP2800 GT: M.2 slots, RAM and what is installed.
- Inside view for the DXP4800, DXP4800 Plus and Pro: M.2 slots, RAM and what is installed.
- Inside view for the DXP8800 Plus and DXP8800 / Pro: M.2 slots, RAM, PCIe slot and what is installed.
- Inside view for the iDX6011 / iDX6012 / Pro: M.2 slots, RAM and what is installed.
- German interface texts reworded to read naturally.
- Fans: an input that is missing from the first tick (script, GPU, HBA, drive) now follows the default input also with "hold", and a missing input is logged and sent as an Unraid notification.
- Spanish, French, Italian, Dutch and Chinese texts use Unraid's own terms and read more naturally.
- LEDs: DXP2800 boards that reject all LED commands are reported once and no longer retried in a loop, the DXP2800 picks its LED frame by the BIOS system version, and the network LED stops blinking when the LED daemon stops.
- The model box on the LED page shows what the plugin does on your model from the model list (DXP480T Plus: power LED only).

## 5.0.0

- DXP2800 GT: fan control, confirmed on hardware by a community tester; it stays off until you run `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan cs201x confirm`, runs at 25 % minimum and puts the fans back to their start speed when the plugin stops. Probe by Alexander Schubert (issue 95).
- DXP2800 GT: LED support for power, network and both disks, off until you set `cs201x_leds="1"` in `leds.cfg`; the LEDs go back as found when the daemon stops (not yet confirmed on hardware). DXP6800 and DXP6800S use the same LED frames.
- Fans: new History page with the speed, rpm and temperatures of every fan over the last hour or 24 hours, and which input drove the speed at each moment (CPU, hottest drive, quiet hours cap, mover or parity check, emergency). Kept in the fan daemon's memory only, nothing is written to the flash drive.
- Fans: a SAS or RAID controller (HBA) can be the temperature source of a fan group, read from the mpt3sas sensor or from storcli, and a FanCtrl Plus 2 storcli sensor is imported as such.
- Fans: choose where each fan group reads its temperature (CPU, drives, NVMe, GPU, network chip or your own script), and export, import or take over the fan settings of FanCtrl Plus, FanCtrl Plus 2, Dynamix Auto Fan Control and IT-Kuny.
- The fan daemon can shape its output (dead zone, kick-start, minimum running speed, up and down ramps) and follow a schedule: quiet hours cap the speed (never below the minimum, never during an emergency), and a running mover or parity check/rebuild can raise the case fans. Both are off by default; the defaults decide exactly as before.
- About: a link to help the project with test hardware or a test on your model.
- The interface is also available in French, Italian, Dutch and Simplified Chinese (machine-assisted, native review welcome).
- MQTT: a display or Home Assistant can update Docker containers, one or all at once, with progress reported back, once you set `mqtt_control_containers="1"` (off by default, separate from `mqtt_control_updates`).
- MQTT: an optional publisher sends temperatures, fans, drives (including each array disk), free space of the array and pools, parity check, rebuild and clear progress, network rates and IP addresses, alarms, notifications and available updates (plugins, containers, Unraid OS) to a broker for displays and Home Assistant; restart, shutdown and plugin updates by command are off unless switched on.
- Front panel: `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas panel buttons [--watch]` reads the power and reset button (read only) and `panel beep` sounds the beeper, from UGREEN's open-source drivers (not yet confirmed on hardware).
- Other fan and LED tools (FanCtrl Plus, Dynamix Auto Fan Control, UGREEN LED drivers, ...) are detected; while one controls the fans, UGREEN NAS Control only reads them and shows a banner with a choice.
- IT8613E models: the fan daemon feeds the watchdog the BIOS armed once it has seen it count (never arms it, yields to `it87_wdt`); new `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan power-on-ac` reads and sets power-on after AC loss.
- Case view (front and rear) for the DXP2800 GT; the DXP4800 GT rear is closer to the real device (ribbed bezel, dotted lettering, light grille in the light theme); fan speed is written under the rear fans; LAN sockets flicker with traffic; the dashboard tile has a background of its own for the colour schemes Dark and Light.
- Web UI for the new features: a Front panel page (button state, test beep), a DXP2800 GT fan card (diagnostics, confirm fans, forget), read-only fan readings for the plain iDX6011/iDX6012, a power-on-after-AC-loss card, the new watchdog states, a backlight value of "default", and the second LAN LED (`netdev2`) of the iDX6011 Pro on the LED pages.
- DXP480T Plus: the power LED lights steady white again instead of flashing.
- LEDs: the DXP2800 falls back to the frame UGREEN's own driver uses when the controller does not acknowledge the usual one (boards from December 2025); breath times stay at UGREEN's minimums (on 1000 ms, off 500 ms).
- Script inputs work on Unraid: `/boot` is vfat and files there can never be executable, so the script now runs through the interpreter named in its shebang (`bash`, `sh`, `php`, also via `env`). A missing shebang or another interpreter is reported in the input test.
- Sensors: on a model without fan control the fan controller shows "No fan control for this model" in a neutral style instead of a red "Not found". Reported with the DXP2800 GT (issue 95).
- Fans: stopping or restoring now waits for a busy tool instead of giving up, and a slow controller, a hung helper or a broker restart no longer leaves fans, LEDs, MQTT status or state files in a bad state.
- IT8613E: the watchdog survives a restart of the fan service; `power-on-ac set` from an unrecognised value needs `--force`.
- Fan control for more models: a fan probe finds which output drives which fan on the DXP2800, DXP4800 (base, Plus, Pro) and DXP8800 Plus, checked against UGREEN's own drivers (the DXP4800 has no CPU fan output, the DXP8800 Plus a second case fan; fan control waits for your confirmation); the iDX6011 Pro gets its four fans, a 25 % floor and the LCD backlight setting (unconfirmed on a device); the plugin feeds the BIOS watchdog while the BIOS has it armed and never arms it.
- iDX6011 Pro and DXP4800 GT: fan, watchdog and backlight now follow UGREEN's published drivers; on the Pro the fourth fan shows the third fan's speed while it reads 0, the backlight accepts 0 (default), the bays follow the Pro wiring, the power LED lights when the status daemon starts, and iDX6012 is recognised; the plain iDX6011/6012 shows fan and watchdog readings only (community reports, not yet checked on a device).
- New models from UGREEN's product list: DXP6800 and Plus, DXP6800S, DXP8800 and Pro, DXP2800S and DH2600 (nobody has tested them yet).
- Questions, ideas and showcases now go to GitHub Discussions (forms, links in README, website and the plugin's About page); issues stay for bugs and model reports.
- Fan settings: new options (inputs, per-fan tuning, quiet hours, job rules) work with your existing settings; the first save of a new option keeps a backup of the old settings file as `fan.cfg.bak`.
- Next channel: every push to `next` publishes a rolling test build `X.(Y+1).0-next.NNNNNN` (install URL `https://ugreen-nas-control.github.io/ugreen-nas-control/next/ugreen-nas-control.plg`), replacing the beta channel; it installs over stable in place, going back to a lower version needs `plugin install <url> forced`.
- The plugin is one program now, with fewer helper scripts and sturdier start and stop: stale pid files no longer stop foreign processes and settings are checked before they reach the LED chip; `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-ctl report` prints an anonymised JSON for model reports.
- Plugin versions always have three parts, `X.Y.Z`; this release is `5.0.0`.

## 4.2

- Sensors: on a model without fan control (every one but the DXP6800 Pro and the DXP4800 GT) the fan controller now says "No fan control for this model" instead of a red "Not found", which looked like a defect. Nothing changes in what the plugin does with the fan chip: it still does not touch it on these models.

## 4.1

- DXP2800 GT: a read-only test command, `ugreen-nas-fan cs201x`, for the Chipsea CS201x chip of this model (it is not an ITE chip). It reads the chip ID, the base address of the embedded controller, the rpm and duty of the two fans and two temperatures, and prints them. It writes no fan setting and does not switch anything on. Fan control for this model is not part of it yet; the plugin itself still does not touch the fan chip on this NAS.

## 4.0

- DXP2800 GT: front and rear view after the product photos (two bays with the LEDs below them, USB-C and USB-A at the front; the back with the grille of the DXP4800 GT (ribbed bezel, dot mesh, dotted lettering, the fan behind it when the NAS reports one), HDMI, USB 3.2, two USB 2.0, 10G LAN, reset and the power inlet). Fan control stays off for this model

## 3.9

- Rear views with fan control (DXP6800 Pro): the fan labels ("Case fan 1 · 900 rpm") are in front of the mesh and readable in both themes

## 3.8

- Rear view of the DXP4800 GT redrawn after the product photo (ribbed bezel around a dot mesh, dot-matrix lettering, ports at the photo positions); the rear fan turns with its rpm (thanks j3n57h0m45)
- Light theme: the fan grille of the rear views is grey instead of a black slab
- The LAN sockets drawn in the rear view flicker with the traffic (since the last poll); no frame around linked ports any more
- The small 18 px icon: LEDs and fan stay inside the drawing

## 3.7

- Dashboard tile: with the colour scheme "Dark" or "Light" the tile has a background of its own again, in that scheme, whatever theme Unraid uses; "Same as Unraid" draws none and follows the dashboard (issue #91)

## 3.6

- Dashboard tile: it always takes its colours from the dashboard behind it (light or dark Unraid theme), whatever the colour scheme of the plugin says. Before, a fixed "dark" scheme gave light text on a light tile (issue #87)

## 3.5

- Fans: "Measure fans" runs every fan through 10 to 100 % (about 6 minutes, started on the Fans page, the daemon pauses meanwhile) and stores the rpm per speed. The curves then show a real rpm axis and the point table the rpm of each point. A short CPU burst no longer stops the measurement (4 checks in a row above 80 °C; 95 °C and above stops at once; the hottest reading goes to the syslog)
- Fans: the Custom curve is set up in three steps (measure, lowest fan speed, curves), starting from Quiet, Standard or Performance; a floating save bar with Discard; "Case fans help the CPU" (the case fans never run slower than the CPU curve asks for; off by default); the minimum speed moved here from Safety
- Quiet mode: the CPU curve starts at 15 % and is up to 5 points lower between about 55 and 85 °C (at the default minimum speed of 25 % the difference is smaller); the other modes are unchanged
- Safety: all fans run at 100 % when the hottest NVMe SSD minus the NVMe offset reaches the disk limit (70 °C with the defaults), with the usual alert; the page shows the headroom to each limit
- New Sensors page: the detected controllers and every reading by part with its source, the Safety limit and the fan it drives (sleeping disks are not woken up)
- Overview regrouped (case and drives, cooling, processor and memory, network); the dashboard tile has a compact summary with fan mode, rpm, hottest temperature and alerts
- Models without fan control show no fan controller chip in the title row
- Scripts: install-dev.sh installs into a staging folder first, plg-hashes.py reports what it could not hash and exits 0 after a normal refresh, the test harness listens on localhost only

## 3.4

- Models without fan control (all but DXP6800 Pro and DXP4800 GT): no fan card, no "fan mode" tile and no "fan controller not usable" warning on the overview any more; Settings explains that there is no fan control for the model and no switch is needed
- DXP8800 Plus: the rear view drawn after a photo of the device: one wide mesh with the UGREEN lettering and the two fans behind it, the PCIe slot, HDMI, USB 3.2, two USB 2.0, the two LAN ports, reset and the power inlet
- DXP4800 GT: the rear view is the chassis of the DXP4800 Plus (mesh with the fan behind it, the ports in one row, two 10G LAN ports)
- Case view of the DXP4800 family, DXP4800 GT, DXP2800 and DXP8800 Plus: the LEDs sit below the bays as on the DXP6800 Pro (no separate LED strip next to the LAN LED any more)

## 3.3

- Overview: a "Night mode" badge next to "Online" while the night mode has the LEDs dimmed or off (the badge is back); the night-mode hint in the LEDs card now follows changes of the schedule without a reload

## 3.2

- About, Supported models: "Confirm my device": tick what works on your model (LEDs, bay order, case view, inside, fan control, temperatures) and open a pre-filled GitHub issue with the model, the versions and your ticks (no serial numbers, IP addresses or drive names). Nothing is sent from the plugin; you submit the issue yourself

## 3.1

- About: a table of every UGREEN NASync model with what the plugin does for it (LEDs, case view, fan control); your model is highlighted; the models are sorted by series (DX, DXP, iDX, DH) and the card is folded away until you open it. The same table is in the README

## 3.0

- Temperatures: the LAN chip rows say "network chip" and a tooltip explains PHY and MAC (two parts of the same chip; some report the same value), and the extra sensors of an SSD get a tooltip (often the controller or the memory chips; only the Composite value steers the fans)
- About: the UGREEN LED Control link tile is gone, the badge says DX · DXP · iDX, the thanks name Alexander and j3n57h0m45 for the development of UGREEN NAS Control

## 2.9

- Fan control can be switched off (Settings): for users whose fans are run by another plugin or script. Off, the plugin does not start the fan daemon and does not read or write the fan chip at all, the fans get the BIOS values back, and the fan pages and the fan card are hidden
- The Unraid dashboard tile no longer pauses after 5 minutes without input (only while the tab is hidden), so a dashboard left open on a screen keeps updating

## 2.8

- Safety: choose drives that must not drive the case fans (an external SSD that runs hot on its own, for example). Ignored drives neither set the speed of the case fans nor trigger the disk emergency rule. Requested in the forum thread by cardsdoc

## 2.7

- Overhaul by j3n57h0m45 (one look across the NAS and LED parts): one LED overview with one switch, the LED settings exist once (system LEDs, drive LEDs, one preview), Night mode and Alerts & shutdown pages, separate Settings and About pages with page-specific headers, save bars with a leave warning on Fan curves and Safety, one spacing scale, the web LEDs mirror the device with one renderer, a shared poller that pauses when the page is hidden or idle, a fork-free LED daemon, cached dmidecode and ethtool reads. The Colours and Effects pages, the second LED switch, the global brightness slider and the dashboard tile size option are gone (the tile size did nothing since the LED tile was merged)
- UGREEN NAS Control now runs on its own: the LED part is maintained here, no longer a copy of UGREEN LED Control (it was last synced from UGREEN LED Control 0.27.0). Settings, effects, the import of UGREEN LED Control's settings and the warning when it is installed too stay

## 2.6

- Contributions by j3n57h0m45: temperatures of the LAN ports, the iGPU and the extra NVMe sensors; the System facts show the fan controller and the LED controller side by side and say why the LED part is off; the LAN and DISK LEDs in the strip front take the LED controller's colours; the case view keeps working on a model without NVMe drives; the Processor card names the fan controller temperature like the temperature list

## 2.5

- Case view for the DXP2800 (two bays, tall rear with one LAN port) and the iDX6011 / iDX6011 Pro (six bays and the display at the front, the rear with the PCIe slot, mesh, vent, power inlet, USB, two 10G LAN ports, HDMI and OCuLink), drawn after photos; the LEDs are drawn like on the DXP6800 Pro. No inside view, no fan control. The tool maps the iDX bays on its two SATA controllers like UGREEN LED Control does

## 2.4

- Rear view of the DXP4800 / Plus / Pro: LAN 1 (10G) and LAN 2 (2.5G) were taken in the kernel's order (eth0, eth1), so the active port could be shown on the wrong socket. The port that supports more is LAN 1 now (speed read with ethtool)

## 2.3

- Case view for the DXP8800 Plus: the front with its eight bays and the strip (power button, LEDs, SD, two Thunderbolt 4, USB-A) and the rear as a row of sockets after the ports the reviews list, plus the drives as cards. No inside view and no fan control yet

## 2.2

- Case view for the DXP4800, DXP4800 Plus and DXP4800 Pro, drawn after photos of the DXP4800 Plus: front with the four bays, the rear with the big mesh, HDMI, USB, both LAN ports and the DC inlet, and the drives as cards. No inside view yet (the M.2 slots of these models are not mapped) and no fan control. SSDs show as "M.2" there

## 2.1

- Dashboard tile and LED card: the row of LEDs showed six bays on a four-bay model (DXP4800 Plus). It now follows the number of bays the LED controller reports

## 2.0

- LEDs: the "LED control" page is gone. Its switch, the notes and the import of UGREEN LED Control's settings moved to Appearance & About; while the LEDs are off the sidebar entry "LED control" leads there, once they are on the entry disappears (it only repeated the switch of General & preview)

## 1.9

- The Support link on the Plugins page opens the support thread in the Unraid forum

## 1.8

- Spanish: the whole interface (overview, case, fans, safety, appearance, dashboard tile) is translated, next to the LED pages with ivolution's translation from UGREEN LED Control. Choose it under Appearance & About or let it follow Unraid's language. The LED pages follow the language of the plugin

## 1.7

- Every download of the plugin now carries an MD5 that Unraid checks (needed for Community Applications). The two binaries are built the same way each time, CI checks the hashes, and the release is made only if they match

## 1.6

- LEDs in the drawings (the bays on the overview, the LED card, the dashboard tile) sit in a dark slot: dimmed or in standby they were not visible on silver or white in light mode

## 1.5

- Light mode, readable: all pages were measured for text contrast and the weak spots fixed (text on the silver bays, the role labels on the drive cards, green values, white text on the blue buttons, the fan labels on the rear picture)

## 1.4

- Overview: the "NVMe SSDs" card is gone; the SSDs are in the temperatures, by their place in the case (M.2-1, M.2-2, system slot, PCIe) with the Unraid mark on the boot SSD. Processor, memory and network share the row

## 1.3

- Overview, temperatures: sections (processor and mainboard, NVMe SSDs, drives) and a small picture in front of every sensor, coloured by its level

## 1.2

- Overview rearranged: the case picture (as before) on the left, fans and the fan mode right next to it, then the four hardware cards in one row, the temperatures in columns and the LEDs below. The page is about half as high. The "System" card moved to Appearance & About

## 1.1

- Case page: the drives are cards now (bay, role such as array disk, parity or cache, a drawing of a hard disk or SSD, model, size, temperature bar) instead of a table; empty bays are shown too
- Light mode: the rear picture is silver like the front, the inside view has a pale board; the pages were checked one by one for dark leftovers

## 1.0

- Versions are now counted 1.0, 1.1 ... 1.9, then 2.0 (never a third number, never 1.10). Unraid compares the version as text, so 0.10.x counted as older than 0.9.x and the update check showed nothing for anyone on 0.9.x. 1.0 is above all of them
- Drives table on the Case page: white text on white cells in some Unraid themes
- The page jumped back to the top while scrolling (Firefox, Safari): a failed hardware read emptied the picture of the case, the page got shorter and the browser scrolled up. The last picture is kept now and the scroll position restored after a redraw

## 0.10.8

- The LED controller facts (bay assignment, control, controller, LEDs found, saved settings) moved from the Alerts & shutdown page to Appearance & About. Model, kernel and version were doubled and are only on the Overview

## 0.10.7

- The size of the dashboard tile (compact or with temperatures) is now set under Appearance & About, next to the colour scheme, instead of on the Alerts & shutdown page

## 0.10.6

- Alerts & shutdown: language and colour scheme were offered twice (here and under Appearance & About). The LED pages now follow the ones from Appearance & About

## 0.10.5

- Sidebar: new entry "Alerts & shutdown" under LEDs. The power LED alerts and the colour and effect shown on shutdown and reboot were already in the plugin, but could not be reached

## 0.10.4

- The case page shows the inside of the DXP6800 Pro as a drawing of the board: M.2 modules, wide SO-DIMMs, the system slot and the PCIe slot, with live names, sizes, temperatures and roles. Other models keep the cards.

## 0.10.3

- SSD roles in words that fit an SSD: "Cache SSD", "Pool SSD “name”", "Array SSD (disk 1)" and "Parity SSD" instead of "Unraid: disk1", which read like a hard disk (dashboard tile, overview card and inside view)

## 0.10.2

- A short description is shown under the plugin's name on the Plugins page (the plugin now ships a README.md in its folder)

## 0.10.1

- Case page, rear of the DXP6800 Pro, redrawn after a photo of the device: black body with silver side rims and screws, the big recessed blende with the fine mesh and the dotted UGREEN lettering and the two case fans behind it (faint, turning with their real rpm, rpm under them), the PCIe slot cover on the left, below the sockets as on the device (HDMI, USB 3.2, two USB 2.0, the two 10GbE ports with the live link, reset), the recessed box at the bottom right with the IEC power inlet (three pins) and the round PSU fan grille with its rings of holes and blades turning behind it (the power supply has no speed sensor, so it turns at a steady pace; not with reduced motion), the Kensington slot and the feet. Everything is placed in per cent of the picture and scales with it

## 0.10.0

- Overview: the fans got room: the three fan tiles are big now, with large turning icons and the rpm, and the model in the middle is a little narrower; the strip under the bays keeps the order of the original (LEDs, ports, lettering) in one line and only scales with the case
- Overview and Case no longer repeat each other: the overview shows the front with the bays, the temperatures, the fans and the LEDs; the Case page has the drives in detail, the rear and the inside. The tiles and the front picture on the Case page, and the tabs on the model picture, are gone
- Overview: in the fan mode tiles the icon and the name sit in the middle of the tile, both ways
- Fix: in the dashboard tile the row "LEDs on the device" vanished for a moment on every refresh (it was added only after the drives had been drawn). Drives and LEDs are read together and drawn once, and only when something changed
- The overview reads the LED controller at most every few seconds, as the LED pages poll it themselves

## 0.9.1

- New icon: a NAS as a small cube with a coloured LED under each of four bays and a white fan in the lid (Plugins page, Settings menu, dashboard tile, sidebar, About page)

## 0.9.0

- LED control on every model UGREEN LED Control already knew: DXP6800 Pro, DXP8800 Plus, DXP4800 (Plus, Pro, GT), DX4600 Pro, DX4700, DXP2800, DXP480T Plus and the iDX6011 (Pro), and other UGREEN DX/DXP/iDX models with a guessed bay order. Models whose bay order or LED numbers nobody has confirmed (iDX6011 Pro, others) are marked "untested" on the LED page
- On models other than the DXP6800 Pro and DXP4800 GT the fan group and the case view are not shown (the fan wiring is unknown); LEDs, system info and temperatures work
- Fix: entries the page hides (the LED pages while LEDs are off, the fan entries on other models) disappeared only in the data, not on screen

## 0.8.0

- Take over the settings of UGREEN LED Control: if that plugin's settings are found, the page LEDs → LED control offers "Take over the settings" (colours, effects, status mode, night mode; what this plugin had is kept as leds.cfg.bak), and switching LED control on for the first time without own settings takes them over by itself. Afterwards UGREEN LED Control can be uninstalled under Plugins: this plugin succeeds it for the DXP6800 Pro and the DXP4800 GT (the About page says so)
- Tidied up: the unused pages of the LED copy (its settings page, its dashboard tile and their script and icon) are no longer installed

## 0.7.0

- LEDs live: the cockpit draws the colours the LEDs show right now into the case picture (the bar of each bay, the power and the LAN LED) and has an LED card (mode, brightness, night mode, one dot per bay); the dashboard tile gets a row "LEDs on the device" with the same colours and shows the LED mode
- This plugin now succeeds UGREEN LED Control for the DXP6800 Pro and the DXP4800 GT: the LED pages of the copy are gone from the menu and from the dashboard, and the plugin runs on its own: an installed UGREEN LED Control no longer keeps the LED part off, the page only warns that both write the same LEDs (the last write wins). ugreenleds-driver and the led_ugreen module still keep it off

## 0.6.0

- The LED pages are part of UGREEN NAS Control now: with the switch under LEDs on, the sidebar lists General & preview, Colours, Effects, Drive status and Schedules (brightness, colours per LED, effects, the status mode for disks and the network, night mode and alerts), in the same window and with the same colour scheme (dark or light). The separate Settings page "UGREEN LED Control" is gone from the menu; the LED dashboard tile stays for now and opens this page
- Turning the switch on or off loads the page again, as the LED pages come with it

## 0.5.0

- The sidebar is grouped: Cockpit (Overview, Case), Fans (Fan curves, Safety), LEDs (LED control) and System. "System" and "About" are one page now ("Appearance & About": language, colour scheme, version, links, thanks). First step towards one plugin for fans and LEDs, the LED pages follow

## 0.4.2

- Dashboard tile, cockpit and inside view: every NVMe SSD shows what it is used for in Unraid: the boot SSD (the one /boot is mounted from) with the Unraid mark, a cache or pool member with a stack icon, an array disk with a drive icon; the tooltip names the role
- The case on the overview is shown straight, front on, not turned
- The overview is more compact: tighter spacing, the fan mode as one row of buttons (the description is the tooltip), the system facts in two columns, and the three-column cockpit now depends on the width of the content area, so it also holds on 13-inch screens

## 0.4.1

- New overview as a cockpit: the detected model in the middle (the case drawn big and slightly turned, with a tab each for the front, the rear and the inside), around it everything that was detected: processor, memory with its modules, network with link speed on the left; fans, temperatures and NVMe SSDs on the right; below the fan mode and the system facts. Dark and light theme, DXP6800 Pro and DXP4800 GT
- Fix: the NVMe slots of the DXP6800 Pro: M.2 slot 1 and the PCIe slot hang on the CPU, M.2 slot 2 and the internal system slot (the UGOS module) on the chipset; the PCIe slot no longer counts every other SSD as a card (the system slot shows in the inside view, the cockpit and the dashboard tile as "S")

## 0.4.0

- Case view: the front of the DXP6800 Pro now shows the SD card slot next to the two Thunderbolt 4 ports and the USB-A port
- Thanks: j3n57h0m45, who developed the DXP4800 GT support (fans through the IT5571, LEDs, case and System view), is named on the About page and in the README

## 0.3.9

- New: light case pictures: with the light theme the case (front, rear, inside) is silver aluminium with dark recesses for bays, ports and fan, as in UGREEN LED Control 0.26.0
- Fix: the fan icons keep their angle when the rpm changes instead of jumping back

## 0.3.8

- New: fan control on the DXP4800 GT: the fans hang on the IT5571 embedded controller (ACPI EC ports 0x62/0x66, no kernel module); CPU fan and case fan with rpm, manual speed and the curves, and the fans go back to the controller's own curve when the daemon stops. CPU temperature on AMD CPUs (k10temp), mainboard temperature from the controller. The 0.3.7 release was built just before this change, so it ships here
- New: a "System" view in the settings sidebar with the Language card and a new Appearance card: dark, light or "same as Unraid", for the settings page and the dashboard tile
- New: the case view shows the DXP4800 GT as on the device: 4 bays, SD, USB-C and USB-A on the front, no PCIe slot, the rear ports in their real order

## 0.3.7

- Built with Go 1.27.3; the LED helper is a little simpler inside, and the CI now also runs staticcheck and govulncheck, with no change in behaviour

## 0.3.6

- New: LED control on the DXP4800 GT (4 bays, ata1 to ata4): the LED controller sits on an AMD DesignWare I2C controller, which the helper drives directly through /dev/mem, no kernel module. The fans stay DXP6800 Pro only
- LED part updated from UGREEN LED Control v0.23.0 to 4f09327: two new switches (show disk errors, blink on disk activity) and a hint on the colour tables when a bay shows the SMART or error colour. No new files

## 0.3.5

- Dashboard tile: a new section "NVMe SSDs" below the drives, with the two M.2 slots (M1, M2) and the SSDs on the PCIe card (P1, P2); hovering one shows temperature, device, size, slot and model

## 0.3.4

- Fix: the bay details of the dashboard tile were cut off at the tile's edge; hovering a bay now shows them below the row, inside the tile

## 0.3.3

- Fix: on Unraid's light theme the temperature rows of the dashboard tile were nearly invisible; they sit on their own dark card now, like the fan cards (GitHub issue #12)
- Dashboard tile: a row of the six bays like Unraid's Disk Location widget; hovering a bay shows temperature, role, device, size, model and the Unraid disk id (GitHub issue #12)
- Case tab: new list "Drives" with the full disk id as Unraid shows it (model and serial), so drives of the same model can be told apart; the bay tooltips show it too (GitHub issue #11)

## 0.3.2

- Case tab: the front is smaller (as wide as the case picture of UGREEN LED Control), and the tiles and the inside view got icons for processor, memory, drives, NVMe, network, RAM, M.2 and PCIe

## 0.3.1

- Case tab: the front looks like the real one (six numbered trays with state and status bar, the strip with power and LAN LEDs, Thunderbolt 4 and USB-A sockets), the rear shows the real socket shapes (power inlet, HDMI, both LAN ports with link LEDs, USB)
- The About page is laid out like the one of UGREEN LED Control: icon, version, links, facts

## 0.3.0

- New tab "Case": tiles for processor, memory, drives, NVMe SSDs and network, the front with the six bays (disk, size, temperature, standby), the rear with both LAN ports (speed, link) and the other ports, and the inside with both RAM slots, the two M.2 slots and the PCIe slot with the SSDs on its card; everything is read from the system (`ugreen-nas-fan hardware`, read only)

## 0.2.0

- New: optional LED control, a copy of UGREEN LED Control 0.23.0 (colours, effects, status mode, night mode, dashboard tile). It is off until you switch it on under Settings → UGREEN NAS Control → LEDs; then Settings → UGREEN LED Control appears
- The LED part only starts on the DXP6800 Pro and not next to the UGREEN LED Control plugin, the ugreenleds-driver plugin or the led_ugreen kernel module, which write the same LEDs
- Nothing changes for the fans; the LED part adds a second binary (ugreen-led-i2c) to the release

## 0.1.2

- The plugin is now made for the DXP6800 Pro only: on any other model it no longer touches the fan controller (the fan wiring is only known for this model); system info and temperatures still work

## 0.1.1

- Fix: a plugin marked as favorite (heart icon in Settings) lost the mark on update and got it back only after a reboot; the update now restores it right away

## 0.1.0

- First version: system info, temperatures (CPU, mainboard, fan controller, NVMe, disks) and fan control for the DXP6800 Pro
- Fan modes BIOS, Quiet, Standard, Performance, Full speed and custom curves for the CPU fan and the case fans
- Emergency rules, minimum speed, Unraid notification for a stopped fan, BIOS values back when stopped or removed
- Dashboard tile, English and German interface
