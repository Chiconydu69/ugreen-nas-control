import { expect, test } from 'bun:test';
import { MOD } from '../src/core/contract-fan-history';
import { fanHistory } from './fixture-history';
import { loadModels } from '../models/index';

const NOW = Date.UTC(2026, 9, 30, 12, 0, 0) / 1000;

test('the sample history has the daemon shape for both ranges', async () => {
    const m = (await loadModels()).find(x => x.id === 'dxp6800-pro')!;
    for (const [range, step, points] of [
        ['1h', 10, 360],
        ['24h', 60, 1440]
    ] as const) {
        const h = fanHistory(m, NOW, range);
        expect(h.running).toBe(true);
        expect([h.step, h.t!.length, h.now]).toEqual([step, points, NOW]);
        expect(h.t![points - 1]! - h.t![0]!).toBe((points - 1) * step);
        expect(h.fans!.map(f => f.id)).toEqual(['fan2', 'fan3', 'fan4']);
        for (const s of h.series!) {
            for (const list of [s.pct, s.rpm, s.temp, s.drv, s.mods]) expect(list.length).toBe(points);
            expect(s.pct.every(p => p != null && p >= 25 && p <= 100)).toBe(true);
            expect(s.drv.every(d => d >= 0 && d < h.drivers!.length)).toBe(true);
        }
        expect(h.cpu!.length).toBe(points);
    }
});

test('a day shows the story: quiet hours cap the case fans, the mover and the parity check raise them, the drive changes', async () => {
    const m = (await loadModels()).find(x => x.id === 'dxp6800-pro')!;
    const h = fanHistory(m, NOW, '24h');
    const [cpuFan, caseFan] = [h.series![0]!, h.series![1]!];
    const seen = (mods: number[], bit: number) => mods.some(x => (x & bit) !== 0);
    expect(seen(caseFan.mods, MOD.quiet)).toBe(true);
    expect(seen(caseFan.mods, MOD.mover)).toBe(true);
    expect(seen(caseFan.mods, MOD.parity)).toBe(true);
    expect(seen(cpuFan.mods, MOD.quiet)).toBe(false);
    expect(new Set(caseFan.drv.map(d => h.drivers![d])).size).toBeGreaterThan(1);
    expect(new Set(cpuFan.drv.map(d => h.drivers![d]))).toEqual(new Set(['cpu']));
    // the CPU fan really moves with the CPU
    expect(Math.max(...cpuFan.pct.map(x => x!)) - Math.min(...cpuFan.pct.map(x => x!))).toBeGreaterThan(30);
    // deterministic
    expect(fanHistory(m, NOW, '24h')).toEqual(h);
});

test('models without a fan chip have no history', async () => {
    const probe = (await loadModels()).find(x => x.fan?.kind === 'probe');
    expect(probe && fanHistory(probe, NOW, '1h')).toEqual(probe ? { ok: true, running: false } : undefined);
});
