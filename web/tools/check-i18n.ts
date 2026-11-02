// i18n checks beyond the compiler (missing and extra keys in other languages are compile errors already):
//  - all languages have the same keys and the same {placeholders}; no empty texts
//  - every key named in the page markup (data-i18n, data-i18n-html, data-unc-*, data-label-key) exists
//  - module parts (features/nas/i18n/parts/<name>.<lang>.ts, spread into the language tables) list the same keys in
//    every language and none that en.ts already has
//  - keys nothing refers to are errors (keys built at run time need a `prefix.${x}` template in the source)
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { de as ledDe } from '../src/features/led/i18n/de';
import { en as ledEn } from '../src/features/led/i18n/en';
import { es as ledEs } from '../src/features/led/i18n/es';
import { fr as ledFr } from '../src/features/led/i18n/fr';
import { it as ledIt } from '../src/features/led/i18n/it';
import { nl as ledNl } from '../src/features/led/i18n/nl';
import { zh as ledZh } from '../src/features/led/i18n/zh';
import { de as nasDe } from '../src/features/nas/i18n/de';
import { en as nasEn } from '../src/features/nas/i18n/en';
import { es as nasEs } from '../src/features/nas/i18n/es';
import { fr as nasFr } from '../src/features/nas/i18n/fr';
import { it as nasIt } from '../src/features/nas/i18n/it';
import { nl as nasNl } from '../src/features/nas/i18n/nl';
import { zh as nasZh } from '../src/features/nas/i18n/zh';
import { placeholderMismatches } from '../src/core/tables';
import { inputsEn } from '../src/features/nas/i18n/parts/inputs.en';
import { historyEn } from '../src/features/nas/i18n/parts/history.en';
import { scheduleEn } from '../src/features/nas/i18n/parts/schedule.en';
import { toolsEn } from '../src/features/nas/i18n/parts/tools.en';

const escapeRe = (s: string) => s.replace(/[\\^$.*+?()[\]{}|]/g, '\\$&');

const root = resolve(import.meta.dir, '../..');
const errors: string[] = [];
const warnings: string[] = [];

function walk(dir: string, accept: (file: string) => boolean): string[] {
    return readdirSync(dir).flatMap(name => {
        const path = join(dir, name);
        return statSync(path).isDirectory() ? walk(path, accept) : accept(path) ? [path] : [];
    });
}

interface Set {
    name: string;
    en: Record<string, string>;
    others: Record<string, Record<string, string>>;
    markup: string[];
    sources: string[];
}

const sets: Set[] = [
    {
        name: 'NAS',
        en: nasEn,
        others: { de: nasDe, es: nasEs, fr: nasFr, it: nasIt, nl: nasNl, zh: nasZh },
        markup: walk(join(root, 'web/plugin'), f => f.endsWith('.page')),
        sources: walk(join(root, 'web/src'), f => f.endsWith('.ts') && !f.includes('/i18n/') && !f.includes('/led/') && !f.endsWith('.test.ts'))
    },
    {
        name: 'LED',
        en: ledEn,
        others: { de: ledDe, es: ledEs, fr: ledFr, it: ledIt, nl: ledNl, zh: ledZh },
        markup: walk(join(root, 'web/plugin/led'), f => f.endsWith('.php')),
        sources: walk(
            join(root, 'web/src'),
            f => f.endsWith('.ts') && !f.includes('/i18n/') && !f.includes('/nas/') && !f.includes('/dash/') && !f.endsWith('.test.ts')
        )
    }
];

// Module parts: one file per language next to the en part. A key also written in en.ts would silently win or lose in the spread.
const PARTS: Record<string, Record<string, string>> = { history: historyEn, inputs: inputsEn, schedule: scheduleEn, tools: toolsEn };
const partDir = join(root, 'web/src/features/nas/i18n/parts');
const baseKeys = new Set([...readFileSync(join(root, 'web/src/features/nas/i18n/en.ts'), 'utf8').matchAll(/^ {4}'([\w.-]+)':/gm)].map(m => m[1] as string));
const partOwner = new Map<string, string>();
for (const [name, en] of Object.entries(PARTS)) {
    for (const lang of ['en', 'de', 'es', 'fr', 'it', 'nl', 'zh']) {
        if (!walk(partDir, f => f.endsWith(`/${name}.${lang}.ts`)).length) errors.push(`NAS part ${name}: no file for ${lang}`);
    }
    for (const key of Object.keys(en)) {
        if (baseKeys.has(key)) errors.push(`NAS part ${name}: key "${key}" is also in en.ts`);
        const other = partOwner.get(key);
        if (other) errors.push(`NAS part ${name}: key "${key}" is also in part ${other}`);
        partOwner.set(key, name);
    }
}

const KEY_ATTRS = /\b(?:data-i18n|data-i18n-html|data-unc-(?:label|title|hint)|data-label-key)="([^"]+)"/g;

for (const set of sets) {
    const keys = Object.keys(set.en);

    for (const [lang, table] of Object.entries(set.others)) {
        const missing = keys.filter(k => !(k in table));
        const extra = Object.keys(table).filter(k => !(k in set.en));
        if (missing.length) errors.push(`${set.name} ${lang}: missing ${missing.join(', ')}`);
        if (extra.length) errors.push(`${set.name} ${lang}: extra ${extra.join(', ')}`);
        const bad = placeholderMismatches(set.en, table);
        if (bad.length) errors.push(`${set.name} ${lang}: empty or different {placeholders}: ${bad.join(', ')}`);
    }

    const used = new Set<string>();

    for (const file of set.markup) {
        for (const match of readFileSync(file, 'utf8').matchAll(KEY_ATTRS)) {
            const key = match[1] as string;
            used.add(key);
            // keys built by PHP (<?= ... ?>) cannot be checked
            if (!key.includes('<?') && !(key in set.en)) errors.push(`${relative(root, file)}: unknown i18n key "${key}"`);
        }
    }

    // Keys in the sources: quoted words, and `prefix.${x}suffix` templates as a pattern (the prefix must hold a dot).
    const patterns: RegExp[] = [];
    for (const file of set.sources) {
        const text = readFileSync(file, 'utf8');
        for (const match of text.matchAll(/(['"])([\w.-]+)\1/g)) {
            if ((match[2] as string) in set.en) used.add(match[2] as string);
        }
        for (const match of text.matchAll(/`([\w.-]*)\$\{[^}]*\}([\w.-]*)`/g)) {
            const [, prefix = '', suffix = ''] = match;
            if (prefix.includes('.')) patterns.push(new RegExp(`^${escapeRe(prefix)}.+${escapeRe(suffix)}$`));
        }
    }

    const unused = keys.filter(k => !used.has(k) && !patterns.some(p => p.test(k)));
    if (unused.length) errors.push(`${set.name}: ${unused.length} keys nothing refers to: ${unused.join(', ')}`);
    console.log(
        `${set.name}: ${keys.length} keys x ${Object.keys(set.others).length + 1} languages, ${set.markup.length} markup files, ${set.sources.length} sources`
    );
}

for (const warning of warnings) console.warn(`warning: ${warning}`);
for (const error of errors) console.error(`error: ${error}`);
if (errors.length) process.exit(1);
console.log('i18n ok');
