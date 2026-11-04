import { expect, test } from 'bun:test';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const root = join(import.meta.dir, '../../..');
const php = Bun.which('php');

// Runs `expr` with the probe and backlight helpers loaded; `input` arrives as $in.
async function evalPhp(expr: string, input: unknown): Promise<unknown> {
    const script = `
require ${JSON.stringify(join(root, 'web/plugin/include/fan_probe.php'))};
require ${JSON.stringify(join(root, 'web/plugin/include/fan_backlight.php'))};
$in = json_decode(file_get_contents('php://stdin'), true);
echo json_encode(${expr});`;
    const run = Bun.spawn([php!, '-r', script], { stdin: new Blob([JSON.stringify(input)]), stdout: 'pipe' });
    return JSON.parse(await new Response(run.stdout).text());
}

test.skipIf(!php)('fan_probe_choice accepts pwm1..pwm9 with cpu or case and nothing else', async () => {
    const choice = (map: unknown) => evalPhp('fan_probe_choice($in)', map);
    expect(await choice({ pwm2: 'cpu', pwm3: 'case' })).toEqual(['pwm2=cpu', 'pwm3=case']);
    for (const bad of [
        null,
        [],
        {},
        { pwm0: 'cpu' },
        { pwm10: 'cpu' },
        { fan2: 'cpu' },
        { pwm2: 'off' },
        { pwm2: '' },
        { 'pwm2;rm': 'cpu' },
        { pwm2: ['cpu'] }
    ]) {
        expect(await choice(bad)).toBeNull();
    }
});

test.skipIf(!php)('fan_backlight_pct accepts 0..100 as plain digits', async () => {
    const pct = (v: unknown) => evalPhp('fan_backlight_pct($in)', v);
    for (const ok of ['0', '1', '55', '100']) expect(await pct(ok)).toBe(Number(ok));
    for (const bad of ['101', '-1', '05', '1.5', ' 5', '', 'abc', '1e1', 50, null]) expect(await pct(bad)).toBeNull();
});

test.skipIf(!php)('fan_tool_error drops the ERROR prefix', async () => {
    expect(await evalPhp('fan_tool_error($in)', 'ERROR: another tool runs\n')).toBe('another tool runs');
    expect(await evalPhp('fan_tool_error($in)', '')).toBe('The fan tool failed');
});

test.skipIf(!php)('run() parses only stdout: stderr noise on success does not break the JSON', async () => {
    const api = join(root, 'web/plugin/api.php');
    const source = await Bun.file(api).text();
    const fn = /function run\(.*?\n}\n/s.exec(source)?.[0];
    expect(fn).toBeDefined();
    const dir = mkdtempSync(join(tmpdir(), 'unc-php-'));
    const harness = join(dir, 'harness.php');
    writeFileSync(
        harness,
        `<?php
${fn}
$ok = run('/bin/sh', ['-c', 'echo warn >&2; echo \\'{"ok":true}\\'']);
$bad = run('/bin/sh', ['-c', 'echo ERROR: nope >&2; exit 3']);
echo json_encode([json_decode($ok['output'], true), $ok['code'], $bad['message'], $bad['code'], $bad['output']]);
`
    );
    const proc = Bun.spawn([php!, harness], { stdout: 'pipe' });
    const out = await new Response(proc.stdout).text();
    rmSync(dir, { recursive: true });
    expect(JSON.parse(out)).toEqual([{ ok: true }, 0, 'ERROR: nope', 3, '']);
});
