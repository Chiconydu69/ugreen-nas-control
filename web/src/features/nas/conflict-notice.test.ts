import { expect, test } from 'bun:test';
import { createFakeDom } from '../../testing/fakedom';
import { answer, status } from '../../testing/fixtures';

const fresh = '?conflict';
const key = '#unc #unc-led-blocker';

// `ugreen-nas-ctl led-status` with ugreenleds-driver installed: the LEDs are off, the blocker is "driver".
const blocked = { enabled: false, supported: true, blocker: 'driver', warning: '', untested: false, import_available: false, model: 'DXP6800 Pro' };

test('the LED conflict notice stays through every poll and clears when the conflict is gone', async () => {
    const original = status.led;
    status.led = { ...blocked };

    const dom = createFakeDom(['unc'], url => answer(url));
    const polls: (() => unknown)[] = [];
    dom.install({
        UNCPoll: {
            awake: () => true,
            setIdle: () => undefined,
            invalidate: () => undefined,
            lastPhaseRx: null,
            fetchText: async (url: string) => ({ ok: true, status: 200, text: JSON.stringify(answer(url)), rx: 0 }),
            json: async (url: string) => answer(url),
            every: (fn: () => unknown) => (polls.push(fn), { poke: () => undefined })
        },
        UNCLedFx: { stamp: (leds: unknown) => leds, paint: () => undefined },
        UGREEN_NAS_THEME: 'dark',
        UGREEN_NAS_LANG: 'en',
        UGREEN_NAS_VERSION: '3.5',
        UGREEN_LED_LANG: 'en',
        UNC_LED: true
    });

    try {
        await import(`./i18n${fresh}`);
        await import(`./app${fresh}`);
        await new Promise(resolve => setTimeout(resolve, 100));

        const shown = () => !dom.props.get(`${key}.hidden`);
        const text = () => String(dom.props.get(`${key}.textContent`));

        expect(shown()).toBe(true);
        expect(text()).toContain('ugreenleds-driver');

        for (let i = 0; i < 3; i++) {
            await polls[0]?.();
            expect(shown()).toBe(true);
            expect(text()).toContain('ugreenleds-driver');
        }

        status.led = { ...blocked, blocker: '' };
        await polls[0]?.();
        expect(shown()).toBe(false);
    } finally {
        dom.restore();
        status.led = original;
    }
});
