import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { hasPowerAc, IDX_MODELS, IT8613_MODELS, powerAcFor } from './power-ac-models';

const go = (file: string) => readFileSync(resolve(import.meta.dir, '../../../../backend/internal/fan', file), 'utf8');

test('the IT8613E list is the backend one', () => {
    const body = /func isIT8613Model[\s\S]*?\{\s*case ([^:]*):/.exec(go('caps.go'))?.[1] ?? '';
    expect(IT8613_MODELS.toSorted()).toEqual([...body.matchAll(/"([^"]+)"/g)].map(m => m[1]!).sort());
});

test('the iDX names are the backend ones', () => {
    expect(go('fanmodels.go')).toContain('model == "iDX6011 Pro"');
    expect(go('fanmodels.go')).toContain('model == "iDX6011" || model == "iDX6012"');
    expect(IDX_MODELS.toSorted()).toEqual(['iDX6011', 'iDX6011 Pro', 'iDX6012']);
});

test('every mechanism and what it offers, as docs/power-on-ac.md says', () => {
    expect(powerAcFor('DXP4800 Plus')).toEqual({ source: 'it8613', readable: true, settable: ['on', 'off', 'last'] });
    expect(powerAcFor('DXP4800 GT')).toEqual({ source: 'ec-cmd', readable: false, settable: ['on', 'off'] });
    expect(powerAcFor('iDX6011 Pro')).toEqual({ source: 'mmio-idx', readable: true, settable: ['on', 'off'] });
    expect(powerAcFor('DXP2800 GT')).toEqual({ source: 'fch-mmio', readable: true, settable: ['on', 'off', 'last'] });
});

test('models without a reachable setting show no card', () => {
    for (const model of ['DX4600 Pro', 'DXP480T Plus', 'DXP6800 Ultra', 'DXP8800 Pro', 'DXP6800 Plus', 'DH2600', 'DXP4800S', ''])
        expect(hasPowerAc(model)).toBe(false);
    expect(hasPowerAc('DXP2800')).toBe(true);
    expect(hasPowerAc('DXP4800 GT2')).toBe(true);
});
