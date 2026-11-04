import { expect, test } from 'bun:test';
import { alpha, breath, describe, hsv } from './fx';

test('alpha maps brightness 0-255 to the opacity of a lit LED', () => {
    expect(alpha(0)).toBe(0);
    expect(alpha('x')).toBe(0);
    expect(alpha(255)).toBeCloseTo(1, 10);
    expect(alpha(1)).toBeGreaterThan(0.18);
    expect(alpha(128)).toBeGreaterThan(alpha(64));
});

test('breath goes from trough to peak', () => {
    expect(breath(0)).toBeCloseTo(0, 10);
    expect(breath(0.5)).toBeCloseTo(1, 10);
    expect(breath(1)).toBeCloseTo(0, 10);
});

test('hsv at full saturation', () => {
    expect(hsv(0)).toEqual([255, 0, 0]);
    expect(hsv(120)).toEqual([0, 255, 0]);
    expect(hsv(240)).toEqual([0, 0, 255]);
    expect(hsv(-120)).toEqual([0, 0, 255]);
    expect(hsv(360)).toEqual([255, 0, 0]);
});

test('describe: off, static, blink, busy flicker, breath', () => {
    expect(describe(undefined, 'disk1', undefined)).toEqual({ kind: 'off' });
    expect(describe({ color: '255 0 0', brightness: 0 }, 'disk1', undefined)).toEqual({ kind: 'off' });
    expect(describe({ color: '255 0 0', brightness: 255, effect: 'off' }, 'disk1', undefined)).toEqual({ kind: 'off' });
    expect(describe({ color: '1 2', brightness: 255 }, 'disk1', undefined)).toEqual({ kind: 'off' });

    const still = describe({ color: '0 255 0', brightness: 255, effect: 'none' }, 'disk1', undefined);
    expect(still).toMatchObject({ kind: 'static', rgb: [0, 255, 0] });

    expect(describe({ color: '0 255 0', brightness: 255, effect: 'blink', on_ms: 500, off_ms: 500 }, 'disk1', undefined)).toMatchObject({
        kind: 'blink',
        lit: 500,
        cycle: 1000
    });
    // 100/100 ms is the activity blink: drawn as a soft flicker, not as flashing
    expect(describe({ color: '0 255 0', brightness: 255, effect: 'blink', on_ms: 100, off_ms: 100 }, 'disk1', undefined)).toMatchObject({
        kind: 'busy',
        cycle: 334
    });
    expect(describe({ color: '0 255 0', brightness: 255, effect: 'breath', on_ms: 1200, off_ms: 900 }, 'power', undefined)).toMatchObject({
        kind: 'breath',
        cycle: 2100
    });
});

test('describe: the software rainbow offsets each LED of the effect', () => {
    const status = { fx: 'rainbow', fx_leds: 'disk1,disk2,disk3', fx_speed: 2, fx_reverse: false };
    const led = { color: '255 0 0', brightness: 255, effect: 'none' };

    expect(describe(led, 'disk2', status)).toMatchObject({ kind: 'rainbow', step: 250, offset: 120 });
    expect(describe(led, 'disk2', { ...status, fx_reverse: true })).toMatchObject({ kind: 'rainbow', offset: -120 });
    expect(describe(led, 'power', status)).toMatchObject({ kind: 'static' });
});
