#!/usr/bin/env bun
// Changelog fragments: one .changes/<slug>.md per PR, folded into CHANGELOG.md at release.
//   bun plugin/tools/changes.ts check
//   bun plugin/tools/changes.ts release --version 5.0.1   (scheme: plugin/tools/version.ts)
import { existsSync, readdirSync, readFileSync, unlinkSync } from 'node:fs';
import { join } from 'node:path';
import { BASE_RE, releaseProblem } from './version.ts';

export const ROOT = join(import.meta.dir, '../..');
export const TYPES = ['feat', 'fix', 'model', 'docs', 'chore'] as const;
export type ChangeType = (typeof TYPES)[number];
export const VERSION_RE = BASE_RE;

const CHANGES_DIR = join(ROOT, '.changes');
const CHANGELOG = join(ROOT, 'CHANGELOG.md');
const SLUG_RE = /^[a-z0-9][a-z0-9-]*\.md$/;
const HEADER = `# Changelog

User-visible changes per release, newest first. Written by \`bun plugin/tools/changes.ts release\` from the files in .changes/; the plugin's change list is generated from this file.
`;

export interface Fragment {
    file: string;
    type: ChangeType;
    text: string;
}
export interface Release {
    version: string;
    lines: string[];
}

/** Returns the fragment or an error message. */
export function parseFragment(file: string, src: string): Fragment | string {
    const m = src.replace(/\r\n/g, '\n').match(/^---\n([\s\S]*?)\n---\n([\s\S]*)$/);
    if (!m) return `${file}: needs a frontmatter block (--- / type: feat / ---)`;
    const keys = [...m[1].matchAll(/^(\w+):\s*(.*?)\s*$/gm)];
    if (keys.length !== 1 || keys[0][1] !== 'type') return `${file}: frontmatter must hold exactly one key, type`;
    const type = keys[0][2] as ChangeType;
    if (!TYPES.includes(type)) return `${file}: type "${type}" is not one of ${TYPES.join('|')}`;
    const text = m[2].trim();
    if (!text) return `${file}: no text`;
    if (text.includes('\n')) return `${file}: the text must be one line`;
    if (/^[-*]\s/.test(text)) return `${file}: no leading "- ", the release adds it`;
    return { file, type, text };
}

export function readFragments(): { fragments: Fragment[]; errors: string[] } {
    const fragments: Fragment[] = [];
    const errors: string[] = [];
    if (!existsSync(CHANGES_DIR)) return { fragments, errors };
    for (const f of readdirSync(CHANGES_DIR).sort()) {
        if (f === 'README.md' || f.startsWith('.')) continue;
        if (!SLUG_RE.test(f)) {
            errors.push(`.changes/${f}: name must be lowercase-slug.md`);
            continue;
        }
        const r = parseFragment(`.changes/${f}`, readFileSync(join(CHANGES_DIR, f), 'utf8'));
        if (typeof r === 'string') errors.push(r);
        else fragments.push(r);
    }
    return { fragments, errors };
}

export function parseChangelog(src: string): Release[] {
    const out: Release[] = [];
    for (const part of src.split(/^## /m).slice(1)) {
        const [head, ...rest] = part.split('\n');
        out.push({ version: head.trim(), lines: rest.filter(l => l.startsWith('- ')).map(l => l.slice(2)) });
    }
    return out;
}

export function sortedTexts(fragments: Fragment[]): string[] {
    return TYPES.flatMap(t => fragments.filter(f => f.type === t).map(f => f.text));
}

function arg(name: string): string | undefined {
    const i = process.argv.indexOf(`--${name}`);
    return i > 0 ? process.argv[i + 1] : undefined;
}

async function check(): Promise<number> {
    const { fragments, errors } = readFragments();
    if (existsSync(CHANGELOG)) {
        const log = parseChangelog(await Bun.file(CHANGELOG).text());
        const seen = new Set<string>();
        for (const r of log) {
            if (!/^\d+\.\d+(\.\d+)*$/.test(r.version)) errors.push(`CHANGELOG.md: bad version "${r.version}"`);
            if (seen.has(r.version)) errors.push(`CHANGELOG.md: ${r.version} twice`);
            seen.add(r.version);
        }
    } else errors.push('CHANGELOG.md missing');
    for (const e of errors) console.error(e);
    if (!errors.length) console.log(`changes ok (${fragments.length} pending fragment${fragments.length === 1 ? '' : 's'})`);
    return errors.length ? 1 : 0;
}

async function release(version: string | undefined): Promise<number> {
    if (!version) {
        console.error('usage: changes.ts release --version X.Y.Z (scheme: plugin/tools/version.ts)');
        return 2;
    }
    const { fragments, errors } = readFragments();
    if (errors.length) {
        for (const e of errors) console.error(e);
        return 1;
    }
    if (!fragments.length) {
        console.error('no fragments in .changes/, nothing to release');
        return 1;
    }
    const src = await Bun.file(CHANGELOG).text();
    const log = parseChangelog(src);
    if (log.some(r => r.version === version)) {
        console.error(`CHANGELOG.md already has ${version}`);
        return 1;
    }
    const problem = releaseProblem(log[0]?.version, version, fragments.map(f => f.type));
    if (problem) {
        console.error(problem);
        return 1;
    }
    const section = `## ${version}\n\n${sortedTexts(fragments)
        .map(t => `- ${t}`)
        .join('\n')}\n\n`;
    const i = src.search(/^## /m);
    const next = i < 0 ? `${src.trimEnd()}\n\n${section}` : src.slice(0, i) + section + src.slice(i);
    await Bun.write(CHANGELOG, next);
    for (const f of fragments) unlinkSync(join(ROOT, f.file));
    console.log(`CHANGELOG.md: ${version} with ${fragments.length} entries; fragments removed`);
    return 0;
}

export { HEADER };

if (import.meta.main) {
    const cmd = process.argv[2];
    if (cmd === 'check') process.exit(await check());
    else if (cmd === 'release') process.exit(await release(arg('version')));
    else {
        console.error('usage: changes.ts check | release --version X.Y.Z');
        process.exit(2);
    }
}
