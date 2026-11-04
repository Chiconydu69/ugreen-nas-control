// Screenshots of the plugin UI for every model in web/models/: model x page x theme, desktop width, written to
// .build/shots/<model>/<page>-<theme>.png, plus .build/shots/index.json (what exists) and the social
// preview (.build/social-preview.png, 1280x640).
//
//   bun web/tools/shots.ts [model-id ...] [--page overview,case] [--theme dark|light] [--no-social]
//
// Browser: Bun.WebView. macOS uses WebKit (nothing to install); Linux uses Chrome over CDP: BUN_CHROME_PATH, a
// `chromium` on PATH, or nixpkgs#chromium fetched on demand (CI). SHOTS_BACKEND=chrome forces Chrome on macOS
// (BUN_CHROME_PATH or Google Chrome.app). SHOTS_SCALE=2 renders at device pixel ratio 2 (Chrome only).
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { $ } from 'bun';
import { fixtureHasChip } from '../models/fixture';
import { loadModels } from '../models/index';
import type { Model } from '../models/schema';
import { type Harness, startHarness } from './harness/serve';
import { isCs201x, isReadOnlyFan, withVariants } from '../demo/variants';
import { hasBeeper, hasButtons } from '../src/features/nas/panel-models';
import { socialHtml } from '../../docs/site/social';

const root = resolve(import.meta.dir, '../..');
const OUT = process.env.SHOTS_OUT ?? join(root, '.build/shots');
const WIDTH = 1440;
const MIN_HEIGHT = 900;
const MAX_HEIGHT = 4200;
const THEMES = ['dark', 'light'] as const;

interface Page {
    id: string;
    nav?: string;
    dash?: boolean;
    applies(m: Model): boolean;
    prepare?: string;
    wait?: string;
}

export const PAGES: Page[] = [
    { id: 'overview', nav: 'overview', applies: () => true },
    { id: 'case', nav: 'case', applies: m => m.caseView !== 'none' },
    {
        id: 'fans',
        nav: 'curves',
        applies: m => fixtureHasChip(m) || m.fan?.kind === 'probe',
        prepare: `(() => { const f = document.querySelector('.unc-fan-shaping-fold'); if (f) f.open = true; })()`
    },
    {
        // A probe model after "Run probe": the result table with the role pickers.
        id: 'fans-probe',
        nav: 'curves',
        applies: m => m.fan?.kind === 'probe' && !isCs201x(m),
        prepare: `document.querySelector('#unc-fm-probe [data-act="run"]')?.click()`,
        wait: `document.querySelector('#unc-fm-probe select[data-role]')`
    },
    {
        // Drive and source checklists of the temperature source, and the quiet-hours fields.
        id: 'fans-edit',
        nav: 'curves',
        applies: m => fixtureHasChip(m) && !isReadOnlyFan(m),
        prepare: `(() => {
            const pick = (id, value) => { const s = document.querySelector(id); s.value = value; s.dispatchEvent(new Event('change', { bubbles: true })); };
            pick('#unc-fi-cpu-src', 'disk_max');
            pick('#unc-fi-case-src', 'max');
            const quiet = document.querySelector('#unc-sq-enabled');
            quiet.checked = true;
            quiet.dispatchEvent(new Event('change', { bubbles: true }));
        })()`
    },
    { id: 'history', nav: 'history', applies: m => fixtureHasChip(m) && !isReadOnlyFan(m), wait: `document.querySelector('.unc-hist-svg polyline')` },
    {
        id: 'history-24h',
        nav: 'history',
        applies: m => fixtureHasChip(m) && !isReadOnlyFan(m),
        prepare: `document.querySelector('[data-range="24h"]').click()`,
        wait: `document.querySelector('[data-range="24h"]').getAttribute('aria-pressed') === 'true' && document.querySelector('.unc-hist-svg polyline')`
    },
    {
        // The tooltip of the second fan, 24 h, at a point where something limits the speed.
        id: 'history-tip',
        nav: 'history',
        applies: m => fixtureHasChip(m) && !isReadOnlyFan(m),
        prepare: `(async () => {
            document.querySelector('[data-range="24h"]').click();
            const end = Date.now() + 8000;
            while (!document.querySelector('.unc-hist-fan:nth-of-type(2) .unc-hist-svg polyline') && Date.now() < end) await new Promise(r => setTimeout(r, 100));
            const plot = document.querySelector('.unc-hist-fan:nth-of-type(2) .unc-hist-plot');
            const box = plot.getBoundingClientRect();
            plot.dispatchEvent(new PointerEvent('pointermove', { bubbles: true, clientX: box.left + box.width * 0.62, clientY: box.top + 120 }));
        })()`,
        wait: `document.querySelector('.unc-hist-tip:not([hidden])')`
    },
    { id: 'safety', nav: 'safety', applies: m => fixtureHasChip(m) && !isReadOnlyFan(m) },
    {
        // The DXP2800 GT before "Confirm fans": the CS201x card with its diagnostics.
        id: 'fans-cs201x',
        nav: 'curves',
        applies: isCs201x,
        wait: `document.querySelector('#unc-fm-cs201x .unc-facts')`
    },
    {
        // The front panel: button state (read once on opening) and the beeper.
        id: 'panel',
        nav: 'panel',
        applies: m => hasButtons(m.fixtureDmi) || hasBeeper(m.fixtureDmi),
        wait: `document.querySelector('#unc-fm-panel-buttons .unc-pill, #unc-fm-panel-beeper .unc-btn')`
    },
    { id: 'sensors', nav: 'sensors', applies: () => true },
    { id: 'leds', nav: 'led:overview', applies: m => m.led !== 'none' },
    { id: 'settings', nav: 'settings', applies: () => true },
    { id: 'about', nav: 'about', applies: () => true, prepare: `(() => { const f = document.querySelector('#unc-models-fold'); if (f) f.open = true; })()` },
    { id: 'dashboard', dash: true, applies: () => true }
];

async function chromePath(): Promise<string> {
    if (process.env.BUN_CHROME_PATH) return process.env.BUN_CHROME_PATH;
    if (process.platform === 'darwin') return '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
    const onPath = Bun.which('chromium') ?? Bun.which('google-chrome-stable');
    if (onPath) return onPath;
    const out = (await $`nix build --inputs-from . --no-link --print-out-paths nixpkgs#chromium`.text()).trim().split('\n')[0];
    return join(out, 'bin/chromium');
}

/** Linux/nix has no system fonts: point fontconfig at Inter and DejaVu so text renders the same everywhere. */
async function nixFonts(): Promise<void> {
    if (process.platform !== 'linux' || process.env.FONTCONFIG_FILE) return;
    const dirs = (await $`nix build --inputs-from . --no-link --print-out-paths nixpkgs#inter nixpkgs#dejavu_fonts`.text()).trim().split('\n');
    const dir = join(tmpdir(), 'unc-fontconfig');
    mkdirSync(dir, { recursive: true });
    const conf = join(dir, 'fonts.conf');
    writeFileSync(
        conf,
        `<?xml version="1.0"?><fontconfig>${dirs.map(d => `<dir>${d}/share/fonts</dir>`).join('')}<cachedir>${dir}/cache</cachedir></fontconfig>`
    );
    process.env.FONTCONFIG_FILE = conf;
}

async function openBrowser(width: number, height: number): Promise<{ view: Bun.WebView; scale: number; backend: string }> {
    const chrome = process.platform !== 'darwin' || process.env.SHOTS_BACKEND === 'chrome';
    if (!chrome) return { view: new Bun.WebView({ width, height }), scale: 1, backend: 'webkit' };
    await nixFonts();
    const path = await chromePath();
    const argv = [
        '--no-sandbox',
        '--disable-dev-shm-usage',
        '--hide-scrollbars',
        '--force-color-profile=srgb',
        '--font-render-hinting=none',
        '--disable-lcd-text',
        '--disable-gpu'
    ];
    const view = new Bun.WebView({ width, height, backend: { type: 'chrome', path, argv, stderr: 'inherit' } });
    await view.navigate('about:blank'); // Chrome has no target to resize before the first navigation
    return { view, scale: Number(process.env.SHOTS_SCALE ?? 1), backend: `chrome (${path})` };
}

const js = async <T>(view: Bun.WebView, expr: string) => (await view.evaluate(expr)) as T;

async function waitFor(view: Bun.WebView, expr: string, what: string, ms = 15000) {
    const end = Date.now() + ms;
    while (Date.now() < end) {
        if (await js<boolean>(view, `Boolean(${expr})`)) return;
        await Bun.sleep(100);
    }
    throw new Error(`timeout waiting for ${what}`);
}

async function setSize(view: Bun.WebView, scale: number, height: number) {
    await view.resize(WIDTH, height);
    if (scale !== 1) await view.cdp('Emulation.setDeviceMetricsOverride', { width: WIDTH, height, deviceScaleFactor: scale, mobile: false });
}

async function shoot(view: Bun.WebView, scale: number, file: string, width = WIDTH, bottom = '.unc') {
    // Grow the viewport to the content so nothing is cut off (the UI scrolls the page, not an inner box).
    const h = Math.ceil(await js<number>(view, `(document.querySelector(${JSON.stringify(bottom)})?.getBoundingClientRect().bottom ?? 0) + 40`));
    const height = Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, h));
    if (width === WIDTH) await setSize(view, scale, height);
    else {
        await view.resize(width, height);
        if (scale !== 1) await view.cdp('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: scale, mobile: false });
    }
    await Bun.sleep(250);
    mkdirSync(join(file, '..'), { recursive: true });
    await Bun.write(file, await view.screenshot());
    return Bun.file(file).size;
}

async function openPage(view: Bun.WebView, h: Harness, dash: boolean) {
    await view.navigate(h.url({ dash, shot: true }));
    if (dash) {
        await waitFor(view, `document.querySelector('.ugreen-tile, [class*="unc-dash"], table td')`, 'dashboard tile');
    } else {
        await waitFor(view, `document.querySelector('#unc-online') && !/…$/.test(document.querySelector('#unc-online').textContent)`, 'status loaded');
    }
    await Bun.sleep(1200); // polls and the first draw settle
}

const args = process.argv.slice(2);
const opt = (name: string) => {
    const i = args.indexOf(name);
    return i >= 0 ? args.splice(i, 2)[1] : undefined;
};
const onlyPages = opt('--page')?.split(',');
const onlyTheme = opt('--theme');
const social = !args.includes('--no-social');
const ids = args.filter(a => !a.startsWith('--'));

// The pseudo models (idx6011-plain) are only shot when named, so a full run keeps the registry's set of screenshots.
const models = (ids.length === 0 ? await loadModels() : withVariants(await loadModels())).filter(m => m.shots && (ids.length === 0 || ids.includes(m.id)));
if (models.length === 0) throw new Error(`no model with screenshots matches ${ids.join(', ')}`);

const { view, scale, backend } = await openBrowser(WIDTH, MIN_HEIGHT);
console.log(`backend: ${backend}, ${WIDTH}px wide, scale ${scale}`);

const manifestPath = join(OUT, 'index.json');
const manifest: Record<string, Record<string, string[]>> = ids.length === 0 && !onlyPages && !onlyTheme
    ? {}
    : await Bun.file(manifestPath)
          .json()
          .catch(() => ({}));
if (ids.length === 0 && !onlyPages && !onlyTheme) rmSync(OUT, { recursive: true, force: true });

let count = 0;
let bytes = 0;
try {
    for (const m of models) {
        const h = await startHarness(m.id);
        try {
            manifest[m.id] ??= {};
            for (const theme of THEMES) {
                if (onlyTheme && onlyTheme !== theme) continue;
                h.configure({ theme, lang: 'en' });
                for (const page of PAGES) {
                    if (!page.applies(m) || (onlyPages && !onlyPages.includes(page.id))) continue;
                    await setSize(view, scale, MIN_HEIGHT);
                    await openPage(view, h, Boolean(page.dash));
                    if (page.nav) {
                        await js(view, `document.querySelector('[data-nav="${page.nav}"]').click()`);
                        await Bun.sleep(700);
                    }
                    if (page.prepare) await js(view, page.prepare);
                    if (page.wait) await waitFor(view, page.wait, `${page.id} after prepare`);
                    await Bun.sleep(300);
                    const file = join(OUT, m.id, `${page.id}-${theme}.png`);
                    const size = await shoot(view, scale, file, page.dash ? 500 : WIDTH, page.dash ? 'table' : '.unc');
                    (manifest[m.id][page.id] ??= []).includes(theme) || manifest[m.id][page.id].push(theme);
                    count++;
                    bytes += size;
                    console.log(`${m.id}/${page.id}-${theme}.png  ${(size / 1024).toFixed(0)} kB`);
                }
            }
        } finally {
            h.stop();
        }
    }

    mkdirSync(OUT, { recursive: true });
    writeFileSync(manifestPath, JSON.stringify(manifest, null, 2) + '\n');

    if (social && (await Bun.file(join(OUT, 'dxp6800-pro/overview-dark.png')).exists())) {
        const server = Bun.serve({
            port: 0,
            async fetch(req) {
                const path = new URL(req.url).pathname;
                if (path === '/') return new Response(socialHtml(), { headers: { 'content-type': 'text/html' } });
                const f = Bun.file(join(dirname(OUT), path));
                return (await f.exists()) ? new Response(f) : new Response('not found', { status: 404 });
            }
        });
        await view.resize(1280, 640);
        if (scale !== 1) await view.cdp('Emulation.setDeviceMetricsOverride', { width: 1280, height: 640, deviceScaleFactor: scale, mobile: false });
        await view.navigate(`http://127.0.0.1:${server.port}/`);
        await waitFor(view, `document.fonts.status === 'loaded' && [...document.images].every(i => i.complete)`, 'social preview');
        await Bun.sleep(300);
        await Bun.write(join(dirname(OUT), 'social-preview.png'), await view.screenshot());
        server.stop(true);
        console.log('social-preview.png  1280x640');
    }
} finally {
    view.close();
}
console.log(`${count} screenshots, ${(bytes / 1024 / 1024).toFixed(1)} MB`);
