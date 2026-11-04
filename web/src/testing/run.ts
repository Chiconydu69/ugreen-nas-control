// Runs a built page script (or a source module) in the fake DOM and returns what it wrote.
import { createFakeDom } from './fakedom';
import { answer } from './fixtures';

export async function runPage(rootId: string, load: () => Promise<unknown>, globals: Record<string, unknown> = {}): Promise<string[]> {
    const dom = createFakeDom([rootId, 'unc-dash', 'unc-dash-state', 'unc-dash-settings'], url => answer(url));
    const calls: string[] = [];
    const poll = {
        awake: () => true,
        setIdle: () => undefined,
        invalidate: () => undefined,
        lastPhaseRx: null,
        fetchText: async (url: string) => ({ ok: true, status: 200, text: JSON.stringify(answer(url)), rx: 0 }),
        json: async (url: string) => answer(url),
        every: (_fn: unknown, ms: unknown) => {
            calls.push(`every ${typeof ms === 'function' ? 'dynamic' : ms}`);
            return { poke: () => undefined };
        }
    };
    const ledfx = {
        stamp: (leds: unknown) => leds,
        paint: (_el: unknown, led: { color?: string; effect?: string } | undefined, name: string | undefined) =>
            void dom.log.push(`paint ${name} ${led?.color} ${led?.effect}`)
    };

    dom.install({
        UNCPoll: poll,
        UNCLedFx: ledfx,
        UGREEN_NAS_THEME: 'dark',
        UGREEN_NAS_LANG: 'en',
        UGREEN_NAS_VERSION: '3.5',
        UGREEN_LED_LANG: 'en',
        UNC_LED: true,
        ...globals
    });
    try {
        await load();
        await new Promise(resolve => setTimeout(resolve, 100));
    } finally {
        dom.restore();
    }

    return [...dom.log, ...calls];
}
