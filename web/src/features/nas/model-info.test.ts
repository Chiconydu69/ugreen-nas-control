import { expect, test } from 'bun:test';
import registry from '../../../../backend/internal/nasctl/models.json';
import { type Vars } from '../../core/i18n';
import { en } from './i18n/en';
import { type FanKind, type LedKind, type ModelFacts, modelBox, modelFacts, modelRows, type ViewKind } from './model-info';
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

test('the DXP480T Plus: power LED only, no fans, case view front and rear', () => {
    const facts = modelFacts('DXP480T Plus') as ModelFacts;
    expect(facts).toEqual({ leds: 'power', fans: 'none', view: 'front-rear' });
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

test('the models table has one row per registry model with its case view, fans and LED word', () => {
    const rows = modelRows();
    expect(rows.map(r => r.name)).toEqual(registry.models.map(m => m.name));
    for (const [i, m] of registry.models.entries()) {
        const r = rows[i]!;
        expect(r.series).toBe(m.series);
        expect(r.re.test(m.fixtureDmi)).toBe(true);
        expect(r.view).toBe(m.caseView as ViewKind);
        expect(r.fans).toBe(modelFacts(m.fixtureDmi)!.fans);
        const word = m.led !== 'bays' ? m.led : { verified: 'verified', community: 'confirmed', partial: 'confirmed' }[m.verified.leds.status] || 'untested';
        expect(r.leds).toBe(word as typeof r.leds);
    }
});

test('the table shows the new case views: DXP4800 and DXP8800 Plus full, DXP480T Plus front and rear, DXP2800 front and rear', () => {
    const by = Object.fromEntries(modelRows().map(r => [r.name, r]));
    expect(by['DXP4800']!.view).toBe('full');
    expect(by['DXP8800 Plus']!.view).toBe('full');
    expect(by['DXP480T Plus']!.view).toBe('front-rear');
    expect(by['DXP2800']!.view).toBe('front-rear');
    expect(by['DXP480T Plus']!.leds).toBe('power');
});
