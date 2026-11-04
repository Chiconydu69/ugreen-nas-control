import { expect, test } from 'bun:test';
import type { StatusResponse } from '../../../core/contract';
import type { FanCaps, ProbeOutput } from '../../../core/contract-fan-probe';
import { backlightVisible } from './backlight';
import { chosenRoles, probeVisible } from './probe';
import * as ui from '../ui/components';
import type { FanCtx } from './module';
import { isPreview, miniCurve, previewView, profilesVisible, sourceRow } from './profiles';
import { watchdogVisible } from './watchdog';

const status = (fan_caps?: Partial<FanCaps>): StatusResponse =>
    ({ ok: true, fan_caps: fan_caps && { kind: 'fixed', channels: 2, watchdog: false, backlight: false, ...fan_caps } }) as StatusResponse;

test('every section shows only from fan_caps', () => {
    expect([watchdogVisible, backlightVisible, probeVisible, profilesVisible].map(f => f(status()))).toEqual([false, false, false, false]);
    expect(watchdogVisible(status({ watchdog: true }))).toBe(true);
    expect(backlightVisible(status({ backlight: true }))).toBe(true);
    expect(probeVisible(status({ kind: 'probe' }))).toBe(true);
    expect(probeVisible(status({ kind: 'fixed' }))).toBe(false);
    expect(profilesVisible(status({}))).toBe(true);
});

test('only mapped outputs with a role are confirmed', () => {
    const outputs: ProbeOutput[] = [
        { pwm: 'pwm1', status: 'mapped', tach: 'fan1', base_rpm: 500, peak_rpm: 1500 },
        { pwm: 'pwm2', status: 'no_fan', base_rpm: 0, peak_rpm: 0 },
        { pwm: 'pwm3', status: 'mapped', tach: 'fan3', base_rpm: 500, peak_rpm: 1500 }
    ];
    expect(chosenRoles(outputs, { pwm1: 'cpu', pwm2: 'case', pwm3: '' })).toEqual([{ pwm: 'pwm1', role: 'cpu' }]);
});

test('the mini curve draws one line per curve and nothing without points', () => {
    const svg = miniCurve([
        {
            cls: 'cpu',
            points: [
                { temp: 40, pct: 30 },
                { temp: 70, pct: 100 }
            ]
        }
    ]);
    expect(svg.match(/<polyline/g)?.length).toBe(1);
    expect(miniCurve([])).toBe('');
});

test('a preview answer is checked before it is shown', () => {
    expect(isPreview({ ok: true })).toBe(false);
    expect(isPreview({ token: 't', title: 'x', diff: [], warnings: [], not_imported: [], profile: {} })).toBe(true);
});

test('text from a profile or a tool never reaches the markup unescaped', () => {
    const evil = '<img src=x onerror=alert(1)>';
    const ctx = { ui, t: (key: string, vars?: Record<string, unknown>) => `${key} ${Object.values(vars ?? {}).join(' ')}` } as unknown as FanCtx;
    const html = previewView(ctx, {
        ok: true,
        source: 'dynamix',
        title: evil,
        profile: { cpu_curve: '40:30,70:100', case_curve: evil },
        diff: [{ key: evil, from: evil, to: evil }],
        warnings: [evil],
        not_imported: [{ key: evil, reason: evil }],
        token: 't',
        unchanged: 1,
        provenance: {},
        v2: false
    });
    const row = sourceRow(ctx, { id: 'itkuny', name: evil, files: [evil], path: evil, running: true });
    for (const markup of [html, row]) {
        expect(markup).not.toContain('<img');
        expect(markup).toContain('&lt;img');
    }
});
