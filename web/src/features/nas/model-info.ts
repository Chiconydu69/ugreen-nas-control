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

export type LedState = 'verified' | 'confirmed' | 'untested' | 'power' | 'none';

export interface TableRow {
    series: string;
    name: string;
    /** The model's DMI pattern. */
    re: RegExp;
    leds: LedState;
    view: ViewKind;
    fans: FanKind;
}

interface TableEntry extends RegistryEntry {
    series: string;
    name: string;
    verified: { leds: { status: string } };
}

/** The word of the LED cell: no LEDs or power LED only from the registry's led kind, else its leds evidence (community and partial = confirmed, expected = untested). */
function ledState(m: TableEntry): LedState {
    if (m.led !== 'bays') return m.led;
    const s = m.verified.leds.status;
    return s === 'verified' ? 'verified' : s === 'community' || s === 'partial' ? 'confirmed' : 'untested';
}

/** The rows of About → Supported models, in registry order (series, then name). */
export function modelRows(models: readonly TableEntry[] = registry.models as TableEntry[]): TableRow[] {
    return models.map(m => ({ series: m.series, name: m.name, re: new RegExp(m.dmiPattern), leds: ledState(m), view: m.caseView, fans: fanKind(m) }));
}
