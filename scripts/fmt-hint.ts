// CI format check (checks.yml, inside `nix develop .#fmt`): runs `just fmt-check`; on failure prints the formatting hint only when the
// formatter itself ran cleanly and changed files, otherwise reports a CI infrastructure problem. Exit code is 1 on every failure.
export type Run = (cmd: string[]) => { code: number; out: string };

export const INFRA = '::error title=CI infrastructure::Could not run the formatter (toolchain or download failure). This is not a problem with your change.';

export function hint(files: string[]): string {
    const list = files.length ? files.join(', ') : 'see the log above';
    return `::error title=Formatting::The formatter would change: ${list}. Maintainers fix formatting when they merge; you don't have to.`;
}

export function formatCheck(run: Run, log: (line: string) => void): number {
    if (run(['just', 'fmt-check']).code === 0) return 0;
    if (run(['just', 'fmt']).code !== 0) return (log(INFRA), 1);
    if (run(['git', 'diff', '--quiet']).code !== 1) return (log(INFRA), 1);
    log(run(['git', 'diff', '-U0']).out);
    log(hint(run(['git', 'diff', '--name-only']).out.split('\n').filter(Boolean)));
    return 1;
}

if (import.meta.main) {
    const run: Run = cmd => {
        const p = Bun.spawnSync(cmd, { stdout: 'pipe', stderr: 'inherit' });
        return { code: p.exitCode ?? 1, out: p.stdout.toString() };
    };
    process.exit(formatCheck(run, console.log));
}
