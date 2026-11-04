// Runs in the plugin frame before the plugin's own scripts (web/tools/demo.ts puts it first in <head>): answers the
// plugin's api.php requests from web/demo/api.ts, and gives the page the globals Unraid's PHP would have written.
import { createDemo, pageGlobals, seedPrefs } from './api';
import { browserStore, findModel, PAGES, pageOfNav } from './shared';

const params = new URLSearchParams(location.search);
const model = findModel(params.get('model'));
const store = browserStore();

// bare=1: the frame is one part of the plugin inside the landing page (docs/site/islands.ts). The plugin's sidebar and page
// header are hidden (skip=drives,fantiles also drops the drives card of the case page and the fan tiles of the fan page), the frame reports its height to the page, and the page's theme (theme=) is applied. Without the
// parameter none of this runs and the demo behaves as before.
const bare = params.get('bare') === '1';
if (bare) {
    const theme = params.get('theme');
    if (theme === 'dark' || theme === 'light') seedPrefs(store, { theme });
    // Frames of one page share sessionStorage; the plugin's one-shot page hand-over (unc-nav) must stay per frame.
    const mem = new Map<string, string>();
    const own = {
        getItem: (k: string) => mem.get(k) ?? null,
        setItem: (k: string, v: string) => void mem.set(k, String(v)),
        removeItem: (k: string) => void mem.delete(k),
        clear: () => mem.clear(),
        key: (i: number) => [...mem.keys()][i] ?? null,
        get length() {
            return mem.size;
        }
    };
    try {
        Object.defineProperty(window, 'sessionStorage', { configurable: true, value: own });
    } catch {
        // the browser keeps the real one; the hand-over then relies on load order
    }
    const skip = (params.get('skip') ?? '').split(',');
    const style = document.createElement('style');
    style.textContent =
        '.unc-side,.unc-head{display:none!important}' +
        '.unc{grid-template-columns:minmax(0,1fr)!important;min-height:0!important;margin:0!important;border:0!important;border-radius:0!important}' +
        (skip.includes('drives') ? '[data-view="case"] .unc-card:first-child{display:none!important}' : '') +
        // The overview island: the cooling card sets the row height, so it drops its hint and temperature row; the case front then has no empty band above the bays.
        '.unc-ov-cool .unc-mode-hint,.unc-ov-cool .unc-heat{display:none!important}' +
        '.unc-ov-case .unc-hero{padding:10px 0}' +
        // Narrow frames: grid children may shrink instead of overflowing their card.
        '.unc-fantiles{grid-template-columns:repeat(auto-fit,minmax(min(300px,100%),1fr))!important}.unc-fantiles>*,.unc-card>*{min-width:0;max-width:100%}' +
        (skip.includes('fantiles') ? '#unc-now{display:none!important}' : '') +
        'html{scrollbar-width:none}html::-webkit-scrollbar{display:none}' +
        '.unc-main{padding:16px!important}body{padding:0!important}body:has(.unc-dash){padding:12px!important}';
    document.documentElement.append(style);
    // The page's theme switch reaches a loaded frame by message, so it does not reload (and flash). The plugin roots carry data-theme.
    window.addEventListener('message', event => {
        const data = event.data as { demo?: string; theme?: string } | null;
        if (event.source !== parent || event.origin !== location.origin || data?.demo !== 'theme') return;
        if (data.theme !== 'dark' && data.theme !== 'light') return;
        seedPrefs(store, { theme: data.theme });
        (window as unknown as { UGREEN_NAS_THEME?: string }).UGREEN_NAS_THEME = data.theme;
        for (const el of document.querySelectorAll<HTMLElement>('[data-theme]')) el.dataset.theme = data.theme;
    });
}
const demo = createDemo(model, store, { live: true, scenario: params.get('scenario') });
const g = pageGlobals(store, model.id, navigator.language);

// The page was rendered with the LED pages on or off; the switch may have changed (the plugin reloads after it).
const root = document.documentElement;
const file = root.dataset.demo ?? 'app';
if ((root.dataset.led === '1') !== g.led) {
    location.replace(`${file}${g.led ? '' : '-noled'}.html${location.search}`);
}

(window as unknown as { __UNC_DEMO: unknown }).__UNC_DEMO = {
    UGREEN_NAS_LANG: g.lang,
    UGREEN_NAS_LANG_SETTING: g.setting,
    UGREEN_NAS_THEME: g.theme,
    UGREEN_LED_LANG: g.lang,
    UGREEN_LED_THEME: g.theme
};

const original = window.fetch;
const real = original.bind(window);
window.fetch = Object.assign(
    async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = new URL(input instanceof Request ? input.url : String(input), location.href);
        if (!/\/api\.php$/.test(url.pathname)) return real(input, init);
        const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase();
        const body = init?.body instanceof URLSearchParams ? init.body : new URLSearchParams(typeof init?.body === 'string' ? init.body : '');
        await new Promise(done => setTimeout(done, 40));
        const answer = demo.handle(method, url.pathname, url.searchParams, body);
        return new Response(JSON.stringify(answer.json), { status: answer.status, headers: { 'content-type': 'application/json' } });
    },
    { preconnect: original.preconnect }
);

// Deep link to a page: the plugin opens the view it finds in sessionStorage (it does so after switching the LEDs).
const page = params.get('page');
if (page && PAGES[page]) {
    sessionStorage.setItem('unc-nav', PAGES[page]);
    params.delete('page');
    history.replaceState(null, '', `${location.pathname}?${params}`);
}

// The page behind the plugin follows its colour scheme (Unraid's own page does that in the real thing).
const background = (theme: string | undefined) => (theme === 'light' ? (file === 'tile' ? '#f2f2f2' : '#f4f6f9') : file === 'tile' ? '#1c1d21' : '#0b0e12');
const paint = () => {
    const scheme =
        document.querySelector<HTMLElement>('[data-theme]')?.dataset.theme ??
        (g.theme === 'auto' && matchMedia('(prefers-color-scheme: light)').matches ? 'light' : g.theme);
    root.style.setProperty('--demo-bg', background(scheme));
    root.style.setProperty('--demo-fg', scheme === 'light' ? '#1c1b1b' : '#f2f2f2');
};
paint();
new MutationObserver(paint).observe(root, { attributes: true, attributeFilter: ['data-theme'], subtree: true });

// Tell the demo page which view is open, so its URL can be shared (also after a reload, which opens the overview again).
const first = pageOfNav(sessionStorage.getItem('unc-nav') ?? 'overview');
window.addEventListener('load', () => parent.postMessage({ demo: 'nav', page: first }, location.origin));

document.addEventListener('click', event => {
    const nav = (event.target as Element | null)?.closest<HTMLElement>('[data-nav]')?.dataset.nav;
    if (nav) parent.postMessage({ demo: 'nav', page: pageOfNav(nav) }, location.origin);
});

// The dashboard tile links to the plugin's settings page: open the plugin view of the demo instead.
document.addEventListener(
    'click',
    event => {
        const link = (event.target as Element | null)?.closest<HTMLAnchorElement>('a[href^="/Settings/"]');
        if (!link) return;
        event.preventDefault();
        parent.postMessage({ demo: 'open', page: pageOfNav(link.dataset.nav ?? 'overview') }, location.origin);
    },
    true
);

// Bare frames tell the page how tall they are, so the iframe has no scrollbar of its own.
if (bare) {
    let last = 0;
    const report = () => {
        const h = Math.ceil(document.body.getBoundingClientRect().height);
        if (h && h !== last) parent.postMessage({ demo: 'height', h }, location.origin);
        last = h;
    };
    // The shim runs in <head>: the body does not exist yet.
    document.addEventListener('DOMContentLoaded', () => new ResizeObserver(report).observe(document.body));
    window.addEventListener('load', report);
}

// Esc inside the plugin closes the site's dialog around the demo.
document.addEventListener('keydown', event => {
    if (event.key === 'Escape') parent.postMessage({ demo: 'esc' }, location.origin);
});
