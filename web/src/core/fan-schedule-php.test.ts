import { expect, test } from 'bun:test';
import { join } from 'node:path';

const root = join(import.meta.dir, '../../..');
const php = Bun.which('php');

async function build(settings: object, daemon: object | null, now: number) {
    const script = `
require ${JSON.stringify(join(root, 'web/plugin/include/fan_schedule.php'))};
$in = json_decode(file_get_contents('php://stdin'), true);
echo json_encode(fan_schedule_build($in['settings'], $in['daemon'], $in['now']));`;
    const run = Bun.spawn([php!, '-r', script], { stdin: new Blob([JSON.stringify({ settings, daemon, now })]), stdout: 'pipe' });
    return JSON.parse(await new Response(run.stdout).text());
}

test.skipIf(!php)('fan_schedule: defaults without cfg_version, v2 settings, live and stale daemon', async () => {
    const off = await build({ quiet_enabled: '1' }, null, 1000);
    expect(off.quiet).toMatchObject({ enabled: false, from: '23:00', to: '07:00', cap: 40, scope: 'case', active: false });
    expect(off).toMatchObject({ job: { mover: false, parity: false, active: '' }, cap: null });

    const cfg = { cfg_version: '2', quiet_enabled: '1', quiet_from: '22:00', quiet_to: '06:30', quiet_cap: '30', quiet_days: 'mon,tue' };
    const live = { time: 995, quiet_active: true, job: 'parity', cap: 30 };
    const on = await build(cfg, live, 1000);
    expect(on.quiet).toMatchObject({ enabled: true, from: '22:00', to: '06:30', cap: 30, days: 'mon,tue', active: true, until: '06:30' });
    expect(on).toMatchObject({ job: { mover: false, parity: true, active: 'parity' }, cap: 30 });

    const stale = await build(cfg, live, 1100);
    expect(stale.quiet.active).toBe(false);
    expect(stale.quiet.until).toBeUndefined();
    expect(stale).toMatchObject({ job: { active: '' }, cap: null });

    expect((await build({ ...cfg, quiet_to: '22:00' }, null, 1)).quiet.enabled).toBe(false);
});
