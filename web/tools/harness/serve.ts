// Local harness: the plugin's install tree (.build/plugin, made by `just web-build` from plugin/files.txt), served by `php -S`
// from a temp dir, with the APIs answering from the fixture of one model (web/models/). No Docker, no hardware, no
// real directory is touched.
//
//   bun web/tools/harness/serve.ts [model-id] [--port N]      serve until Ctrl-C
//   import { startHarness } from './serve'                  used by web/tools/shots.ts and web/tools/e2e
//
// Options of startHarness (all off by default, so the screenshots are not affected):
//   js: 'old'          serve the hand-written front end (src/web/js) of git ref `oldRef` (default upstream/main) instead of the build in the tree
//   requestLog: true   record every api.php request (NAS and LED) as JSON lines, read with h.requests()
//   inject: '<js>'     script put at the start of <head> of view.php (before the page's own scripts)
import { chmodSync, cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import type { Lang } from '../../src/core/i18n';
import { hardware, ledOutputs } from '../../models/fixture';
import { shape, withVariants } from '../../demo/variants';
import { fanHistory } from '../../demo/fixture-history';
import { loadModels } from '../../models/index';

const root = resolve(import.meta.dir, '../../..');
const PLUGIN = 'plugins/ugreen-nas-control';
const TREE = join(root, '.build/plugin');

export interface Harness {
    base: string;
    url(opts?: { dash?: boolean; shot?: boolean }): string;
    configure(opts: { theme?: 'dark' | 'light'; lang?: Lang }): void;
    dir: string;
    requests(): ApiRequest[];
    clearRequests(): void;
    stop(): void;
}

export interface ApiRequest {
    api: 'nas' | 'led';
    method: 'GET' | 'POST';
    action: string;
    query: Record<string, string>;
    body: Record<string, string>;
}

export interface HarnessOptions {
    port?: number | undefined;
    js?: 'new' | 'old' | undefined;
    oldRef?: string | undefined;
    requestLog?: boolean | undefined;
    inject?: string | undefined;
    /** Unix seconds: freezes the fan history to this moment (it otherwise moves with the clock). */
    now?: number | undefined;
}

// The plugin's PHP reads Unraid's absolute paths; in the temp tree they live under <dir>/root.
const rewrite = (text: string, fs: string) => text.replace(/(?<![\w.:-])\/(usr\/local\/emhttp|boot|var\/local\/emhttp|var\/run)(?=\/|\b)/g, m => fs + m);

function phpCommand(): string[] {
    if (Bun.which('php')) return ['php'];
    // Outside `nix develop`: borrow php 8.4 from nixpkgs.
    return ['nix', 'shell', 'nixpkgs#php84', '-c', 'php'];
}

async function freePort(): Promise<number> {
    const probe = Bun.listen({ hostname: '127.0.0.1', port: 0, socket: { data() {} } });
    const { port } = probe;
    probe.stop(true);
    return port;
}

// Every running harness stops its php on normal exit, on errors and on SIGINT/SIGTERM/SIGHUP, so none outlives its
// process. The temp dir name (unc-harness-<model>-<random>) is part of the php command line: after a stop,
// `pgrep -f unc-harness` finds nothing. `nix shell ... php` has a wrapper process; pkill -f on the unique dir
// name takes both.
const running = new Set<() => void>();
let hooked = false;
function hook(): void {
    if (hooked) return;
    hooked = true;
    process.on('exit', () => [...running].forEach(stop => stop()));
    for (const [sig, code] of [
        ['SIGINT', 130],
        ['SIGTERM', 143],
        ['SIGHUP', 129]
    ] as const) {
        process.on(sig, () => {
            [...running].forEach(stop => stop());
            process.exit(code);
        });
    }
}

export async function startHarness(modelId: string, opts: HarnessOptions = {}): Promise<Harness> {
    const known = withVariants(await loadModels());
    const model = known.find(m => m.id === modelId);
    if (!model) throw new Error(`unknown model "${modelId}"; known: ${known.map(m => m.id).join(', ')}`);

    const dir = mkdtempSync(join(tmpdir(), `unc-harness-${model.id}-`));
    const fsRoot = join(dir, 'root');
    const docroot = join(fsRoot, 'usr/local/emhttp');
    const plugin = join(docroot, PLUGIN);
    const boot = join(fsRoot, 'boot/config/plugins/ugreen-nas-control');
    mkdirSync(plugin, { recursive: true });
    mkdirSync(boot, { recursive: true });
    mkdirSync(join(fsRoot, 'var/local/emhttp'), { recursive: true });
    mkdirSync(join(fsRoot, 'var/run'), { recursive: true });

    for (const line of readFileSync(join(root, 'plugin/files.txt'), 'utf8').split('\n')) {
        const [src, dest] = line.trim().split(/\s+/);
        if (!src || src.startsWith('#') || src.startsWith('release:') || !dest) continue;
        const to = join(plugin, dest);
        mkdirSync(dirname(to), { recursive: true });
        if (opts.js === 'old' && /^(led\/)?js\/.*\.js$/.test(dest)) {
            // The hand-written front end of the old ref, on top of the unchanged layout (pages, CSS and PHP did not change).
            const ref = opts.oldRef ?? process.env.UNC_E2E_OLD_REF ?? 'upstream/main';
            // Where the old ref kept them: src/web/js and led/src/web/js.
            const oldPath = dest.startsWith('led/') ? `led/src/web/${dest.slice(4)}` : `src/web/${dest}`;
            const old = Bun.spawnSync(['git', '-C', root, 'show', `${ref}:${oldPath}`]);
            if (old.exitCode !== 0) throw new Error(`git show ${ref}:${oldPath}: ${old.stderr.toString().trim()}`);
            writeFileSync(to, old.stdout);
        } else if (/\.(php|page|sh)$/.test(dest) || /backend\//.test(dest)) writeFileSync(to, rewrite(readFileSync(join(TREE, dest), 'utf8'), fsRoot));
        else {
            if (!existsSync(join(TREE, dest))) throw new Error(`${dest} is missing in .build/plugin; run \`just web-build\` first`);
            cpSync(join(TREE, dest), to);
        }
    }

    const here = import.meta.dir;
    const born = opts.now ?? Math.floor(Date.now() / 1000);
    const shaped = shape(model);
    const fixture = {
        hardware: hardware(model),
        status: shaped.status,
        fan: shaped.fan,
        panel: shaped.panel,
        ...(shaped.powerAc ? { power_ac: shaped.powerAc } : {}),
        ...(shaped.cs201x ? { cs201x: shaped.cs201x } : {}),
        history: { born, frozen: opts.now !== undefined, '1h': fanHistory(model, born, '1h'), '24h': fanHistory(model, born, '24h') }
    };
    writeFileSync(join(plugin, 'fixture.json'), JSON.stringify(fixture));
    writeFileSync(join(plugin, 'api.php'), rewrite(readFileSync(join(here, 'api-fixture.php'), 'utf8'), fsRoot));
    writeFileSync(join(plugin, 'view.php'), rewrite(readFileSync(join(here, 'view.php'), 'utf8'), fsRoot));
    const back = join(plugin, 'led/backend');
    mkdirSync(back, { recursive: true });
    cpSync(join(here, 'fake-led-ctl'), join(back, 'ugreen-led-ctl'));
    chmodSync(join(back, 'ugreen-led-ctl'), 0o755);
    const led = ledOutputs(model);
    for (const [name, value] of Object.entries(led)) writeFileSync(join(back, `led-${name}.json`), JSON.stringify(value));
    for (const [name, value] of Object.entries(led.all)) writeFileSync(join(back, `led-get-${name}.json`), JSON.stringify(value));
    if (model.led !== 'none') writeFileSync(join(boot, 'led.enabled'), '');
    writeFileSync(join(boot, 'leds.cfg'), '');

    writeFileSync(join(fsRoot, 'var/local/emhttp/var.ini'), 'csrf_token="fake"\nmdState="STARTED"\n');
    // Latest released version = newest CHANGELOG.md heading (the tracked .plg is only rendered at release); other streams are marked.
    const stream = process.env.SITE_STREAM ?? 'main';
    const version = `${/^## (\d+\.\d+(?:\.\d+)?)\s*$/m.exec(readFileSync(join(root, 'CHANGELOG.md'), 'utf8'))?.[1] ?? 'dev'}${stream === 'main' ? '' : `+${stream}`}`;
    writeFileSync(join(fsRoot, 'boot/config/plugins/ugreen-nas-control.plg'), `<!ENTITY version "${version}">\n`);

    const cfg = { ui_theme: 'dark', ui_lang: 'en' };
    const writeCfg = () =>
        writeFileSync(
            join(boot, 'fan.cfg'),
            Object.entries(cfg)
                .map(([k, v]) => `${k}="${v}"`)
                .join('\n') + '\n'
        );
    writeCfg();

    const requestLog = join(dir, 'requests.jsonl');
    const env: Record<string, string> = { ...(process.env as Record<string, string>) };
    const prepend: string[] = [];
    if (opts.requestLog || opts.inject) {
        writeFileSync(join(dir, 'prepend.php'), readFileSync(join(here, 'prepend.php'), 'utf8'));
        prepend.push('-d', `auto_prepend_file=${join(dir, 'prepend.php')}`);
        if (opts.requestLog) env.UNC_REQUEST_LOG = requestLog;
        if (opts.inject) {
            writeFileSync(join(dir, 'inject.html'), `<script>${opts.inject}</script>`);
            env.UNC_INJECT_FILE = join(dir, 'inject.html');
        }
    }

    const port = opts.port ?? (await freePort());
    const php = Bun.spawn(
        [...phpCommand(), '-d', 'display_errors=stderr', '-d', 'error_reporting=E_ALL', ...prepend, '-S', `127.0.0.1:${port}`, '-t', docroot],
        {
            cwd: dir,
            env,
            stdout: 'ignore',
            stderr: Bun.file(join(dir, 'php.log'))
        }
    );
    let stopped = false;
    const stop = () => {
        if (stopped) return;
        stopped = true;
        running.delete(stop);
        php.kill();
        Bun.spawnSync(['pkill', '-f', dir], { stdout: 'ignore', stderr: 'ignore' }); // the nix wrapper and any child
        const log = join(dir, 'php.log');
        if (existsSync(log) && /(Warning|Notice|Fatal error|Deprecated|Parse error):/.test(readFileSync(log, 'utf8')))
            console.error(`harness: PHP reported errors, see ${log}`);
        else rmSync(dir, { recursive: true, force: true });
    };
    running.add(stop);
    hook();
    const base = `http://127.0.0.1:${port}`;
    for (let i = 0; i < 100; i++) {
        try {
            if ((await fetch(`${base}/${PLUGIN}/api.php?action=hardware`)).ok) break;
        } catch {
            /* not up yet */
        }
        if (i === 99) {
            const log = join(dir, 'php.log');
            stop();
            throw new Error(`php did not start; see ${log}`);
        }
        await Bun.sleep(100);
    }
    if (opts.requestLog) writeFileSync(requestLog, ''); // without the readiness probe above

    return {
        base,
        dir,
        requests: () =>
            existsSync(requestLog)
                ? readFileSync(requestLog, 'utf8')
                      .split('\n')
                      .filter(Boolean)
                      .map(line => JSON.parse(line) as ApiRequest)
                : [],
        clearRequests: () => writeFileSync(requestLog, ''),
        url: ({ dash = false, shot = false } = {}) => `${base}/${PLUGIN}/view.php?${[dash && 'p=dash', shot && 'shot=1'].filter(Boolean).join('&')}`,
        configure(o) {
            if (o.theme) cfg.ui_theme = o.theme;
            if (o.lang) cfg.ui_lang = o.lang;
            writeCfg();
        },
        stop
    };
}

if (import.meta.main) {
    const args = process.argv.slice(2);
    const portAt = args.indexOf('--port');
    const port = portAt >= 0 ? Number(args.splice(portAt, 2)[1]) : undefined;
    const id = args[0] ?? 'dxp6800-pro';
    const h = await startHarness(id, { port });
    console.log(`${id}: ${h.url()}\n        dashboard tile: ${h.url({ dash: true })}\nCtrl-C to stop`);
    const stop = () => (h.stop(), process.exit(0));
    process.on('SIGINT', stop);
    process.on('SIGTERM', stop);
    await new Promise(() => {});
}
