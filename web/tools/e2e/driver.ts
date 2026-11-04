// One browser tab plus one harness: the helpers the scenarios are written with, and the record of every step
// (API requests, console errors, toasts, DOM dump) the runner compares.
import type { ApiRequest, Harness } from '../harness/serve';

export interface Event {
    kind: string;
    text: string;
}

export interface StepRecord {
    name: string;
    requests: ApiRequest[];
    events: Event[];
    dom: string;
    text: string;
}

export const VIEWPORT = { width: 1440, height: 900 };
const SETTLE_MS = 20000;

export class Session {
    steps: StepRecord[] = [];
    constructor(
        readonly view: Bun.WebView,
        readonly h: Harness
    ) {}

    eval<T = unknown>(expr: string): Promise<T> {
        return this.view.evaluate(expr) as Promise<T>;
    }

    private async ready(dash: boolean): Promise<void> {
        const probe = dash
            ? `Boolean(window.__e2e && document.querySelector('table td') && !/…$/.test(document.body.textContent))`
            : `Boolean(window.__e2e && document.querySelector('#unc-online') && !/…$/.test(document.querySelector('#unc-online').textContent) && document.querySelector('.unc-nav [data-nav="led:overview"]'))`;
        await this.until(probe, 'page ready', 30000);
        await this.tick();
        await this.tick(); // the LED pages register their polls late
    }

    private async tick(): Promise<void> {
        await this.settle(300);
        await this.eval(`window.__e2e.tick()`);
        await this.settle();
    }

    async until(expr: string, what: string, ms = 5000): Promise<void> {
        const end = Date.now() + ms;
        for (;;) {
            try {
                if (await this.eval<boolean>(expr)) return;
            } catch {
                /* mid-navigation */
            }
            if (Date.now() > end) throw new Error(`timeout waiting for ${what}`);
            await Bun.sleep(25);
        }
    }

    /**
     * Idle = no fetch in flight, none finished within `quiet` ms, document loaded. The limit is longer than the
     * page's own 15 s abort of a hung poll request (core/ledfx/poll.ts), so a request that stalls once does not fail the run.
     */
    async settle(quiet = 120): Promise<void> {
        const end = Date.now() + SETTLE_MS;
        let stable = 0;
        let last = 'no answer (navigating?)';
        while (stable < 2) {
            let idle = false;
            try {
                const state = await this.eval<string>(
                    `[window.__e2e.pending, Math.round(window.__e2e.now() - window.__e2e.lastIo), document.readyState].join(' ')`
                );
                const [pending, since, ready] = state.split(' ');
                idle = pending === '0' && Number(since) >= quiet && ready === 'complete';
                last = `${pending} fetches in flight, ${since} ms since the last, readyState ${ready}`;
            } catch {
                /* navigating */
            }
            stable = idle ? stable + 1 : 0;
            if (Date.now() > end) throw new Error(`page never settled (${last})`);
            await Bun.sleep(25);
        }
    }

    async open(opts: { dash?: boolean } = {}): Promise<void> {
        await this.view.navigate(this.h.url(opts.dash ? { dash: true } : {}));
        await this.ready(Boolean(opts.dash));
        await this.settle();
    }

    private async where(selector: string, fx = 0.5): Promise<{ x: number; y: number }> {
        const find = `(() => {
            let el = document.querySelector(${JSON.stringify(selector)});
            if (el instanceof HTMLInputElement && (el.type === 'checkbox' || el.type === 'radio')) el = el.closest('label') || el;
            return el;
        })()`;
        // The view applies a scroll a frame later: wait for it before measuring.
        const found = await this.eval<boolean>(`new Promise(done => {
            const el = ${find};
            if (!el) return done(false);
            // A floating save bar is fixed: scroll to the place it has in the card, where it is not floating.
            (el.closest('.unc-savedock') || el).scrollIntoView({ block: 'center', inline: 'center' });
            requestAnimationFrame(() => requestAnimationFrame(() => done(true)));
        })`);
        if (!found) throw new Error(`no element ${selector}`);
        // A floating save bar moves when the page scrolls: measure until the position holds for two frames.
        const at = await this.eval<{ x: number; y: number }>(`new Promise(done => {
            const measure = () => {
                const r = ${find}.getBoundingClientRect();
                return { x: r.left + r.width * ${fx}, y: r.top + r.height / 2 };
            };
            let last = measure();
            let left = 20;
            const next = () => requestAnimationFrame(() => requestAnimationFrame(() => {
                const now = measure();
                if ((now.x === last.x && now.y === last.y) || --left === 0) return done(now);
                last = now;
                next();
            }));
            next();
        })`);
        return at;
    }

    async click(selector: string, fx = 0.5): Promise<void> {
        const { x, y } = await this.where(selector, fx);
        await this.view.click(Math.round(x), Math.round(y));
    }

    go(nav: string): Promise<void> {
        return this.click(`.unc-nav [data-nav="${nav}"]`);
    }

    async set(selector: string, value: string | boolean, events: string[] = ['input', 'change']): Promise<void> {
        const ok = await this.eval<boolean>(`(() => {
            const el = document.querySelector(${JSON.stringify(selector)});
            if (!el) return false;
            const proto = el instanceof HTMLSelectElement ? HTMLSelectElement.prototype : el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
            const prop = typeof ${JSON.stringify(value)} === 'boolean' ? 'checked' : 'value';
            Object.getOwnPropertyDescriptor(proto, prop).set.call(el, ${JSON.stringify(value)});
            for (const type of ${JSON.stringify(events)}) el.dispatchEvent(new Event(type, { bubbles: true }));
            return true;
        })()`);
        if (!ok) throw new Error(`no element ${selector}`);
    }

    async type(selector: string, text: string): Promise<void> {
        await this.click(selector);
        await this.eval(`document.querySelector(${JSON.stringify(selector)}).select()`);
        await this.view.type(text);
        await this.eval(`document.activeElement.blur()`);
    }

    /** A key press on the focused element. Synthetic: the WebKit view does not deliver named keys (arrows, Home). */
    async key(key: string): Promise<void> {
        await this.eval(`document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: ${JSON.stringify(key)}, bubbles: true, cancelable: true }))`);
    }

    exists(selector: string): Promise<boolean> {
        return this.eval<boolean>(`Boolean(document.querySelector(${JSON.stringify(selector)}))`);
    }

    navs(): Promise<string[]> {
        return this.eval<string[]>(
            `[...document.querySelectorAll('.unc-nav [data-nav]')].filter(b => !b.hidden && b.getClientRects().length).map(b => b.dataset.nav)`
        );
    }

    async advanceClock(ms: number): Promise<void> {
        await this.eval(`window.__e2e.advance(${ms})`);
        await this.tick();
    }

    async step(name: string, action?: () => Promise<void>, opts: { reload?: boolean; quiet?: number } = {}): Promise<void> {
        if (opts.reload) await this.eval(`window.__e2e.marker = 'old'`);
        await action?.();
        if (opts.reload) {
            await this.until(`window.__e2e && window.__e2e.marker === true`, 'reload', 15000);
            await this.ready(false);
        }
        await this.settle(opts.quiet);
        // Layout work (chart redraws on resize) lands a frame later.
        await this.eval(`new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))`);
        const events = await this.eval<Event[]>(`window.__e2e.drain()`);
        const dom = await this.eval<string>(`window.__e2e.dump()`);
        const text = await this.eval<string>(`window.__e2e.text()`);
        const requests = this.h.requests();
        this.h.clearRequests();
        const same = this.steps.filter(step => step.name === name || step.name.startsWith(`${name} #`)).length;
        this.steps.push({ name: same ? `${name} #${same + 1}` : name, requests, events, dom, text });
    }
}
