# Translations

The interface is available in English, German, Spanish, French, Italian, Dutch and Simplified Chinese. English is the fallback: a missing text shows in English.

## Files

| Part                             | Files                                                 |
| -------------------------------- | ----------------------------------------------------- |
| Settings page and dashboard tile | `web/src/features/nas/i18n/{en,de,es,fr,it,nl,zh}.ts` |
| LED pages                        | `web/src/features/led/i18n/{en,de,es,fr,it,nl,zh}.ts` |

Each file is a flat object of `'key': 'text'`. `index.ts` registers all languages on `window`. The bundles `.build/plugin/js/i18n.js` and `.build/plugin/led/js/i18n.js` are built by `just web-build` and not tracked.

## Rules

- Translate values only. Keep `{placeholders}` and HTML tags exactly.
- A key exists in all languages in the same PR. The compiler reports a missing or extra key in every language except `en`; `bun run i18n` (part of `just web-check`) also checks placeholders, empty texts and keys used in the page markup (`data-i18n`, `data-i18n-html`, `data-unc-*`, `data-label-key`) that do not exist.
- Key names: `area.item` as in the existing files (`nav.overview`, `led.fx_rainbow`). Add new keys at the end of their block, not in the middle, so parallel PRs do not collide.
- The Spanish texts of the LED part come from ivolution's work in UGREEN LED Control. French, Italian, Dutch and Simplified Chinese were translated with machine help and would like a native speaker's review; so would the rest of the Spanish. Corrections are welcome as a PR.
- Language codes follow Unraid's language packs (`fr_FR`, `it_IT`, `nl_NL`, `zh_CN`): the plugin uses the part before the underscore. `zh_TW` (traditional Chinese) is not covered and shows English under "Same as Unraid".

## Add a language

1. Copy `en.ts` to `<code>.ts` in both `i18n` folders and translate the values.
2. Register it in both `index.ts` files, in `LANGS` in `web/src/core/i18n.ts`, in `UNC_LANGUAGES` in `web/plugin/include/lang.php`, and in the imports of `web/tools/check-i18n.ts` and the two `i18n.test.ts`.
3. Add an `<option>` to the language select in `web/plugin/UGREENNASControl.page`, the code to the list in the `lang` action of `web/plugin/api.php`, and to `LANGS` in `web/demo/api.ts` and `web/demo/outer.ts`.
4. `just web-check`, then `just harness` and switch the language in Settings.

## German

Unraid concepts follow the official German pack unraid/lang-de_DE (other languages use the matching unraid/lang-xx_XX pack), so the texts match the rest of the Unraid UI; where a pack term is stilted, the natural word wins, but menu and page names stay as Unraid shows them. Drives stay "Laufwerk" (hardware), where Unraid says "Datenträger".

Address: informal "du" throughout, imperative for instructions ("Prüfe …", "Wähle …"). `web/src/features/nas/i18n/de-style.test.ts` fails on calques and stilted phrases (Dienst, erfolgt, mittels, bezüglich, seitens, basierend auf, macht Sinn, bitte beachten Sie, …).

| Concept                   | German term                                          |
| ------------------------- | ---------------------------------------------------- |
| fan service / daemon      | Lüftersteuerung (never "Lüfterdienst")               |
| other fan or LED tool     | andere Lüfter- / LED-Steuerung                       |
| drive (any), HDD          | Laufwerk (any), Festplatte / Platte (HDD)            |
| bay                       | Schacht                                              |
| case view                 | Gehäuseansicht                                       |
| night mode, quiet hours   | Nachtmodus (LEDs), Ruhezeiten (fans)                 |
| fan chip                  | Lüfter-Chip                                          |
| deadband                  | Totzone                                              |
| armed watchdog            | aktiv; the plugin "füttert" the watchdog             |
| array, pool, parity check | Array, Pool, Paritätsprüfung (as Unraid's German UI) |
| notifications             | Benachrichtigungen (not "Meldungen")                 |
| parity rebuild            | Datenwiederherstellung (not "Rebuild")               |
| identify (bay LED)        | Identifizieren                                       |

- No Nominalstil ("Die Messung dauert …", not "Die Durchführung der Messung erfolgt …"); short sentences, one thought each.
- No English word order or word-for-word compounds; ask whether a German admin would say it.
- Keep technical terms Germans use: LED, NVMe, BIOS, Watchdog, MQTT, Home Assistant, Mover, Issue.
- Quotes in running text are „…“; code stays in backticks.
- Units with a space: `25 %`, `40 °C`, `2,5 Gbit/s`; "z. B."; 24-hour time, "Uhr" after a time.
- Buttons and labels: short infinitive or noun ("Speichern", "Messung starten").
- Write "Power-LED", "Netzwerk-LED", "LED-Steuerung": compounds with a hyphen after an abbreviation.
