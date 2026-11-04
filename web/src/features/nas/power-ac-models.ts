// Which models have a "power on after AC loss" setting the tool can reach, by DMI product_name. A mirror of powerACFor in
// backend/internal/fan/acloss.go (IT8613E models: isIT8613Model; DXP4800 GT and DXP2800 GT by prefix; iDX exact names);
// power-ac-models.test.ts keeps the IT8613E list in step. The answer of the tool says what the model offers (`settable`,
// `readable`); this table only decides whether the card shows and feeds the harness.

export const IT8613_MODELS = ['DXP2800', 'DXP4800', 'DXP4800 Plus', 'DXP4800 Pro', 'DXP6800 Pro', 'DXP8800 Plus'];
export const IDX_MODELS = ['iDX6011', 'iDX6012', 'iDX6011 Pro'];

export type AcMode = 'on' | 'off' | 'last';

export interface PowerAc {
    source: 'it8613' | 'ec-cmd' | 'mmio-idx' | 'fch-mmio';
    /** The board can report the current value (not the DXP4800 GT). */
    readable: boolean;
    settable: AcMode[];
}

export function powerAcFor(model: string): PowerAc | null {
    if (IT8613_MODELS.includes(model)) return { source: 'it8613', readable: true, settable: ['on', 'off', 'last'] };
    if (model.startsWith('DXP4800 GT')) return { source: 'ec-cmd', readable: false, settable: ['on', 'off'] };
    if (IDX_MODELS.includes(model)) return { source: 'mmio-idx', readable: true, settable: ['on', 'off'] };
    if (model.startsWith('DXP2800 GT')) return { source: 'fch-mmio', readable: true, settable: ['on', 'off', 'last'] };
    return null;
}

export const hasPowerAc = (model: string): boolean => powerAcFor(model) !== null;
