// plugin/files.txt: every file the plugin installs. Shared by plg.ts (archive) and web/tools/build.ts (install tree).
//   <source> <dest> <644|755>     a file
//   release:<tool> <dest> -> <target>   a symbolic link (target relative to the link's folder); `release:` makes the
//                                       harness and the install tree builder skip the line, like a binary
import { join } from 'node:path';

export interface Entry {
    source: string;
    dest: string;
    mode: string;
}

export interface Link {
    dest: string;
    target: string;
}

export const MANIFEST = join(import.meta.dir, '../files.txt');

async function lines(): Promise<{ n: number; fields: string[] }[]> {
    return (await Bun.file(MANIFEST).text())
        .split('\n')
        .map((raw, n) => ({ n: n + 1, line: raw.trim() }))
        .filter(l => l.line && !l.line.startsWith('#'))
        .map(l => ({ n: l.n, fields: l.line.split(/\s+/) }));
}

/** The regular files. Symlinks are in readLinks(), so the install tree builder never sees them. */
export async function readManifest(): Promise<Entry[]> {
    const out: Entry[] = [];
    for (const { n, fields } of await lines()) {
        if (fields[2] === '->') continue;
        const [source, dest, mode, ...extra] = fields;
        if (!source || !dest || !mode || !/^(644|755)$/.test(mode) || extra.length) throw new Error(`files.txt:${n}: want "<source> <dest> <644|755>"`);
        out.push({ source, dest, mode });
    }
    const dup = out.find((e, i) => out.findIndex(o => o.dest === e.dest) !== i);
    if (dup) throw new Error(`files.txt: ${dup.dest} twice`);
    return out;
}

export async function readLinks(): Promise<Link[]> {
    const out: Link[] = [];
    const files = new Set((await readManifest()).map(e => e.dest));
    for (const { n, fields } of await lines()) {
        if (fields[2] !== '->') continue;
        const [source, dest, , target, ...extra] = fields;
        if (!source?.startsWith('release:') || !dest || !target || extra.length || !/^(\.\.\/)*[A-Za-z0-9._-]+(\/[A-Za-z0-9._-]+)*$/.test(target))
            throw new Error(`files.txt:${n}: want "release:<tool> <dest> -> <relative target>"`);
        if (files.has(dest) || out.some(l => l.dest === dest)) throw new Error(`files.txt: ${dest} twice`);
        out.push({ dest, target });
    }
    return out;
}

/** Install-relative path of a link's target. */
export function linkTarget(l: Link): string {
    const parts = l.dest.split('/').slice(0, -1);
    for (const p of l.target.split('/')) {
        if (p === '..') parts.pop();
        else if (p !== '.') parts.push(p);
    }
    return parts.join('/');
}
