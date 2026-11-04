// The window.* globals the plugin pages and the script files share. The .page files set the UGREEN_* values
// (json_encode'd by PHP); every file below is a classic script, so this is the whole contract between them.
import type { LedState } from './contract';
import type { Lang } from './i18n';

type TextTable = Record<string, string>;
type Tables = Partial<Record<Lang, TextTable>>;

interface FetchTextResult {
    ok: boolean;
    status: number;
    text: string;
    rx: number;
}

/** js/ledfx.js: polling shared by the NAS page, the LED pages and the dashboard tile. */
export interface PollApi {
    awake(): boolean;
    setIdle(ms: number): void;
    fetchText(url: string, maxAge?: number): Promise<FetchTextResult>;
    lastPhaseRx: number | null;
    json<T = unknown>(url: string, maxAge?: number): Promise<T>;
    invalidate(): void;
    every(fn: () => unknown, ms: number | (() => number), when?: () => boolean): { poke(): void };
}

/** js/ledfx.js: the one LED renderer. */
export interface LedFxApi {
    alpha: typeof import('./ledfx/fx').alpha;
    breath: typeof import('./ledfx/fx').breath;
    hsv: typeof import('./ledfx/fx').hsv;
    describe: typeof import('./ledfx/fx').describe;
    paint: typeof import('./ledfx/fx').paint;
    stamp(leds: Record<string, LedState> | null | undefined, rx?: number): Record<string, LedState> | null | undefined;
}

declare global {
    interface Window {
        UNC_I18N?: Tables;
        ULC_I18N?: Tables;
        UNCPoll: PollApi;
        UNCLedFx: LedFxApi;
        UGREEN_NAS_CSRF_TOKEN?: string;
        UGREEN_NAS_VERSION?: string;
        UGREEN_NAS_LANG?: string;
        UGREEN_NAS_LANG_SETTING?: string;
        UGREEN_NAS_THEME?: string;
        UNC_LED?: boolean;
        UNC_NAV?: (nav: string) => void;
        UGREEN_LED_CSRF_TOKEN?: string;
        UGREEN_LED_LANG?: string;
        UGREEN_LED_THEME?: string;
        ULC?: { show(view: string): void };
    }
}
