import { expect, test } from 'bun:test';
import { join } from 'node:path';
import { clamp, RANGES, validValue } from './fields';

interface Case {
    key: string;
    value: string;
    ok: boolean;
}

// The UI never sends what the backend refuses: its checks agree with the Go table (curves are the curve editor's).
test('validValue agrees with the backend validation table', async () => {
    const cases: Case[] = await Bun.file(join(import.meta.dir, '../../../../../backend/internal/fan/testdata/v2_validation.json')).json();
    const wrong = cases
        .filter(c => !c.key.endsWith('curve'))
        .filter(c => validValue(c.key, c.value) !== c.ok)
        .map(c => `${c.key}=${JSON.stringify(c.value)} want ${c.ok}`);
    expect(wrong).toEqual([]);
});

test('every range end is accepted and one step outside is refused', () => {
    for (const [field, [lo, hi]] of Object.entries(RANGES)) {
        const key = field.startsWith('job_') || field.startsWith('quiet_') ? field : `cpu_${field}`;
        expect([validValue(key, String(lo)), validValue(key, String(hi)), validValue(key, String(lo - 1)), validValue(key, String(hi + 1))]).toEqual([
            true,
            true,
            false,
            false
        ]);
    }
    expect(clamp(250, RANGES.start_pct)).toBe(100);
    expect(clamp(0, RANGES.min_run)).toBe(10);
});
