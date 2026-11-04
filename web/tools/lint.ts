// Patterns the compiler lets through but this code base does not want (bun has no linter of its own).
// innerHTML / insertAdjacentHTML / outerHTML are only allowed in the files that still render HTML strings;
// the rewrite to escaping templates removes them from this list.
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';

const root = resolve(import.meta.dir, '../..');
const HTML_ALLOWED = ['web/src/features/nas/app.ts', 'web/src/features/led/app.ts', 'web/src/dash/dash.ts', 'web/src/features/nas/fan/'];

const RULES: { name: string; pattern: RegExp; allowIn?: string[] }[] = [
    { name: 'any type', pattern: /(?::|\bas|<)\s*any\b(?!\w)/ },
    { name: 'ts-ignore / ts-nocheck', pattern: /@ts-(?:ignore|nocheck)/ },
    { name: 'eval / new Function', pattern: /\beval\(|\bnew Function\(/ },
    { name: 'document.write', pattern: /document\.write\(/ },
    { name: 'raw HTML sink', pattern: /\.(?:innerHTML|outerHTML)\s*(?:\+)?=(?!=)|insertAdjacentHTML\(/, allowIn: HTML_ALLOWED }
];

function walk(dir: string): string[] {
    return readdirSync(dir).flatMap(name => {
        const path = join(dir, name);
        return statSync(path).isDirectory() ? walk(path) : path.endsWith('.ts') && !path.endsWith('.d.ts') ? [path] : [];
    });
}

let found = 0;

for (const file of [...walk(join(root, 'web/src')), ...['build', 'check-i18n', 'lint'].map(name => join(root, `web/tools/${name}.ts`))]) {
    const rel = relative(root, file);

    readFileSync(file, 'utf8')
        .split('\n')
        .forEach((line, i) => {
            if (line.trimStart().startsWith('//') || line.trimStart().startsWith('*')) return;
            for (const rule of RULES) {
                if (rule.allowIn?.some(p => rel === p || (p.endsWith('/') && rel.startsWith(p))) || !rule.pattern.test(line)) continue;
                console.error(`${rel}:${i + 1}: ${rule.name}: ${line.trim()}`);
                found++;
            }
        });
}

if (found) process.exit(1);
console.log('lint ok');
