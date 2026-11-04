import { expect, test } from 'bun:test';
import { hardware, ledGet, status } from '../testing/fixtures';
import { isHardwareResponse, isLedAllResponse, isLedStatusResponse, isStaleBundle, isStatusResponse } from './guard';

test('the fixtures have the shapes the guards expect', () => {
    expect(isStatusResponse(status)).toBe(true);
    expect(isHardwareResponse(hardware)).toBe(true);
    expect(isLedStatusResponse(ledGet.status)).toBe(true);
    expect(isLedAllResponse(ledGet.all)).toBe(true);
});

test('a renamed or missing top-level field fails', () => {
    const { settings, ...withoutSettings } = status;

    expect(settings).toBeDefined();
    expect(isStatusResponse(withoutSettings)).toBe(false);
    expect(isStatusResponse({ ...status, settings: { mode: 'quiet' } })).toBe(false);
    expect(isStatusResponse(null)).toBe(false);
    expect(isStatusResponse([])).toBe(false);
    expect(isHardwareResponse({ ...hardware, net: undefined })).toBe(true);
    expect(isHardwareResponse({ ok: true })).toBe(false);
    expect(isLedAllResponse({ ok: true })).toBe(false);
});

test('the optional fan parts are checked when present', () => {
    const caps = { kind: 'fixed', channels: 2, watchdog: true, backlight: false };
    const wdt = { armed: true, timeout: 300, interval: 60, last_pet: 1, time: 2, state: 'armed' };

    expect(isStatusResponse({ ...status, fan_caps: caps, watchdog: wdt })).toBe(true);
    expect(isStatusResponse({ ...status, watchdog: { ...wdt, armed: false, state: 'not_counting' } })).toBe(true);
    expect(isStatusResponse({ ...status, watchdog: null })).toBe(true);
    expect(isStatusResponse({ ...status, fan_caps: { kind: 'fixed' } })).toBe(false);
    expect(isStatusResponse({ ...status, watchdog: { ...wdt, state: 'fed' } })).toBe(false);
    expect(isStatusResponse({ ...status, settings: { ...status.settings, fan_config: {} } })).toBe(false);
});

test('a page of another plugin version than its backend is stale', () => {
    expect(isStaleBundle('3.5', '3.6')).toBe(true);
    expect(isStaleBundle('3.6', '3.6')).toBe(false);
    // No version on either side (backend before 3.6, harness): never stale.
    expect(isStaleBundle('3.5', undefined)).toBe(false);
    expect(isStaleBundle('3.5', '')).toBe(false);
    expect(isStaleBundle(undefined, '3.6')).toBe(false);
});
