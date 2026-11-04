import { expect, test } from 'bun:test';
import { join } from 'node:path';

const root = join(import.meta.dir, '../../..');
const php = Bun.which('php');

// Runs `expr` with the CS201x and panel helpers loaded; `input` arrives as $in.
async function evalPhp(expr: string, input: unknown): Promise<unknown> {
    const script = `
const PLUGIN_DIR = '/x';
require ${JSON.stringify(join(root, 'web/plugin/include/fan_probe.php'))};
require ${JSON.stringify(join(root, 'web/plugin/include/panel.php'))};
$in = json_decode(file_get_contents('php://stdin'), true);
echo json_encode(${expr});`;
    const run = Bun.spawn([php!, '-r', script], { stdin: new Blob([JSON.stringify(input)]), stdout: 'pipe' });
    return JSON.parse(await new Response(run.stdout).text());
}

test.skipIf(!php)('cs201x_fans takes fan1 and fan3, each once; nothing means both', async () => {
    const fans = (v: unknown) => evalPhp('cs201x_fans($in)', v);
    expect(await fans(null)).toEqual([]);
    expect(await fans('')).toEqual([]);
    expect(await fans('fan1')).toEqual(['fan1']);
    expect(await fans('fan3')).toEqual(['fan3']);
    expect(await fans('fan1,fan3')).toEqual(['fan1', 'fan3']);
    expect(await fans('fan3,fan1')).toEqual(['fan3', 'fan1']);
    for (const bad of ['fan2', 'fan1,fan1', 'fan1,', ',fan1', 'fan1,fan2', 'fan1 fan3', 'fan1;reboot', 'FAN1', ['fan1'], 1, 'fan1,fan3,fan1']) {
        expect(await fans(bad)).toBeNull();
    }
});

test.skipIf(!php)('panel_beep_ms accepts 20..2000 as plain digits', async () => {
    const ms = (v: unknown) => evalPhp('panel_beep_ms($in)', v);
    for (const ok of ['20', '99', '200', '999', '1000', '1999', '2000']) expect(await ms(ok)).toBe(Number(ok));
    for (const bad of ['19', '0', '2001', '02000', '0200', '-20', '2e2', '200.5', ' 200', '', 'long', 200, null]) expect(await ms(bad)).toBeNull();
});
