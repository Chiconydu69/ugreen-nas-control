import { expect, test } from 'bun:test';
import registry from '../../../../backend/internal/nasctl/models.json';
import { type Vars } from '../../core/i18n';
import { en } from './i18n/en';
import { type FanKind, type LedKind, type ModelFacts, deviceInfo, modelBox, modelFacts, panelEvidence, type ViewKind } from './model-info';
import type { Key } from './i18n/en';

const t = (key: Key, vars: Vars = {}): string => en[key].replace(/\{(\w+)\}/g, (_, name: string) => String(vars[name] ?? `{${name}}`));

test('every combination of LEDs, fans and case view gives one sentence per capability', () => {
    for (const leds of ['bays', 'power', 'none'] as LedKind[])
        for (const fans of ['fixed', 'probe', 'confirm', 'none'] as FanKind[])
            for (const view of ['full', 'front-rear', 'none'] as ViewKind[]) {
                const text = modelBox('X1', { leds, fans, view }, t);
                expect(text.startsWith('On the X1: ')).toBe(true);
                expect(text).not.toMatch(/\{\w+\}/);
                expect(text).toContain(t(`model.leds.${leds}`));
                expect(text).toContain(t(`model.fans.${fans}`));
                expect(text).toContain(t(view === 'front-rear' ? 'model.case.front_rear' : `model.case.${view}`));
            }
});

test('the DXP480T Plus: power LED only, no fans, case view full', () => {
    const facts = modelFacts('DXP480T Plus') as ModelFacts;
    expect(facts).toEqual({ leds: 'power', fans: 'none', view: 'full' });
    expect(modelBox('DXP480T Plus', facts, t)).toContain(
        'only the power LED (steady white while LED control is on; colour, brightness and off are not supported)'
    );
});

test('an unknown model says so', () => {
    expect(modelFacts('Something Else')).toBeNull();
    expect(modelBox('Something Else', null, t)).toBe('The Something Else is not in the model list yet.');
});

test('the facts follow the registry for every model', () => {
    for (const m of registry.models) {
        const f = modelFacts(m.fixtureDmi) as ModelFacts;
        expect(f).not.toBeNull();
        expect(f.leds).toBe(m.led as LedKind);
        expect(f.view).toBe(m.caseView as ViewKind);
        expect(f.fans === 'none').toBe(m.fan === null);
    }
});

test('the CS201x model is confirmed, the IT8613E probe models are probed', () => {
    expect(modelFacts('DXP2800 GT')?.fans).toBe('confirm');
    expect(modelFacts('DXP4800 Plus')?.fans).toBe('probe');
    expect(modelFacts('DXP4800 GT')?.fans).toBe('fixed');
});

const entry = (verified: Record<string, { status: string; by?: string; note?: string }>) => [{ name: 'X1', series: 'DX', dmiPattern: '^X1', verified }];
const ok = { status: 'verified' };

test('everything confirmed: the quiet "confirm again" action, no untested capability', () => {
    const d = deviceInfo(
        'X1',
        entry({ leds: ok, fans: { status: 'community' }, case: { status: 'partial', note: 'n' }, sensors: ok, panel: { status: 'community', by: 'u' } })
    )!;
    expect(d.rows.map(r => [r.cap, r.state])).toEqual([
        ['leds', 'confirmed'],
        ['fans', 'users'],
        ['case', 'users'],
        ['sensors', 'confirmed'],
        ['panel', 'users']
    ]);
    expect(d.rows[2]!.note).toBe('n');
    expect(d.untested).toEqual([]);
    expect(d.cta).toBe('recheck');
});

test('some untested: the capabilities are listed and the primary action is "confirm"; not supported is no gap', () => {
    const d = deviceInfo('X1', entry({ leds: { status: 'expected' }, fans: { status: 'unsupported' }, case: ok, sensors: { status: 'expected' }, panel: ok }))!;
    expect(d.rows.find(r => r.cap === 'fans')!.state).toBe('none');
    expect(d.untested).toEqual(['leds', 'sensors']);
    expect(d.cta).toBe('confirm');
});

test('the front-panel evidence is the registry check of the model: users on the DXP8800 Plus, nothing elsewhere', () => {
    expect(panelEvidence('DXP8800 Plus')).toEqual({ state: 'users', by: 'Chiconydu69 (issue 165)' });
    expect(panelEvidence('DXP4800 Plus')?.state).toBe('untested');
    expect(panelEvidence('DXP6800 Ultra')?.state).toBe('none');
    expect(panelEvidence('Something Else')).toBeNull();
});

test('an unknown model has no device card data', () => {
    expect(deviceInfo('Something Else')).toBeNull();
});

test('every registry model gets a card with its name, series and a row per tracked capability', () => {
    for (const m of registry.models) {
        const d = deviceInfo(m.fixtureDmi)!;
        expect(d).not.toBeNull();
        expect(d.series).toBe(m.series);
        expect(d.rows.length).toBe(5);
        expect(d.cta).toBe(d.untested.length ? 'confirm' : 'recheck');
    }
});
