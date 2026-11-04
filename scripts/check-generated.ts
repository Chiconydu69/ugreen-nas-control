// Guard against generated files in git (`just generated-check`, part of `just check` and CI).
// Rule: generators write only to gitignored paths (scripts/generated.ts). The only tracked generated files are the
// ones listed with `linguist-generated=true` in .gitattributes, each with a reason comment.
// Fails when
//   (a) a tracked file starts with a "generated" banner and is not in the .gitattributes list,
//   (b) a tracked file matches .gitignore (it was force-added, or the ignore rule came later),
//   (c) a path a generator writes is tracked and not in the .gitattributes list,
//   (d) the web build (web/tools/build.ts) creates or changes anything outside .build/ (a source dir, a stray ignored file),
//   (e) a tracked file sits in a build-output folder (dist/, build/, out/, .build/, node_modules/), or is an image outside
//       the source-asset folders.
import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { $ } from 'bun';
import { ALL_OUTPUTS, OUTPUT_DIRS } from './generated';

const root = resolve(import.meta.dir, '..');
const lines = (s: string) => s.split('\n').filter(Boolean);

const tracked = lines(await $`git -C ${root} ls-files`.text());
const trackedSet = new Set(tracked);

const attributes = readFileSync(join(root, '.gitattributes'), 'utf8')
    .split('\n')
    .map(l => l.trim())
    .filter(l => l && !l.startsWith('#'));
const allowed = new Set(attributes.filter(l => /\slinguist-generated(=true)?(\s|$)/.test(l)).map(l => l.split(/\s+/)[0] as string));

const HOWTO = `Generated files are not committed: add the path to .gitignore and \`git rm --cached\` it. If it must stay tracked
(GitHub renders it, or installed systems read it from main), list it in .gitattributes as
"<path> linguist-generated=true" with a comment saying why. Paths a generator writes are declared in scripts/generated.ts.`;

const problems: string[] = [];

// (a) banner in the first lines of a tracked text file: a comment line that starts with "Generated" / "@generated", or says
// "do not edit". Markdown prose is skipped, and so are templates (.tmpl: they carry the banner of the file they produce).
const BANNER = /^\s*(?:\/\/|#|\/\*+|\*|<!--|;)?\s*(?:@generated|auto-?generated|generated\b)|\bdo not edit\b/im;
for (const file of tracked) {
    if (
        allowed.has(file) ||
        file.endsWith('.tmpl') ||
        file.endsWith('.md') ||
        ['scripts/check-generated.ts', 'scripts/generated.ts', '.gitattributes'].includes(file) ||
        /\.(png|jpe?g|gif|ico|woff2?|lock)$/.test(file)
    )
        continue;
    let head: string;
    try {
        head = readFileSync(join(root, file), 'utf8').split('\n', 6).join('\n');
    } catch {
        continue;
    }
    if (BANNER.test(head)) problems.push(`${file}: starts with a "generated" banner but is tracked and not in .gitattributes`);
}

for (const file of lines(await $`git -C ${root} ls-files -ci --exclude-standard`.text())) {
    problems.push(`${file}: tracked although .gitignore ignores it`);
}

for (const out of ALL_OUTPUTS) {
    const hits = out.endsWith('/') ? tracked.filter(f => f.startsWith(out)) : trackedSet.has(out) ? [out] : [];
    for (const f of hits) if (!allowed.has(f)) problems.push(`${f}: written by a generator (scripts/generated.ts) but tracked and not in .gitattributes`);
}
// every declared tracked output must be in the allowlist, and every allowlisted path must exist
for (const f of allowed) if (!trackedSet.has(f)) problems.push(`.gitattributes: ${f} is listed as generated but is not tracked`);

// (e) structure: nothing tracked inside a build-output folder, and images only in source-asset folders. Catches stale
// output that a moved or deleted .gitignore lets through (b) and that no generator declares any more (c).
const OUTPUT_SEGMENT = /(^|\/)(dist|build|out|\.build|node_modules)\//;
const IMAGE_SOURCES = ['web/assets/', 'web/plugin/led/images/'];
const TRACKED_IMAGES = ['docs/ca/']; // Community Applications screenshots (`just ca-shots`), listed in .gitattributes
for (const f of tracked) {
    if (OUTPUT_SEGMENT.test(f)) problems.push(`${f}: tracked inside a build-output folder`);
    else if (/\.(png|jpe?g|gif|webp|ico|svg)$/i.test(f) && ![...IMAGE_SOURCES, ...TRACKED_IMAGES].some(d => f.startsWith(d)))
        problems.push(`${f}: image outside the source-asset folders (${IMAGE_SOURCES.join(', ')}); rendered images belong in .build/`);
}

// (d) run the web build and compare `git status --ignored` before and after: only .build/ may change
const status = async () => lines(await $`git -C ${root} status --porcelain --ignored --untracked-files=all`.text());
const before = new Set(await status());
await $`bun ${join(root, 'web/tools/build.ts')}`.quiet();
for (const line of await status()) {
    const path = line.slice(3).replace(/^"|"$/g, '');
    if (!before.has(line) && !OUTPUT_DIRS.some(d => path.startsWith(d))) problems.push(`${path}: written by the web build outside ${OUTPUT_DIRS.join(', ')}`);
}

if (problems.length) {
    for (const p of problems) console.error(p);
    console.error(`\n${HOWTO}`);
    process.exit(1);
}
console.log(`generated files ok: ${allowed.size} tracked on purpose, ${ALL_OUTPUTS.length} output paths declared`);
