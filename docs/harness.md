# Harness and screenshots

A fake Unraid around the real plugin files, for looking at the UI, the end-to-end test and the screenshots. No hardware, no Docker; nothing outside a temp dir is written.

## Harness

```
just harness [model]     # php -S on 127.0.0.1, model = id in web/models/ (default dxp6800-pro)
```

- `web/tools/harness/serve.ts` lays the files out like the plugin installs them (`plugin/files.txt`, built into `.build/plugin` by `just web-build`, absolute Unraid paths rewritten into the temp dir) and starts `php -S` (PHP 8.4 from the flake).
- The NAS API (`web/tools/harness/api-fixture.php`) answers from the sample data of the model (`web/models/fixture.ts`) and rejects POSTs without the CSRF token like Unraid.
- The LED API is the real `web/plugin/led/api.php`; its backend (`fake-led-ctl`) answers from JSON files.
- `view.php` renders the `.page` files. Open the printed URL; `?p=dash` shows the dashboard tile, `?shot=1` freezes the clock and CSS animations.
- `UNC_SCENARIO=conflict` (or `conflict-led`) starts a scenario with a rival tool ([conflicts.md](conflicts.md)); the same variable works for `web/tools/shots.ts`.
- `just e2e --old [ref]` serves the hand-written front end of a ref (default `upstream/main`, or `UNC_E2E_OLD_REF`; `src/web/js`, `led/src/web/js`, mapped in `serve.ts`) on the same layout to compare the UIs. A ref without `src/web/js` (the TypeScript front end, 5.0.0 on) is refused with a message: there is nothing to compare, the golden file is the baseline.
- Not covered: the Go tools against real chips, Unraid's own page chrome, and code paths that need the `/sys` of a UGREEN device. Those need hardware; say in the PR which model you tested on.

A new model in `web/models/` shows up in the harness, the screenshots and the site ([development.md](development.md#add-a-model)).

## Screenshots

`just shots [model ...]` drives the harness with `Bun.WebView` for every model in `web/models/` with `shots: true`, every applicable page and both themes. Output: `.build/shots/<model>/<page>-<theme>.png` and `index.json`. Pages: overview, case, fans, sensors, leds, settings, about, dashboard. `SHOTS_SCALE=2` renders at double resolution.

- macOS uses WebKit; Linux uses Chromium over CDP ([Bun.WebView](https://bun.sh/docs/runtime/webview)). The flake devShell provides it with Inter and DejaVu fonts and sets `BUN_CHROME_PATH` and `FONTCONFIG_FILE`. Outside the shell: `BUN_CHROME_PATH`, `chromium` on PATH, or nixpkgs on demand.
- `just e2e` runs the UI against `web/tools/e2e/golden.json`. The golden is proven for WebKit only: Chrome on macOS (`E2E_BACKEND=chrome`) passes single scenarios, a full run shows diffs. The CI job `e2e` is manual (`workflow_dispatch` input `e2e`), not part of `ci-ok`, and hangs on Linux so far.
- README hero and site images: `just site` also runs `just site-shots`, which renders the DXP6800 Pro overview, fans, case and LEDs (dark) and the overview (light) with the fixture of that model into `.build/site/shots/`; Pages serves them at `/shots/` (README links the `/` stream from `main`, `/next/shots/` is the development one). On pull requests `website.yml` builds without deploying and uploads them as the `site-shots` artifact. The flake `site` shell carries Chromium and the fonts like `e2e`.
- The only committed screenshots are the five in `docs/ca/`, still loaded by `plugins/ugreen-nas-control.xml` from `main`: `just ca-shots` renders the same five images into it. They are declared in `.gitattributes`.

## Site preview

`just site` validates `web/models/` and builds `.build/site` (one page per model). `just site-preview` builds it and serves it at `http://localhost:8788/ugreen-nas-control/`, the base path of GitHub Pages, and rebuilds on changes. `SITE_STREAM=next just site` adds the preview banner and noindex of the `/next/` stream (base URL via `SITE_URL`, released site via `ROOT_URL`). Which branch publishes where: [releasing.md](releasing.md#branches).
