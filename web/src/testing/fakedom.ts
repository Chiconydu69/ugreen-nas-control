// A DOM stand-in for running a page script without a browser: every element is a proxy that remembers what was
// written to it (and logs it), lists from querySelectorAll are empty. Enough to run a page's start-up and first
// render and to compare what two versions of a script write.
export type Log = string[];

export interface FakeDom {
    log: Log;
    props: Map<string, unknown>;
    install(extra?: Record<string, unknown>): void;
    restore(): void;
}

type Fn = (...args: unknown[]) => unknown;

export function createFakeDom(ids: string[], fetchFixture: (url: string, init?: RequestInit) => unknown): FakeDom {
    const log: Log = [];
    const props = new Map<string, unknown>();
    const datasets = new Map<string, Record<string, string>>();
    const noop: Fn = () => undefined;

    const element = (path: string): HTMLElement =>
        new Proxy(noop, {
            get(_target, prop) {
                if (typeof prop === 'symbol') return undefined;
                switch (prop) {
                    case 'querySelector':
                        return (selector: string) => element(`${path} ${selector}`);
                    case 'querySelectorAll':
                        return () => [];
                    case 'closest':
                        return (selector: string) => element(`${path} ^${selector}`);
                    case 'getAttribute':
                        return () => null;
                    case 'hasAttribute':
                        return () => false;
                    case 'contains':
                        return () => false;
                    case 'getBoundingClientRect':
                        return () => ({ left: 0, right: 0, top: 0, bottom: 0, width: 0, height: 0 });
                    case 'getClientRects':
                        return () => [{}];
                    case 'classList':
                        return {
                            add: (c: string) => log.push(`${path} +${c}`),
                            remove: (c: string) => log.push(`${path} -${c}`),
                            toggle: (c: string, on?: boolean) => log.push(`${path} ${on === false ? '-' : on === true ? '+' : '~'}${c}`),
                            contains: () => false
                        };
                    case 'style':
                        return new Proxy(
                            { setProperty: (k: string, v: string) => log.push(`${path} style ${k}: ${v}`) },
                            { set: (_t, p, v) => (log.push(`${path} style.${String(p)} = ${v}`), true) }
                        );
                    case 'dataset': {
                        const data = datasets.get(path) ?? {};
                        datasets.set(path, data);
                        return new Proxy(data, { set: (t, p, v) => (log.push(`${path} data-${String(p)} = ${v}`), Reflect.set(t, p, v)) });
                    }
                    case 'firstChild':
                    case 'firstElementChild':
                        return element(`${path} first`);
                    case 'clientWidth':
                        return 320;
                    case 'offsetHeight':
                    case 'offsetParent':
                        return props.get(`${path}.${prop}`) ?? null;
                    case 'insertAdjacentHTML':
                        return (where: string, html: string) => log.push(`${path} insert ${where} ${html}`);
                    case 'value':
                    case 'textContent':
                    case 'innerHTML':
                        return props.get(`${path}.${prop}`) ?? '';
                    case 'checked':
                    case 'hidden':
                    case 'disabled':
                        return props.get(`${path}.${prop}`) ?? false;
                    default:
                        return props.get(`${path}.${prop}`) ?? noop;
                }
            },
            set(_target, prop, value) {
                props.set(`${path}.${String(prop)}`, value);
                log.push(`${path}.${String(prop)} = ${typeof value === 'string' ? value : JSON.stringify(value)}`);
                return true;
            }
        }) as unknown as HTMLElement;

    const store = (): Storage => {
        const map = new Map<string, string>();
        return { getItem: k => map.get(k) ?? null, setItem: (k, v) => void map.set(k, v), removeItem: k => void map.delete(k) } as Storage;
    };

    const saved = new Map<string, { had: boolean; value: unknown }>();
    const restore = (): void => {
        const g = globalThis as unknown as Record<string, unknown>;
        for (const [k, { had, value }] of saved)
            if (had) g[k] = value;
            else delete g[k];
        saved.clear();
    };

    const install = (extra: Record<string, unknown> = {}): void => {
        const g = globalThis as unknown as Record<string, unknown>;
        const document = {
            getElementById: (id: string) => (ids.includes(id) ? element(`#${id}`) : null),
            body: element('body'),
            readyState: 'complete',
            visibilityState: 'visible',
            activeElement: null,
            addEventListener: noop,
            querySelectorAll: () => [],
            createElement: () => element('new'),
            createTextNode: (text: string) => text
        };
        const values = {
            window: g,
            document,
            sessionStorage: store(),
            localStorage: store(),
            location: { reload: noop },
            matchMedia: () => ({ matches: false }),
            getComputedStyle: () => ({ backgroundColor: 'rgb(0, 0, 0)' }),
            requestAnimationFrame: () => 1,
            scrollY: 0,
            scrollTo: noop,
            innerHeight: 800,
            innerWidth: 1200,
            addEventListener: noop,
            ResizeObserver: class {
                observe = noop;
            },
            fetch: async (url: string, init?: RequestInit) => {
                const body = fetchFixture(url, init);
                return new Response(JSON.stringify(body), { status: 200 });
            },
            ...extra
        };
        for (const k of Object.keys(values)) if (!saved.has(k)) saved.set(k, { had: k in g, value: g[k] });
        Object.assign(g, values);
    };

    return { log, props, install, restore };
}
