import { expect, test } from 'bun:test';
import { join } from 'node:path';

const root = join(import.meta.dir, '../../..');
const php = Bun.which('php');

// Runs `fn` over every input in one php process; a spawn per case (~0.13 s each, far more under load) timed out.
async function evalPhp(fn: string, inputs: unknown[]): Promise<unknown[]> {
    const script = `
const PLUGIN_DIR = '/x';
require ${JSON.stringify(join(root, 'web/plugin/include/fan_probe.php'))};
require ${JSON.stringify(join(root, 'web/plugin/include/panel.php'))};
echo json_encode(array_map(${JSON.stringify(fn)}, json_decode(file_get_contents('php://stdin'), true)));`;
    const run = Bun.spawn([php!, '-r', script], { stdin: new Blob([JSON.stringify(inputs)]), stdout: 'pipe' });
    return JSON.parse(await new Response(run.stdout).text());
}

test.skipIf(!php)('cs201x_fans takes fan1 and fan3, each once; nothing means both', async () => {
    const good: [unknown, string[]][] = [
        [null, []],
        ['', []],
        ['fan1', ['fan1']],
        ['fan3', ['fan3']],
        ['fan1,fan3', ['fan1', 'fan3']],
        ['fan3,fan1', ['fan3', 'fan1']]
    ];
    const bad = ['fan2', 'fan1,fan1', 'fan1,', ',fan1', 'fan1,fan2', 'fan1 fan3', 'fan1;reboot', 'FAN1', ['fan1'], 1, 'fan1,fan3,fan1'];
    const got = await evalPhp('cs201x_fans', [...good.map(([v]) => v), ...bad]);
    expect(got).toEqual([...good.map(([, want]) => want), ...bad.map(() => null)]);
});

test.skipIf(!php)('panel_beep_ms accepts 20..2000 as plain digits', async () => {
    const ok = ['20', '99', '200', '999', '1000', '1999', '2000'];
    const bad = ['19', '0', '2001', '02000', '0200', '-20', '2e2', '200.5', ' 200', '', 'long', 200, null];
    const got = await evalPhp('panel_beep_ms', [...ok, ...bad]);
    expect(got).toEqual([...ok.map(Number), ...bad.map(() => null)]);
});
