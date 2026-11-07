// Keeps the credits complete: every GitHub login credited anywhere must be in docs/tools/testers.ts (README block and website
// credits are rendered from it), and no bare @login may notify people from fragments, README or site copy.
export type Source = { file: string; text: string };

const IGNORED_BY = /^(users?|maintainers?)\b/i;

/** Logins named in the `by:` fields of web/models/*.ts ("a, b", "a and users", "a (issue 1)"). */
function modelLogins(text: string): { login: string; line: number }[] {
    const out: { login: string; line: number }[] = [];
    text.split('\n').forEach((l, i) => {
        const m = l.match(/\bby:\s*'([^']*)'/);
        if (!m) return;
        const parts = (m[1] ?? '')
            .replace(/\([^)]*\)/g, '')
            .split(/,|\band\b/)
            .map(p => p.trim())
            .filter(p => p && !IGNORED_BY.test(p) && !/\s/.test(p));
        for (const login of parts) out.push({ login, line: i + 1 });
    });
    return out;
}

/** `[x](https://github.com/x)` in the text after "Thanks to" or "Reported by". */
function creditLinks(text: string): { login: string; line: number }[] {
    const out: { login: string; line: number }[] = [];
    text.split('\n').forEach((l, i) => {
        const at = l.search(/Thanks to|Reported by/);
        if (at < 0) return;
        for (const m of l.slice(at).matchAll(/\[([A-Za-z0-9-]+)\]\(https:\/\/github\.com\/([A-Za-z0-9-]+)\/?\)/g)) out.push({ login: m[2] ?? '', line: i + 1 });
    });
    return out;
}

/** A bare `@login` (after whitespace, an opening bracket or a quote). Code spans, fences and e-mail addresses are fine. */
function bareMentions(file: string, text: string): { mention: string; line: number }[] {
    const out: { mention: string; line: number }[] = [];
    const md = file.endsWith('.md');
    let fence = false;
    text.split('\n').forEach((raw, i) => {
        if (md && /^\s*```/.test(raw)) fence = !fence;
        if (fence) return;
        const l = md ? raw.replace(/`[^`]*`/g, '') : raw;
        for (const m of l.matchAll(/(?:^|[\s(\[>])(@[A-Za-z][A-Za-z0-9-]*)/g)) out.push({ mention: m[1] ?? '', line: i + 1 });
    });
    return out;
}

/**
 * models: web/models/*.ts, credits: .changes/*.md + CHANGELOG.md, prose: .changes/*.md, README.md, docs/site/*.ts.
 * known: logins in TESTERS and MAINTAINERS.
 */
export function lintCredits(models: Source[], credits: Source[], prose: Source[], known: string[]): string[] {
    const have = new Set(known.map(k => k.toLowerCase()));
    const errors: string[] = [];
    for (const { file, text } of models)
        for (const { login, line } of modelLogins(text))
            if (!have.has(login.toLowerCase()))
                errors.push(
                    `${file}:${line}: "${login}" is credited in a model report but missing from TESTERS. Fix: add { login: '${login}', did: '…' } to docs/tools/testers.ts.`
                );
    for (const { file, text } of credits)
        for (const { login, line } of creditLinks(text))
            if (!have.has(login.toLowerCase()))
                errors.push(
                    `${file}:${line}: "${login}" is credited here but missing from TESTERS. Fix: add { login: '${login}', did: '…' } to docs/tools/testers.ts.`
                );
    for (const { file, text } of prose)
        for (const { mention, line } of bareMentions(file, text))
            errors.push(
                `${file}:${line}: bare ${mention} notifies that person. Fix: write [${mention.slice(1)}](https://github.com/${mention.slice(1)}) or put it in backticks.`
            );
    return errors;
}
