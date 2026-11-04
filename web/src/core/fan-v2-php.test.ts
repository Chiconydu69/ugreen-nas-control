import { expect, test } from 'bun:test';
import { join } from 'node:path';

const root = join(import.meta.dir, '../../..');
const php = Bun.which('php');

interface Case {
    key: string;
    value: string;
    ok: boolean;
}

// The PHP save check (web/plugin/include/fan_v2.php) must never accept what the Go loader rejects; the table is the Go
// test's (backend/internal/fan/config_v2_test.go).
test.skipIf(!php)('fan_v2.php accepts and rejects the same v2 settings as the Go loader', async () => {
    const cases: Case[] = await Bun.file(join(root, 'backend/internal/fan/testdata/v2_validation.json')).json();
    const script = `<?php
require ${JSON.stringify(join(root, 'web/plugin/include/fan_v2.php'))};
$cases = json_decode(file_get_contents('php://stdin'), true);
echo json_encode(array_map(fn($c) => fan_v2_valid($c['key'], $c['value']), $cases));`;
    const run = Bun.spawn([php!, '-r', script.replace(/^<\?php/, '')], { stdin: new Blob([JSON.stringify(cases)]), stdout: 'pipe' });
    const got: boolean[] = JSON.parse(await new Response(run.stdout).text());
    const wrong = cases.filter((c, i) => got[i] !== c.ok).map(c => `${c.key}=${JSON.stringify(c.value)} want ${c.ok}`);
    expect(wrong).toEqual([]);
});
