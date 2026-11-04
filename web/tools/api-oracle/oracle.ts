// Replays the case table (cases.json) through the real web/plugin/api.php and web/plugin/led/api.php with fake tools and
// reports what came back: status, headers, body, the config files after, the tool calls, PHP's own errors.
//
//   bun web/tools/api-oracle/oracle.ts            compare with golden.json, exit 1 on a difference
//   bun web/tools/api-oracle/oracle.ts --update   re-record golden.json
//   bun web/tools/api-oracle/oracle.ts <id>...    only the cases whose id starts with one of these
//
// The PHP files are copied into a temp root and their absolute Unraid paths rewritten into it (as web/tools/harness/serve.ts does);
// nothing outside the temp dir is touched. The clock is frozen (time() becomes NOW) and /proc/<pid> of run_detached() is a
// directory the fake tool keeps while a rule sleeps, so the replay is the same on Linux and macOS.
//
// cases.json: { base: { nas, led }, cases }. base holds the files and the fake-tool rules every case of that API starts with;
// a case adds its own (its rules are tried first, `files` entries replace, null removes). A rule answers a call of `fan`,
// `ctl`, `led` or `nas` (the multicall binary) when its args are a prefix of the argv (and its stdin equals the piped text, when given): out (string as is,
// else JSON plus newline), err, code, sleep (ms, keeps /proc/<pid> alive). Request: method (default GET, POST with a body),
// query, body (form fields; "values[k]" keys as sent, or a raw string), upload, missing/nonexec (tools), blockConfig (the
// config folder is a file, so every save fails, also as root). A body, query or upload value "@repeat:<text>:<n>" is <text> n times.
// File paths: absolute Unraid paths, or a bare name inside /boot/config/plugins/ugreen-nas-control (fan.cfg, leds.cfg).
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, realpathSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, relative, resolve } from 'node:path';

const root = resolve(import.meta.dir, '../../..');
export const CASES_FILE = join(import.meta.dir, 'cases.json');
export const GOLDEN_FILE = join(import.meta.dir, 'golden.json');
export const NOW = 1760000000;
const PLUGIN = 'usr/local/emhttp/plugins/ugreen-nas-control';
const CONFIG_DIR = 'boot/config/plugins/ugreen-nas-control';

export interface Rule {
    args: string[];
    stdin?: string;
    out?: unknown;
    err?: string;
    code?: number;
    sleep?: number;
}

export interface Upload {
    field?: string;
    name?: string;
    content?: string;
    repeat?: string;
    count?: number;
}

export interface Case {
    id: string;
    api: 'nas' | 'led';
    note?: string;
    knownBug?: string;
    method?: string;
    query?: Record<string, string> | string;
    body?: Record<string, string> | string;
    contentType?: string;
    upload?: Upload;
    files?: Record<string, string | null>;
    fan?: Rule[];
    ctl?: Rule[];
    led?: Rule[];
    nas?: Rule[];
    missing?: string[];
    nonexec?: string[];
    blockConfig?: boolean;
}

export interface Base {
    files?: Record<string, string>;
    fan?: Rule[];
    ctl?: Rule[];
    led?: Rule[];
    nas?: Rule[];
}

export interface CaseTable {
    base: { nas: Base; led: Base };
    cases: Case[];
}

export interface Call {
    tool: string;
    args: string[];
    stdin?: string;
}

export interface Result {
    status: number;
    headers: Record<string, string>;
    body?: unknown;
    raw: string;
    files?: Record<string, string | null>;
    calls: Call[];
    phpErrors?: string[];
}

export const loadCases = (): CaseTable => JSON.parse(readFileSync(CASES_FILE, 'utf8')) as CaseTable;
export const loadGolden = (): Record<string, Result> => JSON.parse(readFileSync(GOLDEN_FILE, 'utf8')) as Record<string, Result>;

const rewrite = (text: string, fs: string) =>
    text.replace(/(?<![\w.:-])\/(usr\/local\/emhttp|boot|var\/local\/emhttp|var\/run|proc)(?=\/|\b)/g, m => fs + m).replace(/\btime\(\)/g, String(NOW));

export function phpCommand(): string[] {
    return Bun.which('php') ? ['php'] : ['nix', 'shell', 'nixpkgs#php84', '-c', 'php'];
}

const TOOLS: Record<string, string> = {
    'ugreen-nas-fan': `${PLUGIN}/backend/ugreen-nas-fan`,
    'ugreen-nas-ctl': `${PLUGIN}/backend/ugreen-nas-ctl`,
    'ugreen-led-ctl': `${PLUGIN}/led/backend/ugreen-led-ctl`,
    'ugreen-nas': `${PLUGIN}/backend/ugreen-nas`
};

function walk(dir: string, out: Record<string, string> = {}, base = dir): Record<string, string> {
    if (!existsSync(dir) || !statSync(dir).isDirectory()) return out;
    for (const name of readdirSync(dir).sort()) {
        const p = join(dir, name);
        if (statSync(p).isDirectory()) walk(p, out, base);
        else out[relative(base, p)] = readFileSync(p, 'utf8');
    }
    return out;
}

export interface Oracle {
    run(c: Case, base: Base): Promise<Result>;
    stop(): void;
}

export async function startOracle(): Promise<Oracle> {
    const dir = mkdtempSync(join(realpathSync(tmpdir()), 'unc-oracle-'));
    const fs = join(dir, 'root');
    const plugin = join(fs, PLUGIN);
    mkdirSync(join(plugin, 'include'), { recursive: true });
    mkdirSync(join(plugin, 'led'), { recursive: true });
    const src = join(root, 'web/plugin');
    const copy = (rel: string) => writeFileSync(join(plugin, rel), rewrite(readFileSync(join(src, rel), 'utf8'), fs));
    copy('api.php');
    copy('led/api.php');
    for (const f of readdirSync(join(src, 'include'))) if (f.endsWith('.php')) copy(`include/${f}`);
    writeFileSync(join(fs, 'usr/local/emhttp/ping.txt'), 'up');

    const fake = `#!${process.execPath}\n${rewrite(readFileSync(join(import.meta.dir, 'fake-tool.ts'), 'utf8').replace(/^#!.*\n/, ''), fs)}`;
    const installTools = () => {
        for (const path of Object.values(TOOLS)) {
            mkdirSync(dirname(join(fs, path)), { recursive: true });
            writeFileSync(join(fs, path), fake);
            chmodSync(join(fs, path), 0o755);
        }
    };

    const log = join(dir, 'php.log');
    const probe = Bun.listen({ hostname: '127.0.0.1', port: 0, socket: { data() {} } });
    const port = probe.port;
    probe.stop(true);
    const php = Bun.spawn(
        [
            ...phpCommand(),
            '-d',
            'upload_max_filesize=2M',
            '-d',
            'post_max_size=8M',
            '-d',
            'serialize_precision=-1',
            '-d',
            'memory_limit=256M',
            '-d',
            'display_errors=0',
            '-d',
            'log_errors=1',
            '-d',
            `error_log=${log}`,
            '-d',
            'error_reporting=E_ALL',
            '-S',
            `127.0.0.1:${port}`,
            '-t',
            join(fs, 'usr/local/emhttp')
        ],
        { cwd: dir, stdout: 'ignore', stderr: 'ignore' }
    );
    let stopped = false;
    const stop = () => {
        if (stopped) return;
        stopped = true;
        php.kill();
        Bun.spawnSync(['pkill', '-f', dir], { stdout: 'ignore', stderr: 'ignore' });
        rmSync(dir, { recursive: true, force: true });
    };
    process.on('exit', stop);
    const base = `http://127.0.0.1:${port}`;
    for (let i = 0; ; i++) {
        try {
            if ((await fetch(`${base}/ping.txt`)).ok) break;
        } catch {
            /* not up yet */
        }
        if (i >= 150) {
            stop();
            throw new Error('php did not start');
        }
        await Bun.sleep(100);
    }

    let logAt = 0;
    const scrub = (s: string) => s.replaceAll(fs, '');
    const abs = (p: string) => join(fs, p.startsWith('/') ? p : `${CONFIG_DIR}/${p}`);
    const expand = (v: string) => v.replace(/^@repeat:(.*):(\d+)$/s, (_, text: string, n: string) => text.repeat(Number(n)));
    const expandAll = (o: Record<string, string>) => Object.entries(o).map(([k, v]) => [k, expand(v)] as [string, string]);

    return {
        stop,
        async run(c, defaults) {
            for (const sub of ['boot', 'var', 'proc']) rmSync(join(fs, sub), { recursive: true, force: true });
            for (const sub of ['var/run/unc-oracle', 'proc', CONFIG_DIR]) mkdirSync(join(fs, sub), { recursive: true });
            for (const [path, content] of Object.entries({ ...defaults.files, ...c.files })) {
                if (content === null) continue;
                mkdirSync(dirname(abs(path)), { recursive: true });
                writeFileSync(abs(path), content);
            }
            if (c.blockConfig) {
                rmSync(join(fs, CONFIG_DIR), { recursive: true, force: true });
                writeFileSync(join(fs, CONFIG_DIR), '');
            }
            const before = walk(join(fs, 'boot'));
            const rules = Object.fromEntries((['fan', 'ctl', 'led', 'nas'] as const).map(k => [k, [...(c[k] ?? []), ...(defaults[k] ?? [])]]));
            writeFileSync(join(fs, 'var/run/unc-oracle/tools.json'), JSON.stringify(rules));
            writeFileSync(join(fs, 'var/run/unc-oracle/calls.jsonl'), '');
            installTools();
            for (const name of c.missing ?? []) rmSync(join(fs, TOOLS[name]!));
            for (const name of c.nonexec ?? []) chmodSync(join(fs, TOOLS[name]!), 0o644);

            const method = c.method ?? (c.body !== undefined || c.upload ? 'POST' : 'GET');
            const query = typeof c.query === 'string' ? c.query : new URLSearchParams(expandAll(c.query ?? {})).toString();
            const url = `${base}/plugins/ugreen-nas-control/${c.api === 'nas' ? '' : 'led/'}api.php${query ? `?${query}` : ''}`;
            const init: RequestInit = { method };
            const fields = typeof c.body === 'object' ? expandAll(c.body) : [];
            if (c.upload) {
                const form = new FormData();
                for (const [k, v] of fields) form.append(k, v);
                const u = c.upload;
                form.append(
                    u.field ?? 'file',
                    new File([u.content ?? (u.repeat ?? 'a').repeat(u.count ?? 0)], u.name ?? 'profile.json'),
                    u.name ?? 'profile.json'
                );
                init.body = form;
            } else if (typeof c.body === 'string') {
                init.body = expand(c.body);
                init.headers = { 'Content-Type': c.contentType ?? 'application/x-www-form-urlencoded' };
            } else if (c.body !== undefined) {
                init.body = new URLSearchParams(fields).toString();
                init.headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
            }
            const res = await fetch(url, init);
            const text = scrub(await res.text());

            const after = walk(join(fs, 'boot'));
            const files: Record<string, string | null> = {};
            for (const k of Object.keys({ ...before, ...after }).sort())
                if (before[k] !== after[k]) files[`/boot/${k}`] = after[k] === undefined ? null : scrub(after[k]);
            const calls = readFileSync(join(fs, 'var/run/unc-oracle/calls.jsonl'), 'utf8')
                .split('\n')
                .filter(Boolean)
                .map(l => JSON.parse(scrub(l)) as Call);
            await Bun.sleep(5);
            const logText = existsSync(log) ? readFileSync(log, 'utf8') : '';
            const phpErrors = logText
                .slice(logAt)
                .split('\n')
                .flatMap(l => /^\[[^\]]+\] PHP ((?:Warning|Notice|Fatal error|Deprecated|Parse error): .*)$/.exec(l)?.[1] ?? [])
                .map(l =>
                    scrub(l)
                        .replace(/ on line \d+$/, '')
                        .replace(/\.php:\d+$/, '.php')
                        .replace(/\.\d+(\.[0-9a-f]{8})?\.tmp\b/, '.<pid>.tmp')
                        .trim()
                );
            logAt = logText.length;

            const headers: Record<string, string> = {};
            for (const h of ['content-type', 'content-disposition']) if (res.headers.has(h)) headers[h] = res.headers.get(h)!;
            let body: unknown;
            try {
                body = JSON.parse(text);
            } catch {
                /* not JSON */
            }
            return {
                status: res.status,
                headers,
                ...(body === undefined ? {} : { body }),
                raw: text,
                ...(Object.keys(files).length ? { files } : {}),
                calls,
                ...(phpErrors.length ? { phpErrors } : {})
            };
        }
    };
}

// Bytes count for what is not compact JSON (a pretty export, no JSON at all) and for replies of scalars only ({"ok":true},
// {"ok":false,"error":...}). Anything with a list or object inside is compared as parsed JSON: key order is free and an
// empty list equals an empty object (PHP prints [] for both).
export function exactBytes(r: Pick<Result, 'body' | 'raw'>): boolean {
    if (r.body === undefined || r.raw.trimEnd().includes('\n')) return true;
    return typeof r.body === 'object' && r.body !== null && !Array.isArray(r.body) && Object.values(r.body).every(v => typeof v !== 'object' || v === null);
}

const normalize = (v: unknown): unknown =>
    Array.isArray(v)
        ? v.length === 0
            ? {}
            : v.map(normalize)
        : v && typeof v === 'object'
          ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, normalize(x)]))
          : v;

export function diff(golden: Result, actual: Result): string[] {
    const out: string[] = [];
    const same = (a: unknown, b: unknown) => Bun.deepEquals(a, b, true);
    const show = (a: unknown, b: unknown) => `\n  golden ${JSON.stringify(a)}\n  actual ${JSON.stringify(b)}`;
    if (golden.status !== actual.status) out.push(`status ${golden.status} -> ${actual.status}`);
    if (!same(golden.headers, actual.headers)) out.push(`headers${show(golden.headers, actual.headers)}`);
    if (exactBytes(golden)) {
        if (golden.raw !== actual.raw) out.push(`body bytes${show(golden.raw, actual.raw)}`);
    } else if (!same(normalize(golden.body), normalize(actual.body))) out.push(`body${show(golden.body, actual.body)}`);
    if (!same(golden.files ?? {}, actual.files ?? {})) out.push(`files${show(golden.files, actual.files)}`);
    if (!same(golden.calls, actual.calls)) out.push(`calls${show(golden.calls, actual.calls)}`);
    if (!same(golden.phpErrors ?? [], actual.phpErrors ?? [])) out.push(`phpErrors${show(golden.phpErrors, actual.phpErrors)}`);
    return out;
}

// JSON as `just fmt` leaves it (2 spaces, objects always open, a list of scalars on one line when it fits in 160 columns), so a re-record never differs from the formatted file.
export function stringify(value: unknown, indent = '', lead = 0): string {
    if (Array.isArray(value)) {
        if (value.length === 0) return '[]';
        if (value.every(v => typeof v !== 'object' || v === null)) {
            const line = `[${value.map(v => JSON.stringify(v)).join(', ')}]`;
            if (indent.length + lead + line.length + 1 <= 160) return line;
        }
        return `[\n${value.map(v => `${indent}  ${stringify(v, indent + '  ')}`).join(',\n')}\n${indent}]`;
    }
    if (value && typeof value === 'object') {
        const entries = Object.entries(value);
        if (entries.length === 0) return '{}';
        return `{\n${entries.map(([k, v]) => `${indent}  ${JSON.stringify(k)}: ${stringify(v, indent + '  ', JSON.stringify(k).length + 2)}`).join(',\n')}\n${indent}}`;
    }
    return JSON.stringify(value);
}

export async function replay(only: string[] = []): Promise<Record<string, Result>> {
    const table = loadCases();
    const oracle = await startOracle();
    try {
        const results: Record<string, Result> = {};
        for (const c of table.cases) {
            if (only.length && !only.some(p => c.id.startsWith(p))) continue;
            results[c.id] = await oracle.run(c, table.base[c.api]);
        }
        return results;
    } finally {
        oracle.stop();
    }
}

if (import.meta.main) {
    const args = process.argv.slice(2);
    const only = args.filter(a => !a.startsWith('--'));
    const results = await replay(only);
    if (args.includes('--update')) {
        const old = only.length && existsSync(GOLDEN_FILE) ? loadGolden() : {};
        const merged = Object.fromEntries(loadCases().cases.flatMap(c => ((results[c.id] ?? old[c.id]) ? [[c.id, results[c.id] ?? old[c.id]]] : [])));
        writeFileSync(GOLDEN_FILE, stringify(merged) + '\n');
        console.log(`recorded ${Object.keys(results).length} cases into ${relative(root, GOLDEN_FILE)}`);
    } else {
        const golden = loadGolden();
        let bad = 0;
        for (const [id, r] of Object.entries(results)) {
            const d = golden[id] ? diff(golden[id], r) : ['no golden'];
            if (d.length) {
                bad++;
                console.error(`${id}: ${d.join('\n')}`);
            }
        }
        console.log(`${Object.keys(results).length - bad}/${Object.keys(results).length} cases match`);
        process.exit(bad ? 1 : 0);
    }
}
