// End-to-end suite: replays the user flows of scenarios.ts in a real browser (Bun.WebView) against the plugin's
// front end served by the local harness, and checks what the page does: API requests, console errors, toasts,
// what is on screen.
//
//   bun web/tools/e2e/run.ts [--models a,b] [--scenarios x,y] [--old [ref]] [--update] [--jobs N] [--out DIR] [-v]
//
// Without --old the run is checked against web/tools/e2e/golden.json (requests, events and a hash of the screen text
// per step). With --old the same flows also run against the hand-written front end of a git ref (default
// upstream/main = the released 4.x UI, or $UNC_E2E_OLD_REF; the ref must still have src/web/js, i.e. be older than
// the TypeScript front end), and the two are compared step by step, including a diff of the full DOM. --update writes
// golden.json from this run (from the old front end when --old is given). Exit code 1 on any regression.
//
// Browser: Bun.WebView; macOS uses WebKit, Linux Chromium (BUN_CHROME_PATH from the flake devShell; else chromium on PATH or nixpkgs#chromium).
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { $ } from 'bun';
import { fixtureHasChip } from '../../models/fixture';
import { loadModels } from '../../models/index';
import { format } from '../../../scripts/format';
import { type ApiRequest, startHarness } from '../harness/serve';
import { Session, type StepRecord, VIEWPORT } from './driver';
import { E2E_NOW, pageInit } from './page-init';
import { type Ctx, SCENARIOS } from './scenarios';

process.env.TZ = 'Europe/Berlin';
const root = resolve(import.meta.dir, '../../..');
const GOLDEN = join(import.meta.dir, 'golden.json');
const DEFAULT_MODELS = ['dxp4800-gt', 'dxp6800-pro', 'dxp4800', 'idx6011'];

/** Steps are matched by "<scenario> / <step>"; `what` is the canonical line (request or event) that may differ. */
export const KNOWN: { match: RegExp; reason: string }[] = [];

const args = process.argv.slice(2);
const flag = (name: string) => args.includes(name);
const value = (name: string): string | undefined => {
    const i = args.indexOf(name);
    const next = args[i + 1];
    return i >= 0 && next && !next.startsWith('-') ? next : undefined;
};
const oldAt = args.indexOf('--old');
const oldRef = oldAt >= 0 ? (value('--old') ?? process.env.UNC_E2E_OLD_REF ?? 'upstream/main') : undefined;
if (oldRef) {
    const probe = Bun.spawnSync(['git', '-C', root, 'cat-file', '-e', `${oldRef}:src/web/js/app.js`], { stderr: 'pipe' });
    if (probe.exitCode !== 0) {
        console.error(
            `--old ${oldRef}: no hand-written front end there (src/web/js/app.js). --old compares with the pre-5.0 UI; ` +
                `a ref with the TypeScript front end (web/src) needs no comparison, run without --old.`
        );
        process.exit(2);
    }
}
const update = flag('--update');
const verbose = flag('-v');
const modelIds = value('--models')?.split(',') ?? DEFAULT_MODELS;
const onlyScenarios = value('--scenarios')?.split(',');
const jobsMax = Number(value('--jobs') ?? 4);
const out = value('--out') ?? join(tmpdir(), 'unc-e2e');

async function chromePath(): Promise<string> {
    if (process.env.BUN_CHROME_PATH) return process.env.BUN_CHROME_PATH;
    if (process.platform === 'darwin') return '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
    const onPath = Bun.which('chromium') ?? Bun.which('google-chrome-stable');
    if (onPath) return onPath;
    const built = (await $`nix build --inputs-from . --no-link --print-out-paths nixpkgs#chromium`.cwd(root).text()).trim().split('\n')[0] ?? '';
    return join(built, 'bin/chromium');
}

async function browser(): Promise<() => Bun.WebView> {
    const chrome = process.platform !== 'darwin' || process.env.E2E_BACKEND === 'chrome';
    if (!chrome) return () => new Bun.WebView(VIEWPORT);
    const path = await chromePath();
    const argv = ['--no-sandbox', '--disable-dev-shm-usage', '--hide-scrollbars', '--disable-gpu'];
    return () => new Bun.WebView({ ...VIEWPORT, backend: { type: 'chrome', path, argv, stderr: 'inherit' } });
}

type Variant = 'old' | 'new';

interface Job {
    ctx: Ctx;
    scenario: string;
    variant: Variant;
}

interface Result {
    job: Job;
    steps: StepRecord[];
    crash?: string | undefined;
    ms: number;
}

const JOB_LIMIT_MS = 180000;

async function runJob(job: Job, view: Bun.WebView): Promise<Result> {
    const started = Date.now();
    const scenario = SCENARIOS.find(s => s.id === job.scenario)!;
    const h = await startHarness(job.ctx.id, { js: job.variant, oldRef, requestLog: true, inject: pageInit, now: E2E_NOW });
    const session = new Session(view, h);
    let crash: string | undefined;
    try {
        await view.navigate('about:blank'); // Chrome has no target to resize before the first navigation
        await view.resize(VIEWPORT.width, VIEWPORT.height);
        let timer: Timer | undefined;
        const limit = new Promise<never>((_, reject) => {
            timer = setTimeout(() => reject(new Error(`scenario took longer than ${JOB_LIMIT_MS / 1000} s`)), JOB_LIMIT_MS);
        });
        try {
            await Promise.race([scenario.run(session, job.ctx), limit]);
        } finally {
            clearTimeout(timer);
        }
    } catch (error) {
        crash = `after step "${session.steps.at(-1)?.name ?? 'start'}": ${(error as Error).message}`;
        const events = await Promise.race([
            Promise.resolve().then(() => session.eval<unknown[]>(`window.__e2e?.drain() ?? []`)),
            Bun.sleep(2000).then(() => [])
        ]).catch(() => []);
        session.steps.push({ name: 'CRASH', requests: [], events: events as StepRecord['events'], dom: '', text: '' });
    } finally {
        h.stop();
    }
    return { job, steps: session.steps, crash, ms: Date.now() - started };
}

const sorted = (o: object) => JSON.stringify(Object.fromEntries(Object.entries(o).sort(([a], [b]) => (a < b ? -1 : 1))));
const reqLine = (r: ApiRequest) => (r.method === 'POST' ? `${r.api} POST ${sorted(r.body)}` : `${r.api} GET ${sorted(r.query)}`);

/** Writes in order; reads once each (when and how often a poll fires is timing, not behaviour). */
function canonRequests(requests: ApiRequest[]): string[] {
    const writes = requests.filter(r => r.method === 'POST').map(reqLine);
    const reads = [...new Set(requests.filter(r => r.method === 'GET').map(reqLine))].sort();
    return [...writes, ...reads];
}

const KINDS_ERROR = new Set(['console.error', 'console.warn', 'uncaught', 'unhandledrejection', 'resource']);
const stripWhere = (text: string) =>
    (text.split('\n')[0] ?? '')
        .replace(/\(\w[\w.-]*\.js:\d+\)/g, '')
        .replace(/https?:\/\/127\.0\.0\.1:\d+/g, '<host>')
        .replace(/\s+/g, ' ')
        .trim();
const canonEvents = (events: StepRecord['events']) => events.map(e => `${e.kind}: ${stripWhere(e.text)}`).sort();
const errorsOf = (r: Result) => r.steps.flatMap(s => canonEvents(s.events.filter(e => KINDS_ERROR.has(e.kind))));

const hash = (text: string) => new Bun.CryptoHasher('sha1').update(text).digest('hex').slice(0, 12);

interface GoldenStep {
    requests: string[];
    events: string[];
    text: string;
}
type Golden = Record<string, Record<string, Record<string, GoldenStep>>>;

const goldenStep = (s: StepRecord): GoldenStep => ({ requests: canonRequests(s.requests), events: canonEvents(s.events), text: hash(s.text) });

interface Finding {
    where: string;
    what: string;
}

function minus(a: string[], b: string[]): string[] {
    const rest = [...b];
    return a.filter(x => {
        const i = rest.indexOf(x);
        if (i >= 0) rest.splice(i, 1);
        return i < 0;
    });
}

async function lineDiff(a: string, b: string, label: string): Promise<string> {
    const dir = join(out, '.tmp');
    mkdirSync(dir, { recursive: true });
    const fa = join(dir, `${label}.a`);
    const fb = join(dir, `${label}.b`);
    writeFileSync(fa, a);
    writeFileSync(fb, b);
    const diff = await $`diff -U2 ${fa} ${fb}`.nothrow().text();
    return diff;
}

const known = (where: string, what: string) => KNOWN.find(k => k.match.test(`${where} :: ${what}`));

interface Pair {
    model: string;
    scenario: string;
    old?: Result;
    new: Result;
    requestsEqual: boolean;
    errors: { old: number; new: number };
    domDiffLines: number;
    findings: Finding[];
    info: string[];
}

async function comparePair(model: string, scenario: string, o: Result, n: Result): Promise<Pair> {
    const pair: Pair = {
        model,
        scenario,
        old: o,
        new: n,
        requestsEqual: true,
        errors: { old: errorsOf(o).length, new: errorsOf(n).length },
        domDiffLines: 0,
        findings: [],
        info: []
    };
    const add = (where: string, what: string, kind: 'finding' | 'info' = 'finding') => {
        const why = known(where, what);
        if (why) pair.info.push(`${where}: ${what} (known: ${why.reason})`);
        else if (kind === 'finding') pair.findings.push({ where, what });
        else pair.info.push(`${where}: ${what}`);
    };
    if (o.crash) pair.info.push(`old crashed ${o.crash}`);
    if (n.crash) pair.findings.push({ where: `${model}/${scenario}`, what: `new crashed ${n.crash}` });

    const names = new Set([...o.steps.map(s => s.name), ...n.steps.map(s => s.name)]);
    for (const name of names) {
        const so = o.steps.find(s => s.name === name);
        const sn = n.steps.find(s => s.name === name);
        const where = `${model}/${scenario}/${name}`;
        if (!so || !sn) {
            if (!o.crash && !n.crash) add(where, `step only in ${so ? 'old' : 'new'}`);
            continue;
        }
        const ro = canonRequests(so.requests);
        const rn = canonRequests(sn.requests);
        if (JSON.stringify(ro) !== JSON.stringify(rn)) {
            const missing = minus(ro, rn);
            const extra = minus(rn, ro);
            const unknown = [...missing, ...extra].filter(x => !known(where, x));
            if (unknown.length) pair.requestsEqual = false;
            for (const m of missing) add(where, `request only in old: ${m}`);
            for (const x of extra) add(where, `request only in new: ${x}`);
        }
        const eo = canonEvents(so.events);
        const en = canonEvents(sn.events);
        for (const x of minus(en, eo)) add(where, `event only in new: ${x}`);
        for (const x of minus(eo, en)) add(where, `event only in old: ${x}`, 'info');
        if (so.dom !== sn.dom) {
            const diff = await lineDiff(so.dom, sn.dom, 'dom');
            const lines = diff.split('\n').filter(l => /^[+-][^+-]/.test(l) || /^[+-]$/.test(l));
            pair.domDiffLines += lines.length;
            const shown = lines.filter(l => !known(where, l));
            if (shown.length) {
                writeFileSync(join(out, `${model}--${scenario}--${name.replace(/\W+/g, '_')}.dom.diff`), diff);
                add(where, `DOM differs (${lines.length} lines): ${shown.slice(0, 4).join(' / ').slice(0, 300)}`);
            }
        }
    }
    return pair;
}

function compareGolden(model: string, scenario: string, n: Result, golden: Golden): Pair {
    const pair: Pair = { model, scenario, new: n, requestsEqual: true, errors: { old: 0, new: errorsOf(n).length }, domDiffLines: 0, findings: [], info: [] };
    const want = golden[model]?.[scenario];
    if (n.crash) pair.findings.push({ where: `${model}/${scenario}`, what: `crashed ${n.crash}` });
    if (!want) {
        pair.findings.push({ where: `${model}/${scenario}`, what: 'not in golden.json (run with --update)' });
        return pair;
    }
    for (const step of n.steps) {
        if (step.name === 'CRASH') continue;
        const where = `${model}/${scenario}/${step.name}`;
        const g = want[step.name];
        const got = goldenStep(step);
        if (!g) {
            pair.findings.push({ where, what: 'step not in golden.json' });
            continue;
        }
        if (JSON.stringify(g.requests) !== JSON.stringify(got.requests)) {
            pair.requestsEqual = false;
            pair.findings.push({ where, what: `requests differ: want ${JSON.stringify(g.requests)} got ${JSON.stringify(got.requests)}` });
        }
        for (const x of minus(got.events, g.events)) pair.findings.push({ where, what: `new event: ${x}` });
        for (const x of minus(g.events, got.events)) pair.findings.push({ where, what: `missing event: ${x}` });
        if (g.text !== got.text) pair.findings.push({ where, what: 'screen text differs' });
    }
    for (const name of Object.keys(want))
        if (!n.steps.some(s => s.name === name)) pair.findings.push({ where: `${model}/${scenario}/${name}`, what: 'step missing' });
    return pair;
}

const started = Date.now();
rmSync(out, { recursive: true, force: true });
mkdirSync(out, { recursive: true });

const models = await loadModels();
const ctxs: Ctx[] = modelIds.map(id => {
    const m = models.find(x => x.id === id);
    if (!m) throw new Error(`unknown model ${id}`);
    return { id, fan: fixtureHasChip(m), led: m.led !== 'none', probe: m.fan?.kind === 'probe' };
});

const variants: Variant[] = oldRef ? ['old', 'new'] : ['new'];
const queue: Job[] = [];
for (const scenario of SCENARIOS) {
    if (onlyScenarios && !onlyScenarios.includes(scenario.id)) continue;
    for (const ctx of ctxs) if (scenario.applies(ctx)) for (const variant of variants) queue.push({ ctx, scenario: scenario.id, variant });
}

const makeView = await browser();
const results: Result[] = [];
async function worker(): Promise<void> {
    let view = makeView();
    try {
        for (let job = queue.shift(); job; job = queue.shift()) {
            let r = await runJob(job, view);
            // The page did not even come up (machine busy): once more.
            if (r.crash && r.steps.length === 1 && !/took longer/.test(r.crash)) r = await runJob(job, view);
            if (r.crash && /took longer/.test(r.crash)) {
                // an evaluate may still be in flight: this tab is lost
                view.close();
                view = makeView();
            }
            results.push(r);
            if (verbose) console.log(`${job.ctx.id}/${job.scenario} [${job.variant}] ${r.steps.length} steps ${r.ms} ms${r.crash ? ' CRASH ' + r.crash : ''}`);
        }
    } finally {
        view.close();
    }
}
await Promise.all(Array.from({ length: Math.min(jobsMax, queue.length) }, worker));

const get = (model: string, scenario: string, v: Variant) => results.find(r => r.job.ctx.id === model && r.job.scenario === scenario && r.job.variant === v);
const golden: Golden = (await Bun.file(GOLDEN)
    .json()
    .catch(() => ({}))) as Golden;
const pairs: Pair[] = [];
for (const scenario of SCENARIOS) {
    for (const ctx of ctxs) {
        const n = get(ctx.id, scenario.id, 'new');
        if (!n) continue;
        const o = get(ctx.id, scenario.id, 'old');
        pairs.push(o ? await comparePair(ctx.id, scenario.id, o, n) : compareGolden(ctx.id, scenario.id, n, golden));
    }
}

if (update) {
    const next: Golden = onlyScenarios || modelIds.join() !== DEFAULT_MODELS.join() ? golden : {};
    for (const r of results.filter(x => x.job.variant === (oldRef ? 'old' : 'new'))) {
        (next[r.job.ctx.id] ??= {})[r.job.scenario] = Object.fromEntries(r.steps.filter(s => s.name !== 'CRASH').map(s => [s.name, goldenStep(s)]));
    }
    const ordered = (o: object): object => Object.fromEntries(Object.entries(o).sort(([a], [b]) => (a < b ? -1 : 1)));
    for (const k of Object.keys(next)) next[k] = ordered(next[k] ?? {}) as Golden[string];
    writeFileSync(GOLDEN, format(GOLDEN, JSON.stringify(ordered(next), null, 4)));
    console.log(`golden.json written from the ${oldRef ? `old (${oldRef})` : 'new'} front end`);
}

for (const r of results) {
    const dir = join(out, r.job.ctx.id, r.job.scenario);
    mkdirSync(dir, { recursive: true });
    writeFileSync(
        join(dir, `${r.job.variant}.json`),
        JSON.stringify(
            r.steps.map(s => ({ name: s.name, requests: canonRequests(s.requests), events: canonEvents(s.events), dom: s.dom.split('\n').length })),
            null,
            1
        )
    );
    writeFileSync(join(dir, `${r.job.variant}.dom.txt`), r.steps.map(s => `##### ${s.name}\n${s.dom}\n`).join('\n'));
}
rmSync(join(out, '.tmp'), { recursive: true, force: true });

const cell = (p?: Pair) => {
    if (!p) return '-';
    const bad = p.findings.length ? ' FAIL' : '';
    return p.old
        ? `req ${p.requestsEqual ? '=' : 'DIFF'}  err ${p.errors.old}/${p.errors.new}  dom ${p.domDiffLines}${bad}`
        : `req ${p.requestsEqual ? 'ok' : 'DIFF'}  err ${p.errors.new}${bad}`;
};
console.log(
    `\nbackend ${process.platform === 'darwin' && process.env.E2E_BACKEND !== 'chrome' ? 'webkit' : 'chrome'}, ${oldRef ? `new vs ${oldRef}` : 'new vs golden.json'}   [${oldRef ? 'req old=new, err old/new, dom diff lines' : 'req vs golden, err new'}]`
);
const colw = Math.max(...pairs.map(p => cell(p).length), 20) + 2;
console.log('scenario'.padEnd(16) + ctxs.map(c => c.id.padEnd(colw)).join(''));
for (const scenario of SCENARIOS) {
    if (!pairs.some(p => p.scenario === scenario.id)) continue;
    console.log(scenario.id.padEnd(16) + ctxs.map(c => cell(pairs.find(p => p.model === c.id && p.scenario === scenario.id)).padEnd(colw)).join(''));
}

const findings = pairs.flatMap(p => p.findings);
const steps = results.filter(r => r.job.variant === 'new').reduce((a, r) => a + r.steps.length, 0);
if (verbose) for (const p of pairs) for (const i of p.info) console.log(`info  ${i}`);
console.log(`\n${pairs.length} scenario runs, ${steps} steps, ${((Date.now() - started) / 1000).toFixed(1)} s; details in ${out}`);
if (findings.length) {
    console.log(`\n${findings.length} regression${findings.length === 1 ? '' : 's'}:`);
    const groups = new Map<string, string[]>();
    for (const f of findings) groups.set(f.what, [...(groups.get(f.what) ?? []), f.where]);
    let shown = 0;
    for (const [what, wheres] of groups) {
        if (shown++ >= 40) break;
        console.log(`  ${wheres[0]}${wheres.length > 1 ? ` (+${wheres.length - 1} more steps)` : ''}: ${what}`);
    }
    if (groups.size > 40) console.log(`  ... ${groups.size - 40} more kinds (see ${out})`);
    process.exit(1);
}
console.log('no regressions');
process.exit(0);
