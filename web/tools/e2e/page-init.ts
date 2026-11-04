// Runs in the page before its own scripts (injected into <head> by the harness). Makes the page repeatable and
// observable: fixed clock that only moves when a test says so, no animations, errors / console errors / toasts /
// confirm() / window.open() recorded, in-flight fetches counted, and a DOM dump for the snapshots.
// Written as a normal function so it is type-checked; `pageInit` is its source text.
declare const __E2E_NOW__: number;
function init(): void {
    const w = window as any;
    const T0 = __E2E_NOW__;
    let offset = 0;
    const RealDate = Date;
    class FixedDate extends RealDate {
        constructor(...args: any[]) {
            // @ts-expect-error spread into the overloaded super constructor
            if (args.length) super(...args);
            else super(T0 + offset);
        }
        static override now(): number {
            return T0 + offset;
        }
    }
    w.Date = FixedDate;
    // performance.now() moves with the same fixed clock (the case drawing derives an animation phase from it);
    // the test's own idle detection keeps the real one.
    const realNow = performance.now.bind(performance);
    performance.now = () => 5000 + offset;

    // The shared poller's one-second tick runs when a test says so (e2e.tick), not on the wall clock.
    const ticks: (() => void)[] = [];
    const realSetInterval = w.setInterval.bind(w);
    w.setInterval = (fn: () => void, ms?: number, ...rest: unknown[]) => (ms === 1000 ? (ticks.push(fn), 0) : realSetInterval(fn, ms, ...rest));

    // Math.random is seeded: the case drawing staggers its animation delays with it.
    let seed = 0x2f6e2b1;
    Math.random = () => {
        seed = (seed + 0x6d2b79f5) | 0;
        let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
        t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };

    const KEY = '__e2e_log';
    const read = (): any[] => {
        try {
            return JSON.parse(sessionStorage.getItem(KEY) || '[]');
        } catch {
            return [];
        }
    };
    // sessionStorage, so what happened before a reload (language change) is not lost.
    const push = (kind: string, text: unknown): void => {
        try {
            const all = read();
            all.push({ kind, text: String(text).slice(0, 400) });
            sessionStorage.setItem(KEY, JSON.stringify(all));
        } catch {
            /* storage blocked */
        }
    };

    const e2e: any = (w.__e2e = {
        marker: true,
        pending: 0,
        lastIo: realNow(),
        now: realNow,
        tick() {
            ticks.forEach(fn => fn());
        },
        advance(ms: number) {
            offset += ms;
        },
        drain(): any[] {
            const all = read();
            sessionStorage.removeItem(KEY);
            return all;
        }
    });

    addEventListener(
        'error',
        (event: any) => {
            const target = event.target;
            if (target && target !== window) push('resource', target.src || target.href || target.tagName);
            else push('uncaught', `${event.message} (${String(event.filename).split('/').pop()}:${event.lineno})`);
        },
        true
    );
    addEventListener('unhandledrejection', (event: any) => push('unhandledrejection', event.reason?.message ?? event.reason));
    for (const level of ['error', 'warn'] as const) {
        const original = console[level].bind(console);
        console[level] = (...args: unknown[]) => {
            push(`console.${level}`, args.map(String).join(' '));
            original(...args);
        };
    }

    const realFetch = w.fetch.bind(w);
    w.fetch = (...args: unknown[]) => {
        e2e.pending++;
        return realFetch(...args).finally(() => {
            e2e.pending--;
            e2e.lastIo = realNow();
        });
    };
    w.confirm = (message: string) => (push('confirm', message), true);
    w.open = (url: string) => (push('open', url), null);

    // Fan icons stand still (the script-driven spin would make every snapshot differ).
    const matchMedia = w.matchMedia.bind(w);
    w.matchMedia = (query: string) => {
        const list = matchMedia(query);
        if (!/prefers-reduced-motion/.test(query)) return list;
        return new Proxy(list, {
            get: (target: any, key) => (key === 'matches' ? true : typeof target[key] === 'function' ? target[key].bind(target) : target[key])
        });
    };

    const style = document.createElement('style');
    style.textContent =
        '*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important;scroll-behavior:auto!important}body{font-family:system-ui,sans-serif}';
    document.head.appendChild(style);

    // Toasts disappear on a timer: record what was shown instead of looking at the DOM at a random moment.
    new MutationObserver(records => {
        for (const id of ['unc-toast', 'ulc-toast']) {
            const el = document.getElementById(id);
            if (!el || !el.classList.contains('show') || !el.textContent) continue;
            if (records.some(r => el.contains(r.target))) push('toast', `${el.textContent} [${el.className.replace(/^\S+ show ?/, '') || 'success'}]`);
        }
    }).observe(document, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['class'] });

    // What a person reads: text of everything on screen plus the values of the form fields, one item per line.
    const SKIP = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT']);
    e2e.text = (): string => {
        const lines: string[] = [];
        const shown = (el: Element | null): boolean => {
            for (let e = el; e; e = e.parentElement) {
                if (SKIP.has(e.tagName) || e.id === 'unc-toast' || e.id === 'ulc-toast') return false;
                if (e instanceof HTMLElement && (e.hidden || getComputedStyle(e).visibility === 'hidden')) return false;
            }
            return !!el && el.getClientRects().length > 0;
        };
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_ELEMENT | NodeFilter.SHOW_TEXT);
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
            if (n.nodeType === 3) {
                const t = (n.textContent || '').replace(/\s+/g, ' ').trim();
                if (t && shown(n.parentElement)) lines.push(t);
            } else if (n instanceof HTMLInputElement || n instanceof HTMLSelectElement || n instanceof HTMLTextAreaElement) {
                if (shown(n) && !(n instanceof HTMLInputElement && n.type === 'hidden'))
                    lines.push(`[${n instanceof HTMLInputElement && n.type === 'checkbox' ? n.checked : n.value}]`);
            }
        }
        return lines.join('\n');
    };

    // One line per element: <tag sorted attributes> {live state} direct text. Toasts and scripts are left out.
    e2e.dump = (): string => {
        const out: string[] = [];
        const walk = (el: Element, depth: number): void => {
            if (SKIP.has(el.tagName) || el.id === 'unc-toast' || el.id === 'ulc-toast') return;
            const attrs = [...el.attributes]
                .filter(a => a.name !== 'value' || el.tagName !== 'INPUT')
                .sort((a, b) => (a.name < b.name ? -1 : 1))
                .map(a => ` ${a.name}="${a.value.replace(/\s+/g, ' ')}"`)
                .join('');
            const live: string[] = [];
            if (el instanceof HTMLInputElement) {
                live.push(`value=${JSON.stringify(el.value)}`);
                if (el.type === 'checkbox' || el.type === 'radio') live.push(`checked=${el.checked}`);
            } else if (el instanceof HTMLSelectElement) live.push(`value=${JSON.stringify(el.value)}`);
            else if (el instanceof HTMLTextAreaElement) live.push(`value=${JSON.stringify(el.value)}`);
            if (el instanceof HTMLElement && el.tagName !== 'SVG') live.push(`shown=${el.getClientRects().length > 0}`);
            const text = [...el.childNodes]
                .filter(n => n.nodeType === 3)
                .map(n => (n.textContent || '').replace(/\s+/g, ' ').trim())
                .filter(Boolean)
                .join(' | ');
            out.push(`${'  '.repeat(depth)}<${el.tagName.toLowerCase()}${attrs}>${live.length ? ` {${live.join(' ')}}` : ''}${text ? ` ${text}` : ''}`);
            for (const child of el.children) walk(child, depth + 1);
        };
        walk(document.body, 0);
        return out.join('\n');
    };
}

/** The page clock at load, unix seconds; the harness freezes its history to the same moment. */
export const E2E_NOW = Date.parse('2026-10-29T11:00:00Z') / 1000;
export const pageInit = `(${init.toString()})()`.replace('__E2E_NOW__', String(E2E_NOW * 1000));
