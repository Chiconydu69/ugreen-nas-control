import { expect, test } from 'bun:test';
import type { StatusResponse } from '../../../core/contract';
import type { FanCaps } from '../../../core/contract-fan-probe';
import { backlightView, DEFAULT_SLIDER_PCT } from './backlight';
import { confirmFans, cs201xVisible, isCs201xModel } from './cs201x';
import { beeperVisible, buttonsVisible } from './panel';
import { probeVisible } from './probe';

const status = (model: string, caps?: Partial<FanCaps>): StatusResponse =>
    ({ ok: true, info: { model }, fan_caps: caps && { kind: 'fixed', channels: 0, watchdog: false, backlight: false, ...caps } }) as StatusResponse;

test('the CS201x card replaces the IT8613E probe on the DXP2800 GT only', () => {
    const gt = status('DXP2800 GT', { kind: 'probe' });
    const it8613 = status('DXP4800 Plus', { kind: 'probe' });
    expect([cs201xVisible(gt), probeVisible(gt)]).toEqual([true, false]);
    expect([cs201xVisible(it8613), probeVisible(it8613)]).toEqual([false, true]);
    expect(cs201xVisible(status('DXP2800 GT', { kind: 'fixed' }))).toBe(false);
    expect(cs201xVisible(status('DXP2800 GT'))).toBe(false);
    expect([isCs201xModel('DXP2800 GT'), isCs201xModel('DXP2800'), isCs201xModel(undefined)]).toEqual([true, false, false]);
});

test('confirm names the fans unless both are chosen', () => {
    expect(confirmFans(['fan1', 'fan3'])).toBeUndefined();
    expect(confirmFans(['fan3', 'fan1'])).toBeUndefined();
    expect(confirmFans(['fan1'])).toBe('fan1');
    expect(confirmFans(['fan3'])).toBe('fan3');
    expect(confirmFans([])).toBe('');
    expect(confirmFans(['fan2', 'fan3'])).toBe('fan3');
});

test('the panel cards show for the models the backend knows', () => {
    expect([buttonsVisible(status('DXP4800 Plus')), beeperVisible(status('DXP4800 Plus'))]).toEqual([true, true]);
    expect([buttonsVisible(status('DXP4800 GT')), beeperVisible(status('DXP4800 GT'))]).toEqual([true, false]);
    expect([buttonsVisible(status('DXP6800 Ultra')), beeperVisible(status('DXP6800 Ultra'))]).toEqual([false, false]);
    expect([buttonsVisible(status('')), beeperVisible(status(''))]).toEqual([false, false]);
});

test('the backlight is a number or the string "default"', () => {
    expect(backlightView(40)).toEqual({ value: 40, isDefault: false });
    expect(backlightView(0)).toEqual({ value: 0, isDefault: false });
    expect(backlightView(100)).toEqual({ value: 100, isDefault: false });
    expect(backlightView('default')).toEqual({ value: DEFAULT_SLIDER_PCT, isDefault: true });
    expect(backlightView('55')).toEqual({ value: 55, isDefault: false });
    expect(backlightView('garbage')).toEqual({ value: DEFAULT_SLIDER_PCT, isDefault: true });
    expect(backlightView(130)).toEqual({ value: 100, isDefault: false });
});
