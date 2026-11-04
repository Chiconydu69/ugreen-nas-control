#!/usr/bin/env bun
// Packs the plugin and renders the .plg from plugin/ugreen-nas-control.plg.tmpl + plugin/files.txt + CHANGELOG.md.
//   bun plugin/tools/plg.ts pack   [--version X.Y.Z] [--next --run N] [--out .build/dist] [--binary-dir .build/bin] [--binary name=path]
//   bun plugin/tools/plg.ts render [--version X.Y.Z] [--next --run N] [--out .build/dist] [--archive path] [--changes 20]
//   bun plugin/tools/plg.ts check  [--binary-dir .build/bin]
//   bun plugin/tools/plg.ts serve  [--version X.Y.Z] [--port 8790] [--host IP]   test install from this machine (see below)
// pack builds the web files (web/tools/build.ts) and writes ONE archive, <out>/ugreen-nas-control-<version>.tar.xz: every
// file of plugin/files.txt at its install-relative path, binaries from .build/bin/. The archive is reproducible (sorted
// entries, fixed mtime, uid/gid 0), so the same sources give the same SHA256.
// render writes the .plg with the archive's hashes; the plugin manager downloads that one file from the release.
// Stable: version X.Y or X.Y.Z (plugin/tools/version.ts), archive from the release v<X.Y>, installed as ugreen-nas-control.plg.
// Next:   version X.(Y+1).0-next.NNNNNN (X.Y.Z = latest stable, NNNNNN = CI run, zero-padded), archive from the Pages folder /next/ (website.yml, only the current build),
//         installed as ugreen-nas-control.plg too (Unraid names the installed file after the URL's basename), so the channels switch in place;
//         the pluginURL points at the Pages copy so the update check follows the channel.
// Test:   --test (render, pack) or `serve`: version X.Y.Z.tYYYYMMDDHHMM, installed as ugreen-nas-control.plg from
//         --release-url / --plugin-url (this machine). The plugin manager installs only a version that sorts above the
//         installed one as text (strcmp): X.Y.Z.t... is above X.Y.Z and below the next patch. Written to
//         .build/plg-test; only --test accepts the two URL options, `check` refuses a stable or next plg without GitHub URLs.
import { mkdirSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { networkInterfaces, tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { $ } from 'bun';
import { buildWeb, TREE } from '../../web/tools/build.ts';
import { parseChangelog, readFragments, sortedTexts, VERSION_RE } from './changes.ts';
import { localVersion, nextAfter, nextVersion, orderProblems } from './version.ts';
import { ALL_OUTPUTS } from '../../scripts/generated.ts';
import { type Entry, type Link, linkTarget, MANIFEST, readLinks, readManifest } from './manifest.ts';

const ROOT = join(import.meta.dir, '../..');
const REPO = 'ugreen-nas-control/ugreen-nas-control';
const TMPL = join(ROOT, 'plugin/ugreen-nas-control.plg.tmpl');
// Folders whose tracked files must all be in the manifest.
const SHIPPED = ['web/plugin', 'plugin/event'];
// Fixed modification time of every archive entry (2020-01-01 UTC). The installer extracts with tar -m, so installed
// files get the install time.
const MTIME = 1577836800;
// Paths the web pages, the PHP callers and Unraid call by name; they must not move.
const TOOL_NAMES = ['backend/ugreen-nas-ctl', 'backend/ugreen-nas-fan', 'led/backend/ugreen-led-ctl', 'led/backend/ugreen-led-i2c'];
const FIXED_PATHS = [...TOOL_NAMES, 'event/stopping', 'api.php', 'led/api.php', 'UGREENNASControl.page', 'UGREENNASDashboard.page'];

function args() {
    const a = process.argv.slice(3);
    const val = (n: string) => {
        const i = a.indexOf(`--${n}`);
        return i >= 0 ? a[i + 1] : undefined;
    };
    const all = (n: string) => a.flatMap((x, i) => (x === `--${n}` ? [a[i + 1]] : []));
    return { has: (n: string) => a.includes(`--${n}`), val, all };
}

const hex = (alg: 'sha256' | 'md5', data: Uint8Array) => new Bun.CryptoHasher(alg).update(data).digest('hex');
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');

async function latestStable(): Promise<string> {
    const log = parseChangelog(await Bun.file(join(ROOT, 'CHANGELOG.md')).text());
    if (!log.length) throw new Error('CHANGELOG.md has no release');
    return log[0].version;
}


interface Opts {
    version: string; // as written in the plg
    next: boolean;
    test?: boolean;
    releaseTag: string;
    pluginURL: string;
    releaseURL: string;
    plgFile: string;
    archive: string;
}

async function changesBlock(o: Opts, base: string, n: number): Promise<string> {
    const log = parseChangelog(await Bun.file(join(ROOT, 'CHANGELOG.md')).text());
    const from = log.findIndex(r => r.version === base);
    const releases = (from >= 0 ? log.slice(from) : log).slice(0, n);
    const parts: string[] = [];
    if (o.next || o.test) {
        const pending = sortedTexts(readFragments().fragments);
        if (pending.length) parts.push(`### ${o.version}\n${pending.map(t => `- ${esc(t)}`).join('\n')}`);
    }
    for (const r of releases) parts.push(`### ${r.version}\n${r.lines.map(l => `- ${esc(l)}`).join('\n')}`);
    return parts.join('\n\n') + '\n';
}

// ---- archive ----------------------------------------------------------------------------------------------

const octal = (n: number, width: number) => n.toString(8).padStart(width - 1, '0') + '\0';

/** One ustar header (512 bytes). type 2: symbolic link to `link`. */
function tarHeader(name: string, mode: number, size: number, type: '0' | '2' | '5', link = ''): Uint8Array {
    const h = new Uint8Array(512);
    const put = (s: string, at: number) => h.set(new TextEncoder().encode(s), at);
    if (new TextEncoder().encode(name).length > 99) throw new Error(`archive path too long: ${name}`);
    put(name, 0);
    put(octal(mode, 8), 100);
    put(octal(0, 8), 108); // uid
    put(octal(0, 8), 116); // gid
    put(octal(size, 12), 124);
    put(octal(MTIME, 12), 136);
    put('        ', 148); // checksum counts these as spaces
    put(type, 156);
    if (link) {
        if (new TextEncoder().encode(link).length > 99) throw new Error(`archive link target too long: ${link}`);
        put(link, 157);
    }
    put('ustar\0' + '00', 257);
    put('root', 265);
    put('root', 297);
    const sum = h.reduce((a, b) => a + b, 0);
    put(sum.toString(8).padStart(6, '0') + '\0 ', 148);
    return h;
}

/** Deterministic tar: directories and files sorted by path, owner root, fixed mtime. */
export function makeTar(files: { path: string; mode: number; data: Uint8Array; link?: string }[]): Uint8Array {
    const sorted = [...files].sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
    const dirs = new Set<string>();
    for (const f of sorted) for (let d = dirname(f.path); d !== '.'; d = dirname(d)) dirs.add(d);
    const parts: Uint8Array[] = [];
    const all = [...dirs.values().map(d => ({ path: `${d}/`, mode: 0o755, data: new Uint8Array(0), dir: true, link: undefined as string | undefined })), ...sorted.map(f => ({ ...f, dir: false }))];
    all.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
    for (const e of all) {
        parts.push(tarHeader(e.path, e.mode, e.data.length, e.dir ? '5' : e.link ? '2' : '0', e.link));
        if (!e.dir && !e.link) {
            parts.push(e.data);
            parts.push(new Uint8Array((512 - (e.data.length % 512)) % 512));
        }
    }
    parts.push(new Uint8Array(1024));
    return Buffer.concat(parts);
}

/** Entries of an (uncompressed) tar: name, mode, type, size. */
export function listTar(tar: Uint8Array): { name: string; mode: number; type: string; size: number; link: string }[] {
    const out: { name: string; mode: number; type: string; size: number; link: string }[] = [];
    const text = (b: Uint8Array) => new TextDecoder().decode(b).replace(/\0.*$/s, '');
    for (let at = 0; at + 512 <= tar.length;) {
        const h = tar.subarray(at, at + 512);
        if (h.every(b => b === 0)) break;
        const size = parseInt(text(h.subarray(124, 136)), 8);
        out.push({ name: text(h.subarray(0, 100)), mode: parseInt(text(h.subarray(100, 108)), 8), type: text(h.subarray(156, 157)), size, link: text(h.subarray(157, 257)) });
        at += 512 + Math.ceil(size / 512) * 512;
    }
    return out;
}

async function xz(args: string[], input: Uint8Array): Promise<Uint8Array> {
    if (!Bun.which('xz')) throw new Error('xz not found (run it through `just plg`)');
    const p = Bun.spawn(['xz', ...args], { stdin: new Blob([input]), stdout: 'pipe', stderr: 'pipe' });
    const [out, err, code] = await Promise.all([new Response(p.stdout).bytes(), new Response(p.stderr).text(), p.exited]);
    if (code !== 0) throw new Error(`xz ${args.join(' ')}: ${err.trim()}`);
    return out;
}

/** Bytes of a manifest entry: from the install tree when given (pack), else from its source column; binaries from the build folder. null when missing. */
async function sourceBytes(e: Entry, binDir: string, binaries: Map<string, string>, tree?: string): Promise<Uint8Array | null> {
    const path = e.source.startsWith('release:')
        ? (binaries.get(e.source.slice(8)) ?? join(binDir, e.source.slice(8)))
        : tree
          ? join(tree, e.dest)
          : join(ROOT, e.source);
    const file = Bun.file(path);
    return (await file.exists()) ? new Uint8Array(await file.arrayBuffer()) : null;
}

/** The archive (tar.xz) of the manifest. `placeholder`: missing sources become a stand-in (for `check`). */
export async function buildArchive(manifest: Entry[], links: Link[], binDir: string, binaries: Map<string, string>, placeholder = false, tree?: string): Promise<Uint8Array> {
    const files: { path: string; mode: number; data: Uint8Array; link?: string }[] = [];
    for (const l of links) files.push({ path: l.dest, mode: 0o777, data: new Uint8Array(0), link: l.target });
    for (const e of manifest) {
        let data = await sourceBytes(e, binDir, binaries, tree);
        if (!data) {
            if (!placeholder) throw new Error(`${e.source} not found${e.source.startsWith('release:') ? ` (run just go-build, or pass --binary-dir)` : ''}`);
            data = new TextEncoder().encode(`placeholder for ${e.source}\n`);
        }
        files.push({ path: e.dest, mode: parseInt(e.mode, 8), data });
    }
    return xz(['-6', '--check=crc32', '-c'], makeTar(files));
}

// ---- options ----------------------------------------------------------------------------------------------

const GITHUB_RELEASES = `https://github.com/${REPO}/releases/download/`;
// The next channel lives on GitHub Pages (website.yml): base derived from GITHUB_REPOSITORY like the workflow does.
const [PAGES_OWNER, PAGES_NAME] = (process.env.GITHUB_REPOSITORY || REPO).split('/');
const NEXT_PAGES = `https://${PAGES_OWNER.toLowerCase()}.github.io/${PAGES_NAME}/next`;

function stamp(d = new Date()): string {
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}${p(d.getHours())}${p(d.getMinutes())}`;
}

async function options(a: ReturnType<typeof args>): Promise<{ opts: Opts; base: string }> {
    const base = a.val('version') ?? (await latestStable());
    if (!VERSION_RE.test(base)) throw new Error(`version "${base}" must be X.Y.Z, or X.Y for CHANGELOG headings up to 4.x (Unraid compares plugin versions as text)`);
    if ((a.has('release-url') || a.has('plugin-url')) && !a.has('test')) throw new Error('--release-url and --plugin-url are for --test renders only');
    if (a.has('test')) {
        if (a.has('next')) throw new Error('--test and --next exclude each other');
        const releaseURL = a.val('release-url');
        const pluginURL = a.val('plugin-url');
        if (!releaseURL || !pluginURL) throw new Error('--test needs --release-url and --plugin-url');
        const version = localVersion(base, a.val('stamp') ?? stamp());
        if (!/^\d\.\d\.\d\.t\d{12}$/.test(version)) throw new Error(`test version ${version} must be X.Y.Z.tYYYYMMDDHHMM`);
        return {
            base,
            opts: {
                version,
                next: false,
                test: true,
                releaseTag: 'test',
                plgFile: 'ugreen-nas-control.plg',
                archive: `ugreen-nas-control-${version}.tar.xz`,
                releaseURL: releaseURL.replace(/\/+$/, ''),
                pluginURL
            }
        };
    }
    if (!a.has('next')) {
        return {
            base,
            opts: {
                version: base,
                next: false,
                releaseTag: `v${base}`,
                plgFile: 'ugreen-nas-control.plg',
                archive: `ugreen-nas-control-${base}.tar.xz`,
                releaseURL: `${GITHUB_RELEASES}v${base}`,
                pluginURL: `https://raw.githubusercontent.com/${REPO}/main/plugin/ugreen-nas-control.plg`
            }
        };
    }
    const run = a.val('run');
    if (!run || !/^\d{1,6}$/.test(run)) throw new Error('--next needs --run N (CI run number, up to 6 digits)');
    const version = nextVersion(base, run);
    return {
        base,
        opts: {
            version,
            next: true,
            releaseTag: 'next-build',
            plgFile: 'ugreen-nas-control.plg',
            archive: `ugreen-nas-control-${version}.tar.xz`,
            releaseURL: NEXT_PAGES,
            pluginURL: `${NEXT_PAGES}/ugreen-nas-control.plg`
        }
    };
}

const outDir = (a: ReturnType<typeof args>) => (a.val('out') ? resolve(a.val('out') ?? '') : join(ROOT, a.has('test') ? '.build/plg-test' : '.build/dist'));

function binaryMap(a: ReturnType<typeof args>) {
    return new Map(a.all('binary').map(b => b.split('=', 2) as [string, string]));
}

async function build(opts: Opts, base: string, manifest: Entry[], links: Link[], archive: Uint8Array, nChanges: number): Promise<string> {
    const names = (mode: string) =>
        manifest
            .filter(e => e.mode === mode)
            .map(e => e.dest)
            .join(' ');
    const vars: Record<string, string> = {
        VERSION: opts.version,
        RELEASE_URL: opts.releaseURL,
        PLUGIN_URL: opts.pluginURL,
        ARCHIVE: opts.archive,
        ARCHIVE_SHA256: hex('sha256', archive),
        ARCHIVE_MD5: hex('md5', archive),
        EXEC_FILES: names('755'),
        DATA_FILES: names('644'),
        LINKS: links.map(l => `${l.dest}=${l.target}`).join(' '),
        CHANGES: await changesBlock(opts, base, nChanges)
    };
    const tmpl = readFileSync(TMPL, 'utf8');
    return tmpl.replace(/@([A-Z0-9_]+)@/g, (m, k) => {
        if (!(k in vars)) throw new Error(`template: unknown placeholder ${m}`);
        return vars[k];
    });
}

async function xmllint(file: string): Promise<string | null> {
    if (!Bun.which('xmllint')) return null;
    const r = await $`xmllint --noout ${file}`.nothrow().quiet();
    return r.exitCode === 0 ? '' : r.stderr.toString();
}

async function pack(a: ReturnType<typeof args>): Promise<number> {
    const { opts } = await options(a);
    const out = outDir(a);
    await buildWeb(ROOT, { quiet: true });
    const archive = await buildArchive(await readManifest(), await readLinks(), a.val('binary-dir') ?? join(ROOT, '.build/bin'), binaryMap(a), false, join(ROOT, TREE));
    mkdirSync(out, { recursive: true });
    const file = join(out, opts.archive);
    await Bun.write(file, archive);
    console.log(`${file} (${archive.length} bytes, sha256 ${hex('sha256', archive)})`);
    return 0;
}

async function render(a: ReturnType<typeof args>): Promise<number> {
    const { opts, base } = await options(a);
    const out = outDir(a);
    const archiveFile = a.val('archive') ?? join(out, opts.archive);
    if (!(await Bun.file(archiveFile).exists())) throw new Error(`${archiveFile} not found (run: bun plugin/tools/plg.ts pack ...)`);
    const archive = new Uint8Array(await Bun.file(archiveFile).arrayBuffer());
    const text = await build(opts, base, await readManifest(), await readLinks(), archive, Number(a.val('changes') ?? 20));
    mkdirSync(out, { recursive: true });
    const file = join(out, opts.plgFile);
    await Bun.write(file, text);
    console.log(`${file} (version ${opts.version}, archive ${opts.archive})`);
    return 0;
}

async function check(a: ReturnType<typeof args>): Promise<number> {
    const problems: string[] = [];
    const manifest = await readManifest();
    const links = await readLinks();
    const generated = new Set(ALL_OUTPUTS);
    for (const e of manifest) {
        const generatedSource = generated.has(e.source);
        if (!e.source.startsWith('release:') && !generatedSource && !(await Bun.file(join(ROOT, e.source)).exists()))
            problems.push(`files.txt: ${e.source} does not exist`);
        if (e.dest.startsWith('/') || e.dest.includes('..')) problems.push(`files.txt: bad destination ${e.dest}`);
    }
    const have = new Set([...manifest.map(e => e.dest), ...links.map(l => l.dest)]);
    for (const p of FIXED_PATHS) if (!have.has(p)) problems.push(`files.txt: the install path ${p} is gone`);
    const targets = new Set(TOOL_NAMES.map(t => links.find(l => l.dest === t)).map(l => (l ? linkTarget(l) : '')));
    if (targets.size !== 1 || targets.has('')) problems.push(`files.txt: ${TOOL_NAMES.join(', ')} must be links to one binary`);
    const tracked = (await $`git -C ${ROOT} ls-files ${SHIPPED}`.text()).split('\n').filter(Boolean);
    const listed = new Set(manifest.map(e => e.source));
    for (const f of tracked) if (!listed.has(f)) problems.push(`${f} is shipped from a tracked folder but not in plugin/files.txt`);

    const tmpl = readFileSync(TMPL, 'utf8');
    const placeholders = new Set([...tmpl.matchAll(/@([A-Z0-9_]+)@/g)].map(m => m[1]));
    for (const need of ['VERSION', 'CHANGES', 'ARCHIVE', 'ARCHIVE_SHA256', 'EXEC_FILES', 'DATA_FILES', 'LINKS'])
        if (!placeholders.has(need)) problems.push(`template lacks @${need}@`);

    // Archive of the manifest (stand-ins for what is not built), then both channels rendered without network.
    const tmp = mkdtempSync(join(tmpdir(), 'plg-check-'));
    const binDir = a.val('binary-dir') ?? join(ROOT, '.build/bin');
    const base = await latestStable();
    try {
        let archive = new Uint8Array(0);
        if (!Bun.which('xz')) console.error('xz not found, archive not checked');
        else {
            archive = await buildArchive(manifest, links, binDir, binaryMap(a), true);
            const again = await buildArchive(manifest, links, binDir, binaryMap(a), true);
            if (hex('sha256', archive) !== hex('sha256', again)) problems.push('archive is not reproducible');
            const entries = listTar(await xz(['-dc'], archive));
            const files = entries.filter(e => e.type === '0');
            if (files.length !== manifest.length) problems.push(`archive has ${files.length} files for ${manifest.length} manifest lines`);
            const symlinks = entries.filter(e => e.type === '2');
            if (symlinks.length !== links.length || entries.some(e => !['0', '2', '5'].includes(e.type))) problems.push('archive: links do not match plugin/files.txt, or an entry is neither file, link nor folder');
            for (const l of links) {
                const got = symlinks.find(s => s.name === l.dest);
                if (!got || got.link !== l.target) problems.push(`archive: link ${l.dest} -> ${l.target} missing`);
                if (!manifest.some(e => e.dest === linkTarget(l) && e.mode === '755')) problems.push(`files.txt: link ${l.dest} points to ${linkTarget(l)}, which is no executable file of the archive`);
            }
            for (const e of manifest) {
                const got = files.find(f => f.name === e.dest);
                if (!got) problems.push(`archive lacks ${e.dest}`);
                else if (got.mode !== parseInt(e.mode, 8)) problems.push(`archive: ${e.dest} has mode ${got.mode.toString(8)}, want ${e.mode}`);
            }
        }
        const variants: [string, string[]][] = [
            ['stable', []],
            ['next', ['--next', '--run', '1']]
        ];
        for (const [label, extra] of variants) {
            try {
                const fake = {
                    ...a,
                    has: (n: string) => extra.includes(`--${n}`),
                    val: (n: string) => (n === 'version' ? base : n === 'run' ? '1' : undefined)
                };
                const { opts } = await options(fake);
                const text = await build(opts, base, manifest, links, archive, 20);
                const f = join(tmp, `${label}.plg`);
                await Bun.write(f, text);
                const lint = await xmllint(f);
                if (lint === null) console.error('xmllint not found, XML well-formedness not checked');
                else if (lint) problems.push(`${label} plg is not well-formed XML:\n${lint}`);
                if (!/version="&version;"/.test(text) || !text.includes(`<!ENTITY version   "${opts.version}">`))
                    problems.push(`${label}: version entity missing`);
                if (opts.next) {
                    // Unraid compares versions as text: this build must sort above the stable and below the stable after next.
                    const later = nextAfter(base);
                    if (!(opts.version > base) || !(opts.version < nextAfter(later))) problems.push(`next: version ${opts.version} does not sort between ${base} and ${nextAfter(later)}`);
                    problems.push(...orderProblems(base));
                }
                const count = (text.match(/<FILE Name=/g) ?? []).length;
                if (count !== 1) problems.push(`${label}: ${count} FILE downloads, want the one archive`);
                if (!text.includes(opts.archive)) problems.push(`${label}: archive name ${opts.archive} missing`);
                const releaseURL = text.match(/<!ENTITY releaseURL "([^"]*)">/)?.[1] ?? '';
                if (!releaseURL.startsWith(opts.next ? NEXT_PAGES : GITHUB_RELEASES)) problems.push(`${label}: releaseURL ${releaseURL} is not a GitHub ${opts.next ? 'Pages folder' : 'release'} of ${REPO}`);
            } catch (err) {
                problems.push(`${label}: ${(err as Error).message}`);
            }
        }
    } finally {
        rmSync(tmp, { recursive: true, force: true });
    }
    // Local URLs are for test renders only.
    for (const extra of [['--release-url', 'http://192.0.2.1:8790'], ['--plugin-url', 'http://192.0.2.1:8790/ugreen-nas-control.plg']]) {
        try {
            await options({ ...a, has: (n: string) => n === extra[0].slice(2), val: (n: string) => (n === 'version' ? base : undefined) });
            problems.push(`options accepted ${extra[0]} without --test`);
        } catch {
            // refused, as intended
        }
    }
    // The Community Applications entry must point at the plg and carry its minimum Unraid version.
    const ca = readFileSync(join(ROOT, 'plugins/ugreen-nas-control.xml'), 'utf8');
    const caUrl = ca.match(/<PluginURL>([^<]+)<\/PluginURL>/)?.[1];
    const caMin = ca.match(/<MinVer>([^<]+)<\/MinVer>/)?.[1];
    const tmplMin = readFileSync(TMPL, 'utf8').match(/^\s*min="([^"]+)"/m)?.[1];
    const stableUrl = (await options({ ...a, has: () => false, val: (n: string) => (n === 'version' ? base : undefined) })).opts.pluginURL;
    if (caUrl !== stableUrl) problems.push(`plugins/ugreen-nas-control.xml: PluginURL is ${caUrl}, the plg has ${stableUrl}`);
    if (caMin !== tmplMin) problems.push(`plugins/ugreen-nas-control.xml: MinVer is ${caMin}, the template has min="${tmplMin}"`);

    const committed = join(ROOT, 'plugin/ugreen-nas-control.plg');
    if (!(await Bun.file(committed).text()).includes('Generated by plugin/tools/plg.ts'))
        problems.push('plugin/ugreen-nas-control.plg lacks the generated-file header');

    for (const p of problems) console.error(p);
    if (!problems.length)
        console.log(`plg ok (${manifest.length} files in one archive, stable and next render, ${dirname(MANIFEST).split('/').pop()}/files.txt consistent)`);
    return problems.length ? 1 : 0;
}

/** Packs and renders a test plg for this machine and serves it, so the real install path runs on a NAS in the LAN. */
async function serve(a: ReturnType<typeof args>): Promise<number> {
    const port = Number(a.val('port') ?? 8790);
    const host =
        a.val('host') ??
        Object.values(networkInterfaces())
            .flat()
            .find(i => i && i.family === 'IPv4' && !i.internal)?.address;
    if (!host) throw new Error('no LAN address found; pass --host IP');
    const base = `http://${host}:${port}`;
    const vals: Record<string, string | undefined> = { version: a.val('version'), 'release-url': base, 'plugin-url': `${base}/ugreen-nas-control.plg` };
    const t = {
        has: (n: string) => n === 'test' || n in vals || a.has(n),
        val: (n: string) => vals[n] ?? a.val(n),
        all: a.all
    };
    await pack(t);
    await render(t);
    const { opts } = await options(t);
    const dir = outDir(t);
    const server = Bun.serve({
        hostname: '0.0.0.0',
        port,
        async fetch(req) {
            const name = new URL(req.url).pathname.slice(1);
            const file = [opts.plgFile, opts.archive].includes(name) ? Bun.file(join(dir, name)) : null;
            console.log(`${req.method} /${name} -> ${file ? 200 : 404}`);
            return file ? new Response(file) : new Response('not found', { status: 404 });
        }
    });
    console.log(`\nServing ${dir} on ${base} (version ${opts.version}). On the NAS:\n\n  plugin install ${base}/ugreen-nas-control.plg\n`);
    console.log('Roll back: plugin install <stable plg URL> forced (a lower version needs forced). Settings survive. Ctrl-C stops the server.');
    await new Promise<void>(done => {
        for (const sig of ['SIGINT', 'SIGTERM'] as const)
            process.on(sig, () => {
                void server.stop(true);
                done();
            });
    });
    return 0;
}

if (import.meta.main) {
    const cmd = process.argv[2];
    const a = args();
    try {
        process.exit(
            cmd === 'pack'
                ? await pack(a)
                : cmd === 'render'
                  ? await render(a)
                  : cmd === 'check'
                    ? await check(a)
                    : cmd === 'serve'
                      ? await serve(a)
                      : (console.error('usage: plg.ts pack|render|check|serve'), 2)
        );
    } catch (e) {
        console.error((e as Error).message);
        process.exit(1);
    }
}
