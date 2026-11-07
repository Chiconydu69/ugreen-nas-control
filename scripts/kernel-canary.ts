// Weekly check of the newest Unraid kernel config against what the plugin relies on (docs/decisions.md "No kernel module
// of our own"; rules in scripts/kernel-canary-rules.ts). `just kernel-canary [version] [--issue]`.
//
// Source: ich777/unraid_kernel publishes every Unraid kernel as release `<ver>-Unraid`. The archive is untrusted data: it is
// streamed (curl | xz | tar), only the `./.config` member is extracted, into a fresh temp dir, and nothing from it runs.
//
// Exit codes: 0 all must-hold rules ok (informational changes are printed and reported), 1 a must-hold rule is broken,
// 2 the canary cannot check (download, config member, unknown option).
// --issue opens or updates one issue per kernel version (title prefix "Unraid kernel <ver>:"), never a second one.
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { $ } from 'bun';
import { RULES, type Rule, type State } from './kernel-canary-rules';

const KERNEL_REPO = 'ich777/unraid_kernel';
const CONFIG_MEMBER = './.config';
const VERSION_RE = /^\d+\.\d+\.\d+$/;

export interface Row {
    rule: Rule;
    expected: string;
    found: string;
    status: 'ok' | 'FAIL' | 'CHANGED';
}

// `CONFIG_X=y` and `# CONFIG_X is not set`; values keep their quotes.
export function parseConfig(text: string): Map<string, string> {
    const cfg = new Map<string, string>();
    for (const line of text.split('\n')) {
        const set = /^(CONFIG_[A-Za-z0-9_]+)=(.*)$/.exec(line);
        if (set) cfg.set(set[1] ?? '', set[2] ?? '');
        else {
            const off = /^# (CONFIG_[A-Za-z0-9_]+) is not set$/.exec(line);
            if (off) cfg.set(off[1] ?? '', 'n');
        }
    }
    return cfg;
}

// An option missing from the file is "unset": Kconfig leaves out options whose dependencies are off. CONFIG_LSM is a
// list: it counts as set when it names lockdown.
export function stateOf(cfg: Map<string, string>, option: string): State {
    const v = cfg.get(option);
    if (v === undefined || v === 'n') return 'unset';
    if (option === 'CONFIG_LSM') return /(^|[",])lockdown([",]|$)/.test(v) ? 'y' : 'unset';
    if (v === 'y' || v === 'm') return v;
    throw new Error(`${option} has the value ${v}, expected y, m or not set`);
}

const label = (s: State) => (s === 'unset' ? 'not set' : s);

export function evaluate(cfg: Map<string, string>, rules: readonly Rule[] = RULES): Row[] {
    return rules.map(rule => {
        const state = stateOf(cfg, rule.option);
        const ok = rule.allowed.includes(state);
        const lsm = rule.option === 'CONFIG_LSM';
        return {
            rule,
            expected: lsm ? 'no lockdown' : rule.allowed.map(label).join(' or '),
            found: lsm && cfg.has(rule.option) ? cfg.get(rule.option)!.replace(/"/g, '') || '""' : label(state),
            status: ok ? 'ok' : rule.severity === 'must' ? 'FAIL' : 'CHANGED'
        };
    });
}

export const broken = (rows: Row[]) => rows.filter(r => r.status === 'FAIL');
export const changed = (rows: Row[]) => rows.filter(r => r.status === 'CHANGED');

export function table(rows: Row[]): string {
    const cells = [['option', 'expected', 'found', 'status'], ...rows.map(r => [r.rule.option, r.expected, r.found, r.status])];
    const widths = [0, 1, 2, 3].map(i => Math.max(...cells.map(c => (c[i] ?? '').length)));
    return cells
        .map(c =>
            c
                .map((x, i) => x.padEnd(widths[i] ?? 0))
                .join('  ')
                .trimEnd()
        )
        .join('\n');
}

// One issue per kernel version. The title lists what is wrong, deduplicated and in rule order.
export function issueTitle(version: string, rows: Row[]): string {
    const bad = broken(rows);
    const shown = bad.length ? bad : changed(rows);
    return `Unraid kernel ${version}: ${[...new Set(shown.map(r => r.rule.short))].join(', ')}`;
}

export const titlePrefix = (version: string) => `Unraid kernel ${version}:`;

export function issueBody(version: string, rows: Row[]): string {
    const bad = broken(rows);
    const lines = [
        bad.length
            ? `The Unraid kernel ${version} no longer provides what the plugin relies on. ${bad.map(r => r.rule.breaks).join('; ')} may stop working after the update.`
            : `The Unraid kernel ${version} changed an option the plugin watches. Nothing is broken; this may open an option.`,
        '',
        '| Option | Expected | Found | Status | Why we care | What breaks |',
        '| --- | --- | --- | --- | --- | --- |',
        ...rows
            .filter(r => r.status !== 'ok')
            .map(r => `| \`${r.rule.option}\` | ${r.expected} | ${r.found} | ${r.status} | ${r.rule.why} | ${r.rule.breaks} |`),
        '',
        `Source: release \`${version}-Unraid\` of ${KERNEL_REPO}, member \`${CONFIG_MEMBER}\`. Rules: \`scripts/kernel-canary-rules.ts\`, see docs/decisions.md.`
    ];
    return lines.join('\n');
}

// ---- network and process side ----

const ghHeaders = () => ({
    accept: 'application/vnd.github+json',
    ...(process.env.GITHUB_TOKEN ? { authorization: `Bearer ${process.env.GITHUB_TOKEN}` } : {})
});

async function newestVersion(): Promise<string> {
    const res = await fetch(`https://api.github.com/repos/${KERNEL_REPO}/releases?per_page=10`, { headers: ghHeaders() });
    if (!res.ok) throw new Error(`release list: HTTP ${res.status}`);
    const releases = (await res.json()) as { tag_name: string; draft: boolean; prerelease: boolean }[];
    for (const r of releases) {
        const m = /^(\d+\.\d+\.\d+)-Unraid$/.exec(r.tag_name);
        if (m && !r.draft && !r.prerelease) return m[1] ?? '';
    }
    throw new Error('no <version>-Unraid release found');
}

async function fetchConfig(version: string): Promise<string> {
    const dir = await mkdtemp(join(tmpdir(), 'kernel-canary-'));
    try {
        const url = `https://github.com/${KERNEL_REPO}/releases/download/${version}-Unraid/linux-${version}-Unraid.tar.xz`;
        // The archive is about 300 MB; tar must read it to the end.
        const run = await $`curl -fsSL "$URL" | xz -dc | tar -x -f - -C "$DIR" "$MEMBER"`
            .env({ ...process.env, URL: url, DIR: dir, MEMBER: CONFIG_MEMBER, PATH: process.env.PATH ?? '' })
            .nothrow()
            .quiet();
        if (run.exitCode !== 0) throw new Error(`extracting ${CONFIG_MEMBER} from ${url}: ${run.stderr.toString().trim() || `exit ${run.exitCode}`}`);
        const text = await readFile(join(dir, '.config'), 'utf8');
        const header = /^# Linux\/\S+ (\d+\.\d+\.\d+)\S* Kernel Configuration$/m.exec(text);
        if (header?.[1] !== version) throw new Error(`${CONFIG_MEMBER} is for kernel ${header?.[1] ?? 'unknown'}, expected ${version}`);
        return text;
    } finally {
        await rm(dir, { recursive: true, force: true });
    }
}

// A renamed or removed option must fail the canary, not pass as "not set".
async function verifyNames(version: string): Promise<void> {
    const tag = `v${version.split('.').slice(0, 2).join('.')}`;
    const files = [...new Set(RULES.map(r => r.src))];
    const text = new Map<string, string>();
    await Promise.all(
        files.map(async f => {
            const res = await fetch(`https://raw.githubusercontent.com/torvalds/linux/${tag}/${f}`);
            if (!res.ok) throw new Error(`Kconfig ${f} at ${tag}: HTTP ${res.status}`);
            text.set(f, await res.text());
        })
    );
    const missing = RULES.filter(r => !new RegExp(`^\\s*(menu)?config ${r.option.slice('CONFIG_'.length)}$`, 'm').test(text.get(r.src) ?? ''));
    if (missing.length) throw new Error(`not in the ${tag} Kconfig (renamed or removed?): ${missing.map(r => `${r.option} (${r.src})`).join(', ')}`);
}

async function ghJson<T>(...args: string[]): Promise<T> {
    return JSON.parse(await $`gh ${args}`.text()) as T;
}

async function reportIssue(version: string, rows: Row[]): Promise<void> {
    const prefix = titlePrefix(version);
    const issues = await ghJson<{ number: number; title: string; state: string }[]>(
        'issue',
        'list',
        '--state',
        'all',
        '--limit',
        '500',
        '--json',
        'number,title,state'
    );
    const existing = issues.find(i => i.title.startsWith(prefix));
    if (existing) {
        // A closed issue stays closed: the maintainers decided. An open one gets the current table.
        if (existing.state === 'OPEN') await $`gh issue edit ${existing.number} --body ${issueBody(version, rows)}`.quiet();
        console.log(`issue #${existing.number} (${existing.state.toLowerCase()}) already covers ${version}`);
        return;
    }
    const label = broken(rows).length ? 'bug' : 'enhancement';
    const url = (await $`gh issue create --title ${issueTitle(version, rows)} --body ${issueBody(version, rows)} --label ${label}`.text()).trim();
    console.log(`opened ${url}`);
}

async function main(): Promise<number> {
    const args = process.argv.slice(2);
    const issue = args.includes('--issue');
    const explicit = args.find(a => !a.startsWith('--'));
    if (explicit !== undefined && !VERSION_RE.test(explicit)) throw new Error(`version must look like 6.18.60, got ${explicit}`);
    const version = explicit ?? (await newestVersion());
    console.log(`Unraid kernel ${version}`);
    await verifyNames(version);
    const rows = evaluate(parseConfig(await fetchConfig(version)));
    console.log(table(rows));
    if (issue && (broken(rows).length || changed(rows).length)) await reportIssue(version, rows);
    return broken(rows).length ? 1 : 0;
}

if (import.meta.main) {
    try {
        process.exit(await main());
    } catch (e) {
        console.error(`kernel-canary: cannot check: ${(e as Error).message}`);
        process.exit(2);
    }
}
