import { expect, test } from 'bun:test';
import { formatCheck, hint, INFRA, type Run } from './fmt-hint';

/** Fake commands: `codes` maps a command line to its exit code (default 0); `names` is the `git diff --name-only` output. */
function fake(codes: Record<string, number>, names = '') {
    const calls: string[] = [];
    const run: Run = cmd => {
        const line = cmd.join(' ');
        calls.push(line);
        return { code: codes[line] ?? 0, out: line === 'git diff --name-only' ? names : '' };
    };
    const lines: string[] = [];
    return { calls, lines, code: () => formatCheck(run, l => lines.push(l)) };
}

test('check passes: no output, formatter not run', () => {
    const f = fake({});
    expect(f.code()).toBe(0);
    expect(f.calls).toEqual(['just fmt-check']);
    expect(f.lines).toEqual([]);
});

test('formatter fails (toolchain or download): infrastructure error, no hint', () => {
    const f = fake({ 'just fmt-check': 1, 'just fmt': 1 });
    expect(f.code()).toBe(1);
    expect(f.lines).toEqual([INFRA]);
});

test('formatter succeeds with a diff: hint names the files', () => {
    const f = fake({ 'just fmt-check': 1, 'git diff --quiet': 1 }, 'a.ts\nb.go\n');
    expect(f.code()).toBe(1);
    expect(f.lines.at(-1)).toBe(hint(['a.ts', 'b.go']));
    expect(f.lines).not.toContain(INFRA);
});

test('formatter succeeds without a diff but the check failed: infrastructure error, no hint', () => {
    const f = fake({ 'just fmt-check': 1 });
    expect(f.code()).toBe(1);
    expect(f.lines).toEqual([INFRA]);
});
