import type { CalibRows } from './contract';

export interface CurvePoint {
    temp: number;
    pct: number;
}

/** "40:30,60:50" -> points; pieces that are not numbers are dropped. */
export const parseCurve = (text: string | null | undefined): CurvePoint[] =>
    String(text || '')
        .split(',')
        .map(part => {
            const [temp, pct] = part.split(':').map(Number);
            return { temp: temp as number, pct: pct as number };
        })
        .filter(p => Number.isFinite(p.temp) && Number.isFinite(p.pct));

export const curveText = (points: CurvePoint[]): string => points.map(p => `${p.temp}:${p.pct}`).join(',');

/** Speed for a temperature, linear between the points (as the daemon does it). */
export function curveAt(points: CurvePoint[], temp: number): number {
    const first = points[0];
    const last = points[points.length - 1];
    if (!first || !last) return 100;
    if (temp <= first.temp) return first.pct;
    for (let i = 1; i < points.length; i++) {
        const a = points[i - 1] as CurvePoint;
        const b = points[i] as CurvePoint;
        if (temp <= b.temp) {
            return b.temp === a.temp ? b.pct : Math.round(a.pct + ((temp - a.temp) / (b.temp - a.temp)) * (b.pct - a.pct));
        }
    }
    return last.pct;
}

/** The same rule as valid_curve() in api.php: 2 or more integer points, rising temperatures 0-120 °C, speeds 0-100 %. */
export const validCurve = (points: CurvePoint[]): boolean =>
    points.length >= 2 &&
    points.every(
        (p, i) =>
            Number.isInteger(p.temp) &&
            Number.isInteger(p.pct) &&
            p.temp >= 0 &&
            p.temp <= 120 &&
            p.pct >= 0 &&
            p.pct <= 100 &&
            (i === 0 || p.temp > (points[i - 1] as CurvePoint).temp)
    );

export function effectivePoints(points: CurvePoint[], min: number, max: number, floor: number): CurvePoint[] {
    const first = points[0];
    const last = points[points.length - 1];
    if (!first || !last) return [];
    const all = [{ temp: min, pct: first.pct }, ...points, { temp: max, pct: last.pct }];
    const out: CurvePoint[] = [];

    all.forEach((p, i) => {
        const prev = all[i - 1];
        if (prev && (prev.pct - floor) * (p.pct - floor) < 0) {
            out.push({ temp: prev.temp + ((p.temp - prev.temp) * (floor - prev.pct)) / (p.pct - prev.pct), pct: floor });
        }
        out.push({ temp: p.temp, pct: Math.max(p.pct, floor) });
    });
    return out;
}

/** Expected rpm of a fan at a speed: linear between the measured points; below the lowest one only the measured 0 counts. null: not measured. */
export function rpmAt(rows: CalibRows | undefined, pct: number): number | null {
    const first = rows?.[0];
    const last = rows?.[rows.length - 1];
    if (!rows || !first || !last || !Number.isFinite(pct)) return null;
    if (pct < first[0]) return pct <= 0 && first[1] === 0 ? 0 : null;
    if (pct >= last[0]) return last[1];
    const i = rows.findIndex(r => r[0] > pct);
    const [p0, r0] = rows[i - 1] as [number, number];
    const [p1, r1] = rows[i] as [number, number];
    return Math.round(r0 + ((r1 - r0) * (pct - p0)) / (p1 - p0));
}

export function rpmTicks(rows: CalibRows | undefined): { rpm: number; pct: number }[] {
    if (!rows || rows.length < 2) return [];
    const lo = (rows[0] as [number, number])[1];
    const hi = (rows[rows.length - 1] as [number, number])[1];
    const step = [100, 250, 500, 1000].find(s => (hi - lo) / s <= 6) || 1000;
    const ticks: { rpm: number; pct: number }[] = [];

    for (let rpm = Math.ceil(lo / step) * step; rpm <= hi; rpm += step) {
        const i = rows.findIndex(r => r[1] >= rpm);
        if (i < 1) continue;
        const [p0, r0] = rows[i - 1] as [number, number];
        const [p1, r1] = rows[i] as [number, number];
        ticks.push({ rpm, pct: r1 === r0 ? p1 : p0 + ((p1 - p0) * (rpm - r0)) / (r1 - r0) });
    }
    return ticks;
}
