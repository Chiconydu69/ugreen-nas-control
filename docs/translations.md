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
