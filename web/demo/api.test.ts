// Parity: for every model and every read action the in-browser API answers what the PHP harness answers
// (web/tools/harness: api-fixture.php for the NAS, the real led/api.php on the fake ugreen-led-ctl for the LEDs).
// Needs the install tree: `just web-build`. Run: `bun test demo` in web/ (`just demo` does).
import { expect, test } from 'bun:test';
import { loadModels } from '../models/index';
import { withVariants } from './variants';
import { startHarness } from '../tools/harness/serve';
import { createDemo, type Store } from './api';

const memory = (): Store => {
    const map = new Map<string, string>();
    return { getItem: k => map.get(k) ?? null, setItem: (k, v) => void map.set(k, v), removeItem: k => void map.delete(k) };
};

// PHP turns an empty JSON object into an empty array; both mean "nothing" to the page.
// The NIC counters grow with the clock: only their names are compared.
const norm = (v: unknown, key = ''): unknown => {
    if (key === 'net_io' && Array.isArray(v)) return v.map(n => (n as { name: string }).name);
    if (Array.isArray(v)) return v.length ? v.map(x => norm(x)) : {};
    if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, norm(x, k)]));
    return v;
};

const READS: [string, string, string][] = [
    ['nas', 'api.php', 'action=status'],
    ['nas', 'api.php', 'action=hardware'],
    ['nas', 'api.php', 'action=fan_inputs'],
    ['nas', 'api.php', 'action=fan_schedule'],
    ['nas', 'api.php', 'action=fan_profile_sources'],
    ['nas', 'api.php', 'action=fan_probe_status'],
    ['nas', 'api.php', 'action=fan_probe_tachs'],
    ['nas', 'api.php', 'action=fan_backlight'],
    ['nas', 'api.php', 'action=cs201x_read'],
    ['nas', 'api.php', 'action=panel_buttons'],
    ['nas', 'api.php', 'action=power_ac_get'],
    ['led', 'led/api.php', 'action=status'],
    ['led', 'led/api.php', 'action=list'],
    ['led', 'led/api.php', 'action=all'],
    ['led', 'led/api.php', 'action=bays'],
    ['led', 'led/api.php', 'action=schedule'],
    ['led', 'led/api.php', 'action=led&led=power'],
    ['led', 'led/api.php', 'action=led&led=disk1']
];

test('every read action answers like the PHP harness, for every model', async () => {
    for (const model of withVariants(await loadModels())) {
        const harness = await startHarness(model.id);
        try {
            const demo = createDemo(model, memory());
            for (const [, file, query] of READS) {
                const real = await fetch(`${harness.base}/plugins/ugreen-nas-control/${file}?${query}`);
                const mine = demo.handle('GET', `/plugins/ugreen-nas-control/${file}`, new URLSearchParams(query), new URLSearchParams());
                const label = `${model.id} ${file}?${query}`;
                expect({ label, status: mine.status, json: norm(mine.json) }).toEqual({ label, status: real.status, json: norm(await real.json()) });
            }
        } finally {
            harness.stop();
        }
    }
}, 120000);

test('writes show up in the next read, per model, and reset clears them', async () => {
    const [model] = await loadModels();
    if (!model) throw new Error('no models');
    const store = memory();
    const demo = createDemo(model, store);
    const post = (file: string, body: Record<string, string>) => demo.handle('POST', `/x/${file}`, new URLSearchParams(), new URLSearchParams(body));
    const get = (file: string, query: string) =>
        demo.handle('GET', `/x/${file}`, new URLSearchParams(query), new URLSearchParams()).json as Record<string, any>;

    expect(post('api.php', { action: 'mode', value: 'quiet' }).status).toBe(200);
    expect(get('api.php', 'action=status').settings.mode).toBe('quiet');
    expect(post('api.php', { action: 'min', min_pct: '5' }).status).toBe(400);
    expect(post('led/api.php', { action: 'color', led: 'power', r: '1', g: '2', b: '3' }).status).toBe(200);
    expect(get('led/api.php', 'action=all').leds.power.color).toBe('1 2 3');
    expect(post('led/api.php', { action: 'color', led: 'power', r: '1', g: '2', b: '300' }).status).toBe(400);

    // A second session on the same store sees the same state.
    const again = createDemo(model, store);
    expect((again.handle('GET', '/x/api.php', new URLSearchParams('action=status'), new URLSearchParams()).json as any).settings.mode).toBe('quiet');
});

test('fan actions: v2 settings, probe flow and backlight keep their state', async () => {
    const models = await loadModels();
    const demoOf = (id: string) =>
        createDemo(
            models.find(m => m.id === id)!,
            memory()
        );
    const call = (d: ReturnType<typeof createDemo>, method: 'GET' | 'POST', query: string) =>
        d.handle(
            method,
            '/x/api.php',
            method === 'GET' ? new URLSearchParams(query) : new URLSearchParams(),
            method === 'POST' ? new URLSearchParams(query) : new URLSearchParams()
        );

    const gt = demoOf('dxp4800-gt');
    expect(call(gt, 'POST', 'action=fan_v2&values[quiet_enabled]=1&values[quiet_cap]=60&values[cpu_allow_off]=1').status).toBe(200);
    const cfg = (call(gt, 'GET', 'action=status').json as any).settings.fan_config;
    expect([cfg.version, cfg.quiet.enabled, cfg.quiet.cap, cfg.cpu.allow_off]).toEqual([2, true, 60, true]);
    expect((call(gt, 'GET', 'action=fan_schedule').json as any).quiet.cap).toBe(60);
    expect(call(gt, 'POST', 'action=fan_v2&values[quiet_cap]=5').status).toBe(400);
    expect(call(gt, 'GET', 'action=fan_probe_tachs').status).toBe(409);

    const probe = demoOf('dxp2800');
    expect(call(probe, 'POST', 'action=fan_probe_confirm&map[pwm3]=cpu').status).toBe(409);
    expect(call(probe, 'POST', 'action=fan_probe_run').status).toBe(200);
    expect(call(probe, 'POST', 'action=fan_probe_confirm&map[pwm1]=cpu').status).toBe(409);
    const done = call(probe, 'POST', 'action=fan_probe_confirm&map[pwm3]=cpu&map[pwm2]=case');
    expect(done.status).toBe(200);
    expect((call(probe, 'GET', 'action=fan_probe_status').json as any).fan_control_possible).toBe(true);
    expect((call(probe, 'GET', 'action=status').json as any).fan_caps.channels).toBe(2);

    const idx = demoOf('idx6011');
    expect(call(idx, 'POST', 'action=fan_backlight&pct=30').json).toEqual({ ok: true, pct: 30 });
    expect((call(idx, 'GET', 'action=fan_backlight').json as any).pct).toBe(30);
    expect(call(idx, 'POST', 'action=fan_backlight&pct=101').status).toBe(400);
    expect(call(gt, 'POST', 'action=fan_backlight&pct=30').status).toBe(409);
});

// Time-dependent, so not in the READS comparison: the harness (PHP) and the demo give the same shape for both ranges.
test('fan_history: same shape from the harness and the demo, bad range refused', async () => {
    const model = (await loadModels()).find(m => m.id === 'dxp6800-pro')!;
    const harness = await startHarness(model.id);
    try {
        const demo = createDemo(model, memory());
        for (const range of ['1h', '24h']) {
            const query = `action=fan_history&range=${range}`;
            const real = (await (await fetch(`${harness.base}/plugins/ugreen-nas-control/api.php?${query}`)).json()) as any;
            const mine = demo.handle('GET', '/plugins/ugreen-nas-control/api.php', new URLSearchParams(query), new URLSearchParams()).json as any;
            const shape = (h: any) => ({
                keys: Object.keys(h).sort(),
                series: Object.keys(h.series[0]).sort(),
                fans: h.fans,
                drivers: h.drivers,
                points: h.t.length,
                step: h.step,
                lengths: h.series.map((s: any) => [s.pct, s.rpm, s.temp, s.drv, s.mods].map(a => a.length))
            });
            expect(shape(mine)).toEqual(shape(real));
            expect(Math.abs(mine.now - real.now)).toBeLessThan(120);
        }
        const bad = await fetch(`${harness.base}/plugins/ugreen-nas-control/api.php?action=fan_history&range=7d`);
        expect(bad.status).toBe(400);
        const mine = demo.handle('GET', '/x/api.php', new URLSearchParams('action=fan_history&range=7d'), new URLSearchParams());
        expect(mine.status).toBe(400);
    } finally {
        harness.stop();
    }
}, 60000);
