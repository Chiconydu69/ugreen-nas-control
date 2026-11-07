// Fails when prose types a fact that docs/site/facts.ts derives: a model or language count, the minimum Unraid version
// or a plugin version. Generated blocks (<!-- name:start --> ... <!-- name:end -->) are skipped; what stays is listed in
// docs/tools/facts-allow.txt (`file | matched text | reason`, one per line).
export interface Source {
    file: string;
    text: string;
}

interface Rule {
    re: RegExp;
    fact: string;
    /** Files the rule applies to; default every linted file. */
    only?: string[];
}

const RULES: Rule[] = [
    { re: /\b\d+ (?:models?|languages)\b/g, fact: 'count from the registry (count() in docs/site/facts.ts) or LANGS (LANG_COUNT / LANGUAGES)' },
    { re: /\bUnraid \d+\.\d+/g, fact: 'minimum Unraid version from the .plg template (UNRAID_MIN, UNRAID_PLUS, UNRAID_OR_NEWER in docs/site/facts.ts)' },
    {
        re: /\b\d+\.\d+\.\d+\b/g,
        fact: 'plugin version: link the release instead of typing it (docs/releasing.md#versions)',
        only: ['README.md', 'docs/site/content.ts', 'docs/site/build.ts']
    }
];

/** Blank out generated blocks, keeping every newline so line numbers stay right. */
export function withoutGenerated(text: string): string {
    return text.replace(/<!-- ([\w:-]+):start -->[\s\S]*?<!-- \1:end -->/g, block => block.replace(/[^\n]/g, ' '));
}

export function parseAllow(text: string): { file: string; match: string; line: number }[] {
    return text
        .split('\n')
        .map((raw, i) => ({ raw, line: i + 1 }))
        .filter(({ raw }) => raw.trim() && !raw.startsWith('#'))
        .map(({ raw, line }) => {
            const [file = '', match = ''] = raw.split('|').map(s => s.trim());
            return { file, match, line };
        });
}

/** One message per problem: `file:line: "text" is a derived fact: <where it comes from>`. */
export function lintFacts(sources: Source[], allowText: string): string[] {
    const allow = parseAllow(allowText);
    const used = new Set<number>();
    const out: string[] = [];
    for (const { file, text } of sources) {
        const lines = withoutGenerated(text).split('\n');
        lines.forEach((line, i) => {
            for (const rule of RULES) {
                if (rule.only && !rule.only.includes(file)) continue;
                for (const m of line.matchAll(rule.re)) {
                    const hit = allow.findIndex(a => a.file === file && a.match === m[0]);
                    if (hit >= 0) used.add(hit);
                    else out.push(`${file}:${i + 1}: "${m[0]}" is a derived fact: ${rule.fact}`);
                }
            }
        });
    }
    allow.forEach((a, i) => {
        if (!used.has(i)) out.push(`docs/tools/facts-allow.txt:${a.line}: unused entry "${a.file} | ${a.match}" (remove it)`);
    });
    return out;
}
