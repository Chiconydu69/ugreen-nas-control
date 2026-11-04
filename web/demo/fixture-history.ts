// Sample fan history (api.php action fan_history) for the harness and the demo: the shape of backend/internal/fan/history.go
// with made-up but plausible days. The CPU warms up in bursts on top of a daily wave, the drives follow the day and warm up
// during a parity check, the mover raises the case fans twice a day, and the quiet hours (23:00 to 07:00) cap them. The
// fans follow the sample curves of the fixture (fanConfig) with the daemon's rules: up at once, down 2 % per 2 s tick,
// job floor and boost, quiet cap that a job floor beats. Deterministic: the same time gives the same numbers.
import { curveAt, parseCurve } from '../src/core/curve';
import { MOD, type FanHistoryRange, type FanHistoryResponse, type FanHistorySeries } from '../src/core/contract-fan-history';
import { berlinOffset } from '../src/features/nas/fan/history-model';
import { fanConfig, fixtureHasChip } from '../models/fixture';
import type { Model } from '../models/schema';

const STEP = 10;
const FINE = 360;
const COARSE = 1440;
const RAMP_DOWN = 10; // % per 10 s: 2 % per 2 s tick
const MIN_RUN = 25;
const CAP = 40;
const JOB_FLOOR = 75;
const JOB_BOOST = 20;
const offsets = new Map<number, number>();
/** Europe/Berlin offset, cached per hour (Intl is slow). */
const offsetAt = (t: number): number => {
    const hour = Math.floor(t / 3600);
    let v = offsets.get(hour);
    if (v === undefined) offsets.set(hour, (v = berlinOffset(hour * 3600)));
    return v;
};
const DRIVERS = ['other', 'cpu', 'disk:disk1', 'disk:disk2'];

const frac = (n: number): number => n - Math.floor(n);
const hash = (n: number): number => frac(Math.sin(n * 12.9898) * 43758.5453);
const wave = (t: number, seed: number): number => 0.55 * Math.sin(t / 41 + seed) + 0.45 * Math.sin(t / 173 + seed * 2.3);
/** The same, over hours: how a drive warms and cools. */
const slow = (t: number, seed: number): number => 0.55 * Math.sin(t / 1900 + seed) + 0.45 * Math.sin(t / 5300 + seed * 2.3);
/** 0 to 1 and back to 0 over [0, 1]: a burst that warms up quickly and cools slowly. */
const bump = (x: number): number => (x <= 0 || x >= 1 ? 0 : x < 0.3 ? x / 0.3 : (1 - x) / 0.7);

interface Inputs {
    cpu: number;
    disk1: number;
    disk2: number;
    quiet: boolean;
    mover: boolean;
    parity: boolean;
}

function inputs(t: number, offset: number): Inputs {
    const local = (((t + offset) % 86400) + 86400) % 86400;
    const hour = local / 3600;
    const day = Math.cos(((hour - 15) / 24) * 2 * Math.PI); // warmest at 15:00

    // CPU bursts: a slot of 15 minutes has one now and then (more by day), 3 to 7 minutes long
    const slot = Math.floor(t / 900);
    const at = (t - slot * 900) / 60;
    const len = 3 + hash(slot + 3) * 4;
    const start = hash(slot + 1) * (15 - len);
    const burst = hash(slot) < 0.2 + 0.25 * ((day + 1) / 2) ? bump((at - start) / len) * (12 + hash(slot + 7) * 14) : 0;
    const cpu = 41 + 4 * day + burst + 0.8 * wave(t, 1);

    const parity = hour >= 1.5 && hour < 5.5;
    const mover = (hour >= 3 && hour < 3.34) || (hour >= 15 && hour < 15.34);
    const parityHeat = parity ? 7 * Math.min(1, (hour - 1.5) * 3, (5.5 - hour) * 1.2) : 0;
    const base = 36 + 2.2 * day + parityHeat;
    return {
        cpu: Math.round(cpu),
        disk1: Math.round(base + 2.6 * slow(t, 4) + (mover ? 1.5 : 0)),
        disk2: Math.round(base + 0.6 + 2.6 * slow(t, 9)),
        quiet: hour >= 23 || hour < 7,
        mover,
        parity
    };
}

interface Fine {
    t: number;
    cpu: number;
    disk: number;
    fans: { pct: number; rpm: number; temp: number; drv: number; mods: number }[];
}

function simulate(m: Model, from: number, count: number): Fine[] {
    const cfg = fanConfig();
    const curves = { cpu: parseCurve(cfg.cpu.curve), case: parseCurve(cfg.case.curve) };
    const fans = Object.entries(m.fan?.fans ?? {}).map(([id, role]) => ({ id, role: role === 'cpu' ? ('cpu' as const) : ('case' as const) }));
    const cur = fans.map(() => -1);
    const out: Fine[] = [];
    const warm = 24; // samples before the window, so the ramps have settled

    for (let i = -warm; i < count; i++) {
        const t = from + i * STEP;
        const x = inputs(t, offsetAt(t));
        const hot = x.disk2 >= x.disk1 ? 3 : 2;
        const disk = Math.max(x.disk1, x.disk2);
        const row: Fine = { t, cpu: x.cpu, disk, fans: [] };

        fans.forEach((f, k) => {
            const cpuFan = f.role === 'cpu';
            const temp = cpuFan ? x.cpu : disk;
            const raw = curveAt(curves[f.role], temp);
            const target = Math.max(raw, MIN_RUN);
            let mods = raw < MIN_RUN ? MOD.floor : 0;

            const prev = cur[k] as number;
            let pct = prev < 0 || target >= prev ? target : Math.max(target, prev - RAMP_DOWN);
            if (pct !== target) mods |= MOD.shape;

            if (!cpuFan) {
                const job = x.mover ? { floor: JOB_FLOOR, boost: 0 } : x.parity ? { floor: 0, boost: JOB_BOOST } : { floor: 0, boost: 0 };
                const jobbed = x.mover ? Math.max(pct, job.floor) : x.parity ? Math.min(pct + job.boost, 100) : pct;
                if (jobbed !== pct) mods |= x.mover ? MOD.mover : MOD.parity;
                pct = jobbed;
                if (x.quiet) {
                    const bound = Math.min(100, Math.max(CAP, MIN_RUN, job.floor) + job.boost);
                    if (pct > bound) {
                        pct = bound;
                        mods |= MOD.quiet;
                    }
                }
            }
            cur[k] = pct;
            const max = cpuFan ? 2400 : 1900;
            const rpm = Math.round(300 + (pct / 100) * (max - 300) + 8 * wave(t, 20 + k));
            if (i >= 0) row.fans.push({ pct, rpm, temp, drv: cpuFan ? 1 : hot, mods });
        });
        if (i >= 0) out.push(row);
    }
    return out;
}

const tenth = (n: number): number => Math.round(n * 10) / 10;
const mean = (list: number[]): number => list.reduce((a, b) => a + b, 0) / list.length;

/** The answer for a model and a range, ending at `now` (unix seconds). A model without fans has none. */
export function fanHistory(m: Model, now: number, range: FanHistoryRange): FanHistoryResponse {
    if (!fixtureHasChip(m)) return { ok: true, running: false };
    const ids = Object.entries(m.fan?.fans ?? {});
    const step = range === '1h' ? STEP : 60;
    const points = range === '1h' ? FINE : COARSE;
    const group = step / STEP;
    const end = Math.floor(now / step) * step;
    const first = end - (points - 1) * step;
    const fine = simulate(m, first, points * group);

    const t: number[] = [];
    const cpu: number[] = [];
    const disk: number[] = [];
    const series: FanHistorySeries[] = ids.map(([id]) => ({ id, pct: [], rpm: [], temp: [], drv: [], mods: [] }));
    for (let p = 0; p < points; p++) {
        const chunk = fine.slice(p * group, (p + 1) * group);
        t.push(first + p * step);
        cpu.push(tenth(mean(chunk.map(c => c.cpu))));
        disk.push(tenth(mean(chunk.map(c => c.disk))));
        series.forEach((s, k) => {
            const f = chunk.map(c => c.fans[k]!);
            s.pct.push(Math.round(mean(f.map(x => x.pct))));
            s.rpm.push(Math.round(mean(f.map(x => x.rpm))));
            s.temp.push(tenth(mean(f.map(x => x.temp))));
            const counts = new Map<number, number>();
            f.forEach(x => counts.set(x.drv, (counts.get(x.drv) ?? 0) + 1));
            s.drv.push([...counts.entries()].sort((a, b) => b[1] - a[1])[0]![0]);
            s.mods.push(f.reduce((a, x) => a | x.mods, 0));
        });
    }
    return {
        ok: true,
        running: true,
        range,
        step,
        now: end,
        since: end - 26 * 3600,
        fans: ids.map(([id, role]) => ({ id, role })),
        drivers: DRIVERS,
        t,
        cpu,
        case: disk,
        series
    };
}
