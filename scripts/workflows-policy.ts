// Policy lint for .github/workflows/*.yml (`just workflows-policy`, part of `just workflows-lint`). actionlint checks syntax and
// zizmor checks security patterns (it also rejects `uses:` not pinned to a commit SHA, so that is not repeated here); this checks the repo's own rules:
//   1. every job carries the repository guard in its `if:`, so forks skip it (workflows in FORK_WORKFLOWS run in forks on purpose)
//   2. a workflow triggered by pull_request_target or workflow_run has no actions/checkout and passes PR/issue text through `env:`, never inline in `run:`
//   3. the workflow has a top-level `permissions: {}` (jobs add what they need)
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

export const REPO = 'ugreen-nas-control/ugreen-nas-control';
export const GUARD = `github.repository == '${REPO}'`;
/** Workflows that run in forks for contributors, so their jobs carry no guard. */
export const FORK_WORKFLOWS = new Set(['checks.yml']);

type Obj = Record<string, any>;

const INLINE_EVENT_TEXT = /github\.event\.pull_request\.head\.|github\.event\.[\w.]*\.(body|title)\b/;

/** Problems of one workflow file as "file: job: message (fix)" lines. */
export function lint(file: string, text: string, doc: Obj = Bun.YAML.parse(text) as Obj): string[] {
    const out: string[] = [];
    const jobs: Obj = doc?.jobs ?? {};

    const p = doc?.permissions;
    if (!(p && typeof p === 'object' && Object.keys(p).length === 0)) {
        out.push(`${file}: no top-level \`permissions: {}\` (add it below \`on:\` and grant per job)`);
    }

    if (!FORK_WORKFLOWS.has(file)) {
        for (const [name, job] of Object.entries<Obj>(jobs)) {
            if (!String(job?.if ?? '').includes(GUARD)) {
                out.push(
                    `${file}: job ${name}: no repository guard (add \`if: \${{ ${GUARD} }}\`, combined with \`&&\` if it has an \`if:\`; forks skip the job)`
                );
            }
        }
    }

    const on = doc?.on ?? doc?.[true as any];
    const triggers = typeof on === 'string' ? [on] : Array.isArray(on) ? on : Object.keys(on ?? {});
    if (triggers.some(t => t === 'pull_request_target' || t === 'workflow_run')) {
        for (const [name, job] of Object.entries<Obj>(jobs)) {
            for (const step of job?.steps ?? []) {
                if (String(step?.uses ?? '').startsWith('actions/checkout')) {
                    out.push(
                        `${file}: job ${name}: actions/checkout in a pull_request_target/workflow_run workflow (remove it: this trigger has write access and a secret scope, PR code must not run)`
                    );
                }
                if (INLINE_EVENT_TEXT.test(String(step?.run ?? ''))) {
                    out.push(`${file}: job ${name}: PR or issue text inside \`run:\` (pass it through \`env:\` and use "$VAR")`);
                }
            }
        }
    }
    return out;
}

if (import.meta.main) {
    const dir = '.github/workflows';
    const problems = readdirSync(dir)
        .filter(f => f.endsWith('.yml') || f.endsWith('.yaml'))
        .sort()
        .flatMap(f => lint(f, readFileSync(join(dir, f), 'utf8')));
    if (problems.length) {
        console.error(problems.join('\n'));
        process.exit(1);
    }
    console.log('workflows policy: ok');
}
