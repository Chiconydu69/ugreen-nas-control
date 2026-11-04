// Builds the site demo into .build/demo (scripts/generated.ts): the plugin's real pages, rendered once through the harness,
// running the unmodified bundles of .build/plugin against the in-browser API of web/demo/.
//
//   bun web/tools/demo.ts           build .build/demo; also copy it into .build/site/demo when the site is built
//   bun web/tools/demo.ts --sync    only copy .build/demo into .build/site/demo (the site build wipes .build/site)
//
// Needs the install tree (`just web-build`) and php (the harness). Entry: demo/index.html?model=<id>&page=<page>&theme=&lang=&view=dashboard.
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { gzipSync } from 'node:zlib';
import { DEMO_DIR, DEMO_SITE_DIR, WEB_TREE } from '../../scripts/generated';
import { PLG_URL } from '../../docs/site/content';
import { loadModels } from '../models/index';
import { startHarness } from './harness/serve';

const root = resolve(import.meta.dir, '../..');
const src = join(root, 'web/demo');
const out = join(root, DEMO_DIR);
const site = join(root, DEMO_SITE_DIR);
const PLUGIN = '/plugins/ugreen-nas-control/';
const ASSETS = ['css', 'js', 'images', 'icons', 'led/css', 'led/js', 'led/images'];

export function syncIntoSite(): boolean {
    if (!existsSync(join(root, '.build/site'))) return false;
    if (!existsSync(out)) throw new Error(`${DEMO_DIR} is missing; run \`just demo\``);
    rmSync(site, { recursive: true, force: true });
    cpSync(out, site, { recursive: true });
    return true;
}

function frame(html: string, kind: 'app' | 'tile', led: boolean): string {
    let h = html.replaceAll(PLUGIN, 'assets/');
    const globals =
        kind === 'app'
            ? ['UGREEN_NAS_LANG', 'UGREEN_NAS_LANG_SETTING', 'UGREEN_NAS_THEME', ...(led ? ['UGREEN_LED_LANG', 'UGREEN_LED_THEME'] : [])]
            : ['UGREEN_NAS_LANG', 'UGREEN_NAS_THEME'];
    for (const name of globals) {
        const next = h.replace(new RegExp(`window\\.${name} = [^;\\n]*;`), `window.${name} = __UNC_DEMO.${name};`);
        if (next === h) throw new Error(`${kind}: page global ${name} not found; web/plugin/*.page changed, adapt web/tools/demo.ts`);
        h = next;
    }
    const swaps: [RegExp | string, string][] = [
        ['<html>', `<html data-demo="${kind}" data-led="${led ? 1 : 0}">`],
        ['<title>UGREEN NAS Control</title>', '<title>UGREEN NAS Control demo</title>'],
        [
            '<meta charset="utf-8">',
            '<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><script src="demo.js"></script>' +
                '<style>html{background:var(--demo-bg)}body{font-family:Inter,system-ui,-apple-system,"Segoe UI",sans-serif;color:var(--demo-fg)}</style>'
        ],
        ...(kind === 'tile' ? ([['<table style="width:430px">', '<table style="width:100%;max-width:430px;margin:0 auto">']] as [string, string][]) : []),
        [/(<body style="[^"]*?)background:#[0-9a-f]{6}/, '$1background:var(--demo-bg)']
    ];
    for (const [from, to] of swaps) {
        const next = h.replace(from, to);
        if (next === h) throw new Error(`${kind}: ${String(from)} not found in the rendered page; harness/view.php changed, adapt web/tools/demo.ts`);
        h = next;
    }
    return h;
}

async function render(): Promise<Record<string, string>> {
    const h = await startHarness('dxp6800-pro');
    const post = (value: 'on' | 'off') =>
        fetch(`${h.base}${PLUGIN}api.php`, { method: 'POST', body: new URLSearchParams({ action: 'led', value, csrf_token: 'fake' }) }).then(r => {
            if (!r.ok) throw new Error(`harness: led ${value}: HTTP ${r.status}`);
        });
    const get = async (dash: boolean) => (await fetch(h.url({ dash }))).text();
    try {
        const pages: Record<string, string> = {};
        pages['app.html'] = frame(await get(false), 'app', true);
        pages['tile.html'] = frame(await get(true), 'tile', true);
        await post('off');
        pages['app-noled.html'] = frame(await get(false), 'app', false);
        pages['tile-noled.html'] = frame(await get(true), 'tile', false);
        return pages;
    } finally {
        h.stop();
    }
}

async function bundle(entry: string, models: unknown): Promise<string> {
    const result = await Bun.build({
        entrypoints: [join(src, entry)],
        format: 'iife',
        target: 'browser',
        minify: true,
        define: { __MODELS__: JSON.stringify(models), __PLG_URL__: JSON.stringify(PLG_URL) }
    });
    if (!result.success) {
        for (const log of result.logs) console.error(log);
        throw new Error(`build failed: ${entry}`);
    }
    return (await result.outputs[0]!.text()) + '\n';
}

const size = (dir: string): { raw: number; gz: number; files: number } => {
    let raw = 0;
    let gz = 0;
    let files = 0;
    for (const name of readdirSync(dir, { recursive: true }) as string[]) {
        const path = join(dir, name);
        if (!statSync(path).isFile()) continue;
        const data = readFileSync(path);
        raw += data.length;
        gz += gzipSync(data).length;
        files++;
    }
    return { raw, gz, files };
};

if (import.meta.main) {
    if (process.argv.includes('--sync')) {
        console.log(syncIntoSite() ? `${DEMO_SITE_DIR} updated` : 'no .build/site yet, nothing to sync');
        process.exit(0);
    }
    const tree = join(root, WEB_TREE);
    if (!existsSync(join(tree, 'js/app.js'))) throw new Error(`${WEB_TREE} is missing; run \`just web-build\` first`);

    const models = (await loadModels()).map(({ note: _note, dmiPattern: _dmi, shots: _shots, ...keep }) => keep);
    rmSync(out, { recursive: true, force: true });
    mkdirSync(out, { recursive: true });
    for (const [name, html] of Object.entries(await render())) writeFileSync(join(out, name), html);
    for (const dir of ASSETS) {
        mkdirSync(dirname(join(out, 'assets', dir)), { recursive: true });
        cpSync(join(tree, dir), join(out, 'assets', dir), { recursive: true });
    }
    writeFileSync(join(out, 'demo.js'), await bundle('shim.ts', models));
    writeFileSync(join(out, 'outer.js'), await bundle('outer.ts', models));
    cpSync(join(src, 'index.html'), join(out, 'index.html'));
    cpSync(join(src, 'demo.css'), join(out, 'demo.css'));

    const { raw, gz, files } = size(out);
    console.log(`${DEMO_DIR}  ${files} files, ${(raw / 1024).toFixed(0)} kB (${(gz / 1024).toFixed(0)} kB gzip), ${models.length} models`);
    if (syncIntoSite()) console.log(`${DEMO_SITE_DIR} updated`);
}
