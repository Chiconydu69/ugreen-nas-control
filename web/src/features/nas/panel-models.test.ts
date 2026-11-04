import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { BEEPER_MODELS, BUTTON_MODELS, hasBeeper, hasButtons, powerDoubtful } from './panel-models';

const go = (file: string) => readFileSync(resolve(import.meta.dir, '../../../../backend/internal/panel', file), 'utf8');

// Every quoted name inside `exact: []string{...}` and `prefix: []string{...}` of the Go table.
const names = (source: string, field: 'exact' | 'prefix'): string[] =>
    [...source.matchAll(new RegExp(`${field}: \\[\\]string\\{([^}]*)\\}`, 'g'))].flatMap(m => [...m[1]!.matchAll(/"([^"]+)"/g)].map(x => x[1]!)).sort();

test('the button table is the one of the backend', () => {
    const source = go('buttons.go');
    expect(BUTTON_MODELS.exact.toSorted()).toEqual(names(source, 'exact'));
    expect(BUTTON_MODELS.prefix.toSorted()).toEqual(names(source, 'prefix'));
});

test('the beeper table is the one of the backend', () => {
    const source = go('beeper.go');
    expect(BEEPER_MODELS.exact.toSorted()).toEqual(names(source, 'exact'));
    expect(BEEPER_MODELS.prefix.toSorted()).toEqual(names(source, 'prefix'));
});

test('exact names are exact, prefixes match variants, Ultra is refused', () => {
    expect(hasButtons('DXP4800')).toBe(true);
    expect(hasButtons('DXP4800 Plus rev2')).toBe(true);
    expect(hasButtons('DXP4800 GT')).toBe(true);
    expect(hasButtons('DXP4800 GT2')).toBe(false);
    expect(hasButtons('DXP6800 Ultra')).toBe(false);
    expect(hasBeeper('DXP6800 Pro')).toBe(true);
    expect(hasBeeper('DXP6800 Ultra')).toBe(false);
    expect(hasBeeper('')).toBe(false);
});

test('the DXP2800 GT, the DXP4800 GT and the DX4600 have buttons but no beeper', () => {
    for (const model of ['DXP2800 GT', 'DXP4800 GT', 'DX4600 Pro']) {
        expect([model, hasButtons(model), hasBeeper(model)]).toEqual([model, true, false]);
    }
});

test('the iDX beepers: plain and Pro are different exact names, the buttons take both by prefix', () => {
    expect(hasBeeper('iDX6011')).toBe(true);
    expect(hasBeeper('iDX6011 Pro')).toBe(true);
    expect(hasBeeper('iDX6011 Pro Max')).toBe(false);
    expect(hasButtons('iDX6011 Pro Max')).toBe(true);
    expect(powerDoubtful('iDX6011 Pro')).toBe(true);
    expect(powerDoubtful('DXP4800')).toBe(false);
});
