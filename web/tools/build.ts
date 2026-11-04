// Builds the install tree of the plugin at .build/plugin (scripts/generated.ts): every non-binary file of plugin/files.txt
// at its install-relative path. Tracked sources (web/plugin) are copied, the JS bundles and PNG icons are
// built from web/src and web/assets. Nothing is written into the source tree.
//   bun web/tools/build.ts                  build the tree below the repo root
//   bun web/tools/build.ts --out DIR        build it below DIR instead (DIR/.build/plugin), e.g. to prove the build
//   bun web/tools/build.ts --no-icons       skip the icons (they need rsvg-convert)
//   bun web/tools/build.ts --verify         build the bundles into a temp dir and delete it: proves the sources build
// The bundles are classic IIFE, unminified, with the same window.* globals as the hand-written files they replaced.
import { chmodSync, copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { readManifest } from '../../plugin/tools/manifest';
import { WEB_BUNDLES, WEB_ICONS, WEB_TREE } from '../../scripts/generated';

const root = resolve(import.meta.dir, '../..');
export const TREE = WEB_TREE.replace(/\/$/, '');
const BANNER = '// Generated from web/src by `bun run build`. Do not edit.\n"use strict";\n';

async function bundle(entry: string): Promise<string> {
    const result = await Bun.build({ entrypoints: [resolve(root, entry)], format: 'iife', target: 'browser', minify: false, splitting: false });
    if (!result.success) {
        for (const log of result.logs) console.error(log);
        throw new Error(`build failed: ${entry}`);
    }
    const [output] = result.outputs;
    if (!output || result.outputs.length !== 1) throw new Error(`${entry}: expected one output file`);
    return BANNER + (await output.text());
}

function put(path: string, data: string | Uint8Array): void {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, data);
}

export async function buildWeb(out = root, opts: { icons?: boolean; quiet?: boolean } = {}): Promise<string[]> {
    const written: string[] = [];
    rmSync(resolve(out, TREE), { recursive: true, force: true });
    const generated = new Set([...Object.values(WEB_BUNDLES), ...WEB_ICONS.map(i => i.out)]);
    for (const e of await readManifest()) {
        if (e.source.startsWith('release:') || generated.has(e.source)) continue;
        const to = resolve(out, TREE, e.dest);
        mkdirSync(dirname(to), { recursive: true });
        copyFileSync(resolve(root, e.source), to);
        chmodSync(to, parseInt(e.mode, 8));
    }
    for (const [entry, target] of Object.entries(WEB_BUNDLES)) {
        const text = await bundle(entry);
        put(resolve(out, target), text);
        written.push(target);
        if (!opts.quiet) console.log(`${target}  ${text.split('\n').length} lines`);
    }
    if (opts.icons ?? true) {
        const rsvg = Bun.which('rsvg-convert');
        if (!rsvg) throw new Error('rsvg-convert not found (run it through `just web-build`)');
        for (const { svg, size, out: target } of WEB_ICONS) {
            const png = Bun.spawnSync([rsvg, '-w', String(size), '-h', String(size), resolve(root, svg)]);
            if (png.exitCode !== 0) throw new Error(`rsvg-convert ${svg}: ${png.stderr.toString().trim()}`);
            put(resolve(out, target), png.stdout);
            written.push(target);
            if (!opts.quiet) console.log(`${target}  ${size} px`);
        }
    }
    return written;
}

if (import.meta.main) {
    const args = process.argv.slice(2);
    if (args.includes('--verify')) {
        const tmp = mkdtempSync(join(tmpdir(), 'unc-build-'));
        try {
            const written = await buildWeb(tmp, { icons: false, quiet: true });
            console.log(`builds: ${written.length} bundles`);
        } finally {
            rmSync(tmp, { recursive: true, force: true });
        }
        process.exit(0);
    }
    const at = args.indexOf('--out');
    const out = at >= 0 ? resolve(args[at + 1] ?? '') : root;
    await buildWeb(out, { icons: !args.includes('--no-icons') });
}
