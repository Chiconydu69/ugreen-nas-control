// Fan history: the SVG of one fan (pure string building, no DOM). Four rows share the time axis:
//   lane      which input drove the speed (colour bands, short label inside)
//   limits    what changed the speed after the curve (quiet hours cap, mover / parity raise, minimum speed)
//   speed     duty in %
//   rpm       measured speed
//   temp      the input that drove the fan, plus the CPU and the hottest drive for reference
// Emergencies shade all rows red. Drawn 1:1 at the card's width so the text keeps its size.
import { MOD, type FanHistoryRange, type FanHistoryResponse } from '../../../core/contract-fan-history';
import { escapeHtml } from '../../../core/html';
import { berlinOffset, clock, domain, lines, maskRuns, runs, sourceKind, tempRange, timeTicks, type FanTrack, type Pt } from './history-model';

export interface Geometry {
    W: number;
    H: number;
    L: number;
    R: number;
    rows: { lane: [number, number]; mods: [number, number]; speed: [number, number]; rpm: [number, number]; temp: [number, number] };
    x(t: number): number;
    /** The time at an x position inside the plot. */
    time(x: number): number;
}

const LANE: [number, number] = [4, 22];
const MODS: [number, number] = [25, 37];
const SPEED: [number, number] = [58, 124];
const RPM: [number, number] = [148, 188];
const TEMP: [number, number] = [212, 276];

export function geometry(W: number, from: number, to: number): Geometry {
    const L = 40;
    const R = 10;
    const span = Math.max(1, to - from);
    return {
        W,
        H: 302,
        L,
        R,
        rows: { lane: LANE, mods: MODS, speed: SPEED, rpm: RPM, temp: TEMP },
        x: t => L + ((Math.min(to, Math.max(from, t)) - from) / span) * (W - L - R),
        time: x => from + ((x - L) / (W - L - R)) * span
    };
}

export interface ChartText {
    speed: string;
    rpm: string;
    temp: string;
    /** Short text for a band: "CPU", "disk2". */
    short(source: string): string;
}

const num = (n: number): string => (Math.round(n * 10) / 10).toString();

function path(points: Pt[], x: (t: number) => number, y: (v: number) => number): string {
    return points.map(p => `${num(x(p.t))},${num(y(p.v))}`).join(' ');
}

function polylines(parts: Pt[][], cls: string, x: (t: number) => number, y: (v: number) => number): string {
    return parts
        .map(part =>
            part.length === 1
                ? `<circle class="${cls} dot" cx="${num(x((part[0] as Pt).t))}" cy="${num(y((part[0] as Pt).v))}" r="1.5"/>`
                : `<polyline class="${cls}" points="${path(part, x, y)}"/>`
        )
        .join('');
}

/** The SVG body (inside <svg viewBox>) of one fan. */
export function fanChart(track: FanTrack, h: FanHistoryResponse, range: FanHistoryRange, g: Geometry, text: ChartText): string {
    const t = h.t ?? [];
    const step = h.step ?? 10;
    const [from, to] = domain(h, range);
    const { series } = track;
    const drivers = h.drivers ?? [];
    const { L, W, R } = g;
    const right = W - R;
    const bottom = g.rows.temp[1];

    // Row scales: value -> y
    const row =
        ([a, b]: [number, number], lo: number, hi: number) =>
        (v: number) =>
            b - ((v - lo) / (hi - lo)) * (b - a);
    const ySpeed = row(g.rows.speed, 0, 100);
    const rpmTop = Math.max(500, Math.ceil(Math.max(0, ...series.rpm.map(v => v ?? 0)) / 500) * 500);
    const yRpm = row(g.rows.rpm, 0, rpmTop);
    const [tLo, tHi] = tempRange([series.temp, h.cpu ?? [], h.case ?? []]);
    const yTemp = row(g.rows.temp, tLo, tHi);

    let out = '';

    // Time grid and labels
    const ticks = timeTicks(from, to, range, berlinOffset(to));
    for (const tick of ticks) {
        const x = g.x(tick);
        out += `<line class="vgrid" x1="${num(x)}" x2="${num(x)}" y1="${g.rows.speed[0]}" y2="${bottom}"/>`;
        out += `<text class="axis" x="${num(x)}" y="${bottom + 16}" text-anchor="middle">${clock(tick)}</text>`;
    }

    // Row grids and axes
    for (const pct of [0, 50, 100]) {
        out += `<line class="grid" x1="${L}" x2="${right}" y1="${ySpeed(pct)}" y2="${ySpeed(pct)}"/>`;
        out += `<text class="axis" x="${L - 6}" y="${ySpeed(pct) + 3.5}" text-anchor="end">${pct}</text>`;
    }
    for (const rpm of [0, rpmTop]) {
        out += `<line class="grid" x1="${L}" x2="${right}" y1="${yRpm(rpm)}" y2="${yRpm(rpm)}"/>`;
        out += `<text class="axis" x="${L - 6}" y="${yRpm(rpm) + 3.5}" text-anchor="end">${rpm}</text>`;
    }
    for (const deg of [tLo, (tLo + tHi) / 2, tHi]) {
        out += `<line class="grid" x1="${L}" x2="${right}" y1="${yTemp(deg)}" y2="${yTemp(deg)}"/>`;
        out += `<text class="axis" x="${L - 6}" y="${yTemp(deg) + 3.5}" text-anchor="end">${num(deg)}</text>`;
    }
    out += `<text class="rowtitle" x="${L}" y="${g.rows.speed[0] - 7}">${escapeHtml(text.speed)}</text>`;
    out += `<text class="rowtitle" x="${L}" y="${g.rows.rpm[0] - 7}">${escapeHtml(text.rpm)}</text>`;
    out += `<text class="rowtitle" x="${L}" y="${g.rows.temp[0] - 7}">${escapeHtml(text.temp)}</text>`;

    // Emergency: all rows
    for (const r of maskRuns(series.mods, MOD.emergency)) {
        const x0 = g.x(t[r.from] as number);
        const x1 = g.x((t[r.to] as number) + step);
        out += `<rect class="emergency" x="${num(x0)}" y="${g.rows.lane[0]}" width="${num(Math.max(2, x1 - x0))}" height="${bottom - g.rows.lane[0]}"/>`;
    }

    // Lane: the source of each stretch
    const [la, lb] = g.rows.lane;
    for (const r of runs(series.drv, v => v >= 0)) {
        const source = drivers[r.value] ?? 'other';
        const x0 = g.x(t[r.from] as number);
        const x1 = g.x((t[r.to] as number) + step);
        const w = Math.max(1.5, x1 - x0);
        const label = text.short(source);
        out += `<rect class="band k-${sourceKind(source)}" x="${num(x0)}" y="${la}" width="${num(w)}" height="${lb - la}" rx="2"/>`;
        if (w > label.length * 6.4 + 10) {
            out += `<text class="bandtext" x="${num(x0 + w / 2)}" y="${(la + lb) / 2 + 3.5}" text-anchor="middle">${escapeHtml(label)}</text>`;
        }
    }

    // Limits strip: one thin segment per modifier, side by side in the strip's height
    const [ma, mb] = g.rows.mods;
    const strips: [number, string][] = [
        [MOD.quiet, 'quiet'],
        [MOD.mover | MOD.parity, 'job'],
        [MOD.floor, 'floor']
    ];
    const sh = (mb - ma) / strips.length;
    strips.forEach(([mask, cls], i) => {
        for (const r of maskRuns(series.mods, mask)) {
            const x0 = g.x(t[r.from] as number);
            const x1 = g.x((t[r.to] as number) + step);
            out += `<rect class="mod m-${cls}" x="${num(x0)}" y="${num(ma + i * sh)}" width="${num(Math.max(1.5, x1 - x0))}" height="${num(sh - 0.6)}" rx="1"/>`;
        }
    });

    // Speed, rpm, temperatures
    const pct = lines(t, series.pct, step);
    for (const part of pct) {
        if (part.length > 1) {
            const area = `${num(g.x((part[0] as Pt).t))},${ySpeed(0)} ${path(part, g.x, ySpeed)} ${num(g.x((part[part.length - 1] as Pt).t))},${ySpeed(0)}`;
            out += `<polygon class="area" points="${area}"/>`;
        }
    }
    out += polylines(pct, 'duty', g.x, ySpeed);
    out += polylines(lines(t, series.rpm, step), 'rpmline', g.x, yRpm);
    out += polylines(lines(t, h.cpu ?? [], step), 'ref cpu', g.x, yTemp);
    out += polylines(lines(t, h.case ?? [], step), 'ref disk', g.x, yTemp);
    out += polylines(lines(t, series.temp, step), 'drive', g.x, yTemp);

    out += `<line class="cross" data-cross x1="0" x2="0" y1="${g.rows.lane[0]}" y2="${bottom}" hidden/>`;
    return out;
}
