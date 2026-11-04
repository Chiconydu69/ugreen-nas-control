// Shared by the inputs, shaping and schedule modules: the value ranges the backend accepts (mirrors
// web/plugin/include/fan_v2.php and backend/internal/fan/config_v2.go; fields.test.ts runs them against
// backend/internal/fan/testdata/v2_validation.json) and the markup of a select.
import { escapeHtml } from '../../../core/html';

export type Range = readonly [lo: number, hi: number];

export const RANGES = {
    deadband: [0, 10],
    start_pct: [0, 100],
    start_ms: [0, 10000],
    min_run: [10, 100],
    up_step: [1, 100],
    down_step: [1, 100],
    quiet_cap: [10, 100],
    job_mover_pct: [1, 100],
    job_parity_pct: [1, 100]
} as const satisfies Record<string, Range>;

export const ON_MISSING = ['fallback', 'curve_low', 'hold'] as const;
export const JOB_MODES = ['off', 'floor', 'boost'] as const;
export const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const;

const DRIVE = '[A-Za-z0-9_.-]{1,40}';
const PART = new RegExp(
    `^(?:cpu|nvme|gpu|hba|disk_max|disk_avg|disk_(?:max|avg):${DRIVE}(?:\\+${DRIVE}){0,31}|nic:[A-Za-z0-9_.@-]{1,15}|script:[a-z0-9_-]{1,32})$`
);

export const validSpec = (spec: string): boolean => {
    if (spec.length > 256) return false;
    if (spec === '' || spec === 'auto') return true;
    if (!spec.startsWith('max:')) return PART.test(spec);
    const parts = spec.slice(4).split('|');
    return parts.length >= 2 && parts.length <= 8 && parts.every(part => PART.test(part));
};

const inRange = (value: string, [lo, hi]: Range): boolean => /^[+-]?\d{1,9}$/.test(value) && Number(value) >= lo && Number(value) <= hi;

/** Is `value` accepted for the v2 setting `key` (group keys cpu_*, case_*, fan.<id>.* and the schedule keys)? Curves are not handled here. */
export function validValue(key: string, value: string): boolean {
    const group = /^(?:fan\.fan[1-4]\.|(?:cpu|case)_)([a-z_]+)$/.exec(key)?.[1];
    switch (group ?? key) {
        case 'input':
            return validSpec(value);
        case 'deadband':
        case 'start_pct':
        case 'start_ms':
        case 'min_run':
        case 'up_step':
        case 'down_step':
        case 'quiet_cap':
        case 'job_mover_pct':
        case 'job_parity_pct':
            return inRange(value, RANGES[(group ?? key) as keyof typeof RANGES]);
        case 'on_missing':
            return (ON_MISSING as readonly string[]).includes(value);
        case 'allow_off':
        case 'quiet_enabled':
            return value === '0' || value === '1';
        case 'quiet_from':
        case 'quiet_to':
            return /^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(value);
        case 'quiet_days':
            return value.split(',').every(day => (DAYS as readonly string[]).includes(day));
        case 'quiet_scope':
            return value === 'case' || value === 'all';
        case 'job_mover':
        case 'job_parity':
            return (JOB_MODES as readonly string[]).includes(value);
        default:
            return false;
    }
}

export const clamp = (value: number, [lo, hi]: Range): number => Math.min(hi, Math.max(lo, Math.round(value)));

export interface Choice {
    value: string;
    label: string;
    disabled?: boolean;
}

export const selectHtml = (attrs: string, choices: Choice[], current: string): string =>
    `<select ${attrs}>${choices
        .map(
            c =>
                `<option value="${escapeHtml(c.value)}"${c.value === current ? ' selected' : ''}${c.disabled ? ' disabled' : ''}>${escapeHtml(c.label)}</option>`
        )
        .join('')}</select>`;

/** The entries of `next` that differ from `base`: what a save has to send. With `touched`, only the keys the user edited: a stale draft never reverts what changed elsewhere since. */
export function changes(next: Record<string, string>, base: Record<string, string>, touched?: ReadonlySet<string>): Record<string, string> {
    return Object.fromEntries(Object.entries(next).filter(([key, value]) => base[key] !== value && (!touched || touched.has(key))));
}

export function v2Params(values: Record<string, string>): { [field: `values[${string}]`]: string } {
    const params: { [field: `values[${string}]`]: string } = {};
    for (const [key, value] of Object.entries(values)) params[`values[${key}]`] = value;
    return params;
}
