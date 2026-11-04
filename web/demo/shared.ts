// Pieces the demo page (outer.ts) and the plugin frame (shim.ts) share.
import type { Model } from '../models/schema';
import type { Store } from './api';

/** The registry, put in by tools/demo.ts at build time. */
declare const __MODELS__: Model[];
export const MODELS: Model[] = __MODELS__;
export const DEFAULT_MODEL = 'dxp6800-pro';

export const findModel = (id: string | null): Model => MODELS.find(m => m.id === id) ?? MODELS.find(m => m.id === DEFAULT_MODEL) ?? MODELS[0]!;

export const PAGES: Record<string, string> = {
    overview: 'overview',
    case: 'case',
    sensors: 'sensors',
    fans: 'curves',
    safety: 'safety',
    history: 'history',
    leds: 'led:overview',
    drives: 'led:drives',
    night: 'led:schedules',
    alerts: 'led:system',
    settings: 'settings',
    about: 'about'
};
export const pageOfNav = (nav: string): string => Object.entries(PAGES).find(([, v]) => v === nav)?.[0] ?? 'overview';

export function pageAvailable(model: Model, page: string): boolean {
    if (page === 'fans' || page === 'safety' || page === 'history') return model.fan !== null;
    if (page === 'case') return model.caseView !== 'none';
    if (['leds', 'drives', 'night', 'alerts'].includes(page)) return model.led !== 'none';
    return page in PAGES;
}

/** localStorage, or a plain object when the browser refuses it (private mode, blocked cookies). */
export function browserStore(): Store {
    try {
        localStorage.setItem('unc-demo/probe', '1');
        localStorage.removeItem('unc-demo/probe');
        return localStorage;
    } catch {
        const map = new Map<string, string>();
        return { getItem: k => map.get(k) ?? null, setItem: (k, v) => void map.set(k, v), removeItem: k => void map.delete(k) };
    }
}
