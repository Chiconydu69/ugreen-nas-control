// What the plugin does on the detected model, from the model registry (web/models, rendered into models.json by `just docs`).
// The box on the LED page and the empty case view build their sentences from it; no model is named by hand.
import registry from '../../../../backend/internal/nasctl/models.json';
import type { Vars } from '../../core/i18n';
import type { Key } from './i18n/en';

export type LedKind = 'bays' | 'power' | 'none';
export type FanKind = 'fixed' | 'probe' | 'confirm' | 'none';
export type ViewKind = 'full' | 'front-rear' | 'none';

export interface ModelFacts {
    leds: LedKind;
    fans: FanKind;
    view: ViewKind;
}

interface RegistryEntry {
    dmiPattern: string;
    led: LedKind;
    caseView: ViewKind;
    fan: { chip: string; kind?: string } | null;
}

/** Fan kind from the registry entry: the CS201x is confirmed with `cs201x confirm`; the IT8613E probe models by a probe whose map the user confirms. */
const fanKind = (m: RegistryEntry): FanKind => (!m.fan ? 'none' : /cs201/i.test(m.fan.chip) ? 'confirm' : m.fan.kind === 'probe' ? 'probe' : 'fixed');

/** First registry model whose DMI pattern matches the product name (the registry is in matching order); null for an unknown model. */
export function modelFacts(product: string, models: readonly RegistryEntry[] = registry.models as RegistryEntry[]): ModelFacts | null {
    const hit = models.find(m => new RegExp(m.dmiPattern).test(product));
    if (!hit) return null;
    const fans = fanKind(hit);
    return { leds: hit.led, fans, view: hit.caseView };
}

const LEDS = { bays: 'model.leds.bays', power: 'model.leds.power', none: 'model.leds.none' } as const satisfies Record<LedKind, Key>;
const FANS = { fixed: 'model.fans.fixed', probe: 'model.fans.probe', confirm: 'model.fans.confirm', none: 'model.fans.none' } as const satisfies Record<
    FanKind,
    Key
>;
const VIEWS = { full: 'model.case.full', 'front-rear': 'model.case.front_rear', none: 'model.case.none' } as const satisfies Record<ViewKind, Key>;

type T = (key: Key, vars?: Vars) => string;

/** "On the {model}: {leds} {fans} {case}" for the box on the LED page. */
export function modelBox(model: string, facts: ModelFacts | null, t: T): string {
    if (!facts) return t('model.unknown', { model });
    return t('model.box', { model, leds: t(LEDS[facts.leds]), fans: t(FANS[facts.fans]), case: t(VIEWS[facts.view]) });
}

/** The phrase of the empty case view. */
export const caseText = (t: T): string => t(VIEWS.none);

export type DeviceCap = 'leds' | 'fans' | 'case' | 'sensors' | 'panel';
/** confirmed = verified by a maintainer; users = confirmed by users; partial = checked, but something is open (the note says what). */
export type DeviceState = 'confirmed' | 'users' | 'partial' | 'untested' | 'none';
export type Cta = 'confirm' | 'recheck' | 'tell';

export interface DeviceRow {
    cap: DeviceCap;
    state: DeviceState;
    /** The registry note, empty without one. */
    note: string;
}

export interface Device {
    name: string;
    series: string;
    rows: DeviceRow[];
    /** Capabilities nobody confirmed yet. */
    untested: DeviceCap[];
    /** confirm: something is untested; recheck: everything offered is confirmed. */
    cta: Cta;
}

interface DeviceEntry {
    name: string;
    series: string;
    dmiPattern: string;
    verified: Record<string, { status: string; by?: string; note?: string }>;
}

const STATE: Record<string, DeviceState> = { verified: 'confirmed', community: 'users', partial: 'partial', expected: 'untested', unsupported: 'none' };
const CAPS: DeviceCap[] = ['leds', 'fans', 'case', 'sensors', 'panel'];

/** The "Your device" card of About: one row per capability the registry tracks for the model. Null for an unknown model. */
export function deviceInfo(product: string, models: readonly DeviceEntry[] = registry.models as unknown as DeviceEntry[]): Device | null {
    const hit = models.find(m => new RegExp(m.dmiPattern).test(product));
    if (!hit) return null;
    const rows: DeviceRow[] = CAPS.map(cap => {
        const c = hit.verified[cap];
        return { cap, state: STATE[c?.status ?? 'expected'] ?? 'untested', note: c?.note ?? '' };
    });
    const untested = rows.filter(r => r.state === 'untested').map(r => r.cap);
    return { name: hit.name, series: hit.series, rows, untested, cta: untested.length ? 'confirm' : 'recheck' };
}

/** The registry's front-panel check (buttons and beeper) of the model: the state and who confirmed it. Null for an unknown model. */
export function panelEvidence(
    product: string,
    models: readonly DeviceEntry[] = registry.models as unknown as DeviceEntry[]
): { state: DeviceState; by: string; note: string } | null {
    const hit = models.find(m => new RegExp(m.dmiPattern).test(product));
    const c = hit?.verified.panel;
    return c ? { state: STATE[c.status] ?? 'untested', by: c.by ?? '', note: c.note ?? '' } : null;
}

/** Users or a maintainer confirmed the front-panel buttons on the model (registry panel check community or verified; partial is the beeper only). The shutdown on a held power button is offered only then (backend nasctl.PanelConfirmed). */
export function panelConfirmed(product: string, models: readonly DeviceEntry[] = registry.models as unknown as DeviceEntry[]): boolean {
    const hit = models.find(m => new RegExp(m.dmiPattern).test(product));
    const status = hit?.verified.panel?.status;
    return status === 'community' || status === 'verified';
}
