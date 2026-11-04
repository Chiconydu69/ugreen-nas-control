import { expect, test } from 'bun:test';
import type { FanConfigV2 } from '../../../core/contract';
import { changes, validValue } from './fields';
import { draftOf, problem, valuesOf } from './schedule';

const cfg = {
    quiet: { enabled: true, from: '23:00', to: '07:00', days: 'sat,mon', cap: 40, scope: 'case' },
    mover: { mode: 'floor', pct: 60 },
    parity: { mode: 'off', pct: 50 }
} as unknown as FanConfigV2;

test('the draft keeps weekday order and saves values the backend accepts', () => {
    const values = valuesOf(draftOf(cfg));
    expect(values.quiet_days).toBe('mon,sat');
    for (const [key, value] of Object.entries(values)) expect([key, validValue(key, value)]).toEqual([key, true]);
});

test('quiet hours that never start, or on no day, are not sent', () => {
    const d = draftOf(cfg);
    expect(problem(d)).toBe('');
    expect(problem({ ...d, to: d.from })).toBe('schedule.err_same');
    expect(problem({ ...d, enabled: false, to: d.from })).toBe('');
    expect(problem({ ...d, days: [] })).toBe('schedule.err_days');
});

test('a profile applied while an edit is unsaved: the save sends only the edited key', () => {
    const draft = draftOf(cfg);
    draft.cap = 55;
    const applied = {
        ...cfg,
        quiet: { ...cfg.quiet, from: '22:00', scope: 'all' },
        mover: { mode: 'boost', pct: 80 }
    } as unknown as FanConfigV2;
    const stale = changes(valuesOf(draft), valuesOf(draftOf(applied)));
    expect(Object.keys(stale).sort()).toEqual(['job_mover', 'job_mover_pct', 'quiet_cap', 'quiet_from', 'quiet_scope']);
    expect(changes(valuesOf(draft), valuesOf(draftOf(applied)), new Set(['quiet_cap']))).toEqual({ quiet_cap: '55' });
});
