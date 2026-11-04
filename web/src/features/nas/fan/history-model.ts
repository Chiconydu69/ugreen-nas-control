// Fan history: the shaping of the fan_history answer for the chart (pure, no DOM). The drawing is in history.ts.
import { MOD, type FanHistoryRange, type FanHistoryResponse, type FanHistorySeries } from '../../../core/contract-fan-history';

export const RANGE_SECONDS: Record<FanHistoryRange, number> = { '1h': 3600, '24h': 86400 };

/** How often a visible card asks again: as often as a new point arrives, at most every 10 s. */
export const REFRESH_SECONDS: Record<FanHistoryRange, number> = { '1h': 10, '24h': 60 };

export type SourceKind = 'cpu' | 'disk' | 'nvme' | 'gpu' | 'hba' | 'nic' | 'script' | 'idle' | 'emergency' | 'off' | 'other';

/** What a source is, for the colour of its band: "disk:sdb" is a disk, "emergency:cpu_hot" an emergency. */
export function sourceKind(source: string): SourceKind {
    const kind = source.split(':')[0] ?? '';
    switch (kind) {
        case 'cpu':
        case 'nvme':
        case 'gpu':
        case 'hba':
        case 'nic':
        case 'script':
        case 'emergency':
            return kind;
        case 'disk':
        case 'disk_avg':
        case 'disk_max':
            return 'disk';
        case 'idle':
            return 'idle';
        case 'bios':
        case 'rival':
        case 'calibration':
        case 'missing':
        case 'hold':
            return 'off';
        default:
            return 'other';
    }
}

/** The part after the colon of a source ("sdb", "eth0", "cpu_hot"), '' when it has none. */
export const sourceArg = (source: string): string => source.split(':').slice(1).join(':');

export interface Run {
    /** First and last point index of the run. */
    from: number;
    to: number;
    value: number;
}

/** Consecutive points with the same value; values the filter rejects end a run and start none. */
export function runs(values: number[], keep: (value: number) => boolean = () => true): Run[] {
    const out: Run[] = [];
    values.forEach((value, i) => {
        if (!keep(value)) return;
        const last = out[out.length - 1];
        if (last && last.to === i - 1 && last.value === value) last.to = i;
        else out.push({ from: i, to: i, value });
    });
    return out;
}

/** Runs of points that have a modifier bit set. */
export const maskRuns = (mods: number[], mask: number): Run[] =>
    runs(
        mods.map(m => m & mask),
        v => v !== 0
    ).map(r => ({ ...r, value: mask }));

export interface Pt {
    t: number;
    v: number;
}

/**
 * Polylines of a series: a missing value, or a hole longer than 2.5 steps (the daemon was not running), breaks the line, so
 * nothing is drawn across what was not recorded.
 */
export function lines(t: number[], values: (number | null)[], step: number): Pt[][] {
    const out: Pt[][] = [];
    let line: Pt[] = [];
    let last = Number.NEGATIVE_INFINITY;
    t.forEach((time, i) => {
        const v = values[i];
        if (v == null || time - last > step * 2.5) {
            if (line.length) out.push(line);
            line = [];
        }
        last = time;
        if (v != null) line.push({ t: time, v });
    });
    if (line.length) out.push(line);
    return out;
}

/** Index of the point nearest to a time, -1 when there are none. */
export function nearest(t: number[], at: number): number {
    if (!t.length) return -1;
    let lo = 0;
    let hi = t.length - 1;
    while (lo < hi) {
        const mid = (lo + hi) >> 1;
        if ((t[mid] as number) < at) lo = mid + 1;
        else hi = mid;
    }
    return lo > 0 && Math.abs((t[lo - 1] as number) - at) <= Math.abs((t[lo] as number) - at) ? lo - 1 : lo;
}

/** x axis: the window ends at the answer's time (rounded to a step), and starts one range earlier. */
export function domain(h: FanHistoryResponse, range: FanHistoryRange): [number, number] {
    const end = h.now ?? h.t?.[h.t.length - 1] ?? 0;
    return [end - RANGE_SECONDS[range], end];
}

/** Clock ticks of the x axis, on round times (Europe/Berlin hours and minutes are rounded in UTC offsets of whole hours). */
export function timeTicks(from: number, to: number, range: FanHistoryRange, offsetSeconds = 0): number[] {
    const every = range === '1h' ? 600 : 4 * 3600;
    const out: number[] = [];
    for (let t = Math.ceil((from + offsetSeconds) / every) * every - offsetSeconds; t <= to; t += every) out.push(t);
    return out;
}

/** Offset of Europe/Berlin from UTC at a time, in seconds (the page shows that time zone everywhere). */
export function berlinOffset(seconds: number): number {
    const parts = new Intl.DateTimeFormat('en-GB', {
        timeZone: 'Europe/Berlin',
        hourCycle: 'h23',
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    }).formatToParts(new Date(seconds * 1000));
    const get = (type: string): number => Number(parts.find(p => p.type === type)?.value);
    const local = Date.UTC(get('year'), get('month') - 1, get('day'), get('hour'), get('minute'), get('second')) / 1000;
    return Math.round(local - Math.floor(seconds));
}

const BERLIN_CLOCK = new Intl.DateTimeFormat('de-DE', { timeZone: 'Europe/Berlin', hourCycle: 'h23', hour: '2-digit', minute: '2-digit', second: '2-digit' });
const BERLIN_DAY = new Intl.DateTimeFormat('de-DE', { timeZone: 'Europe/Berlin', day: '2-digit', month: '2-digit' });

/** "14:32" or, with seconds, "14:32:10": 24 h, Europe/Berlin like every time on the page. */
export function clock(seconds: number, withSeconds = false): string {
    const text = BERLIN_CLOCK.format(new Date(seconds * 1000));
    return withSeconds ? text : text.slice(0, 5);
}

/** "30.10. 14:32" */
export const dayClock = (seconds: number, withSeconds = false): string => `${BERLIN_DAY.format(new Date(seconds * 1000))} ${clock(seconds, withSeconds)}`;

export interface FanTrack {
    id: string;
    role: string;
    series: FanHistorySeries;
}

/** The fans of an answer, with their series, in the order the daemon lists them (fan ids are unique). */
export function tracks(h: FanHistoryResponse): FanTrack[] {
    const series = new Map((h.series ?? []).map(s => [s.id, s]));
    return (h.fans ?? []).flatMap(f => {
        const s = series.get(f.id);
        return s ? [{ id: f.id, role: f.role, series: s }] : [];
    });
}

/** The modifier names set in `mods` that matter to a reader, in the order they apply in the daemon. */
export function modNames(mods: number): ('floor' | 'mover' | 'parity' | 'quiet' | 'shape' | 'missing' | 'emergency')[] {
    const out: ReturnType<typeof modNames> = [];
    if (mods & MOD.emergency) out.push('emergency');
    if (mods & MOD.missing) out.push('missing');
    if (mods & MOD.floor) out.push('floor');
    if (mods & MOD.mover) out.push('mover');
    if (mods & MOD.parity) out.push('parity');
    if (mods & MOD.quiet) out.push('quiet');
    if (mods & MOD.shape) out.push('shape');
    return out;
}

/** The value range of temperature lines: padded to whole 10 °C, at least 20 °C high so a flat line stays flat. */
export function tempRange(values: (number | null)[][]): [number, number] {
    const all = values.flat().filter((v): v is number => v != null);
    if (!all.length) return [20, 60];
    const lo = Math.floor((Math.min(...all) - 2) / 10) * 10;
    const hi = Math.max(Math.ceil((Math.max(...all) + 2) / 10) * 10, lo + 20);
    return [lo, hi];
}

/** Rows for the table view: about `count` evenly spread points, the newest last. */
export function sampleIndexes(length: number, count: number): number[] {
    if (length <= count) return Array.from({ length }, (_, i) => i);
    return Array.from({ length: count }, (_, i) => Math.round((i * (length - 1)) / (count - 1)));
}
