#!/usr/bin/env bun
// Release helpers for the workflows (needs `gh` and GH_TOKEN; run in CI only).
//   bun plugin/tools/release.ts state   <tag>        none | draft | published | incomplete
//   bun plugin/tools/release.ts fetch   <tag> <dir>  download every asset of the release (a draft too)
//   bun plugin/tools/release.ts verify  <dir>        assets in <dir> match plugin/ugreen-nas-control.plg: archive hash, SHA256SUMS
//   bun plugin/tools/release.ts publish <tag> <sha>  create the tag at <sha> and flip the draft to published
// A release is complete when the archive and the plg are attached. Drafts are invisible to /releases/tags/<tag>,
// so they are looked up in the release list.
import { mkdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { $ } from 'bun';

const ROOT = join(import.meta.dir, '../..');
const REPO = process.env.GITHUB_REPOSITORY;

interface Asset {
    id: number;
    name: string;
}
interface Release {
    id: number;
    tag_name: string;
    draft: boolean;
    assets: Asset[];
}

const sha256 = (data: Uint8Array) => new Bun.CryptoHasher('sha256').update(data).digest('hex');

async function gh(...args: string[]): Promise<string> {
    const r = await $`gh ${args}`.nothrow().quiet();
    if (r.exitCode !== 0) throw new Error(`gh ${args.join(' ')}: ${r.stderr.toString().trim()}`);
    return r.stdout.toString();
}

function repo(): string {
    if (!REPO) throw new Error('GITHUB_REPOSITORY is not set');
    return REPO;
}

async function find(tag: string): Promise<Release | null> {
    const pages = JSON.parse(await gh('api', '--paginate', '--slurp', `repos/${repo()}/releases?per_page=100`)) as Release[][];
    return pages.flat().find(r => r.tag_name === tag) ?? null;
}

const archiveName = (tag: string) => `ugreen-nas-control-${tag.replace(/^v/, '')}.tar.xz`;

function complete(r: Release, tag: string): boolean {
    const names = new Set(r.assets.map(a => a.name));
    return names.has(archiveName(tag)) && names.has('ugreen-nas-control.plg');
}

async function state(tag: string): Promise<string> {
    const r = await find(tag);
    if (!r) return 'none';
    if (!complete(r, tag)) return 'incomplete';
    return r.draft ? 'draft' : 'published';
}

async function fetchAssets(tag: string, dir: string): Promise<void> {
    const r = await find(tag);
    if (!r) throw new Error(`no release ${tag}`);
    mkdirSync(dir, { recursive: true });
    for (const a of r.assets) {
        const r2 = await $`gh api -H "Accept: application/octet-stream" ${`repos/${repo()}/releases/assets/${a.id}`}`.nothrow().quiet();
        if (r2.exitCode !== 0) throw new Error(`download ${a.name}: ${r2.stderr.toString().trim()}`);
        await Bun.write(join(dir, a.name), r2.stdout);
    }
}

async function verify(dir: string): Promise<void> {
    const plg = readFileSync(join(ROOT, 'plugin/ugreen-nas-control.plg'), 'utf8');
    const version = plg.match(/<!ENTITY version\s+"([^"]+)">/)?.[1];
    const want = plg.match(/<SHA256>([0-9a-f]{64})<\/SHA256>/)?.[1];
    if (!version || !want) throw new Error('plugin/ugreen-nas-control.plg has no version or SHA256');
    const archive = await Bun.file(join(dir, archiveName(`v${version}`))).bytes();
    if (sha256(archive) !== want) throw new Error(`archive sha256 ${sha256(archive)} differs from the plg (${want})`);
    const released = await Bun.file(join(dir, 'ugreen-nas-control.plg')).text();
    if (released !== plg) throw new Error('the plg asset differs from plugin/ugreen-nas-control.plg');
    const sums = await Bun.file(join(dir, 'SHA256SUMS')).text();
    for (const line of sums.split('\n').filter(Boolean)) {
        const [hash, name] = line.split(/\s+/);
        if (sha256(await Bun.file(join(dir, name)).bytes()) !== hash) throw new Error(`SHA256SUMS: ${name} does not match`);
    }
    console.log(`release assets of ${version} match the plg`);
}

async function publish(tag: string, sha: string): Promise<void> {
    const r = await find(tag);
    if (!r) throw new Error(`no release ${tag}`);
    if (!r.draft) return console.log(`${tag} is already published`);
    const ref = (await $`git ls-remote --tags origin ${`refs/tags/${tag}`}`.nothrow().quiet().text()).trim().split(/\s+/)[0];
    if (ref && ref !== sha) throw new Error(`tag ${tag} exists at ${ref}, not at ${sha}`);
    await gh('release', 'edit', tag, '--repo', repo(), '--draft=false', '--target', sha, '--latest');
    console.log(`${tag} published at ${sha}`);
}

const [cmd, a, b] = process.argv.slice(2);
try {
    if (cmd === 'state' && a) console.log(await state(a));
    else if (cmd === 'fetch' && a && b) await fetchAssets(a, b);
    else if (cmd === 'verify' && a) await verify(a);
    else if (cmd === 'publish' && a && b) await publish(a, b);
    else {
        console.error('usage: release.ts state <tag> | fetch <tag> <dir> | verify <dir> | publish <tag> <sha>');
        process.exit(2);
    }
} catch (e) {
    console.error((e as Error).message);
    process.exit(1);
}
