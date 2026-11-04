// Which models have a readable power/reset button and a beeper, by DMI product_name. A mirror of the tables in
// backend/internal/panel (buttons.go ButtonSpecs, beeper.go BeeperSpecs), which match the way UGREEN's drivers do: exact
// name or name prefix. panel-models.test.ts keeps the two in step. Every entry is vendor source, not yet confirmed on hardware.

interface Match {
    exact: string[];
    prefix: string[];
}

export const BUTTON_MODELS: Match = {
    exact: ['DXP2800', 'DXP2800S', 'DXP4800', 'DXP4800S', 'DXP4800 GT', 'DXP2800 GT'],
    prefix: ['DXP4800 Plus', 'DXP4800 Pro', 'DXP6800', 'DXP8800', 'FORT 6', 'DXP480T Plus', 'iDX6011', 'iDX6012', 'DH2600', 'DX4600', 'DX4700']
};

export const BEEPER_MODELS: Match = {
    exact: ['DXP2800', 'DXP2800S', 'DXP4800', 'DXP4800S', 'DX4700', 'iDX6011', 'iDX6012', 'iDX6011 Pro'],
    prefix: ['DXP4800 Plus', 'DXP4800 Pro', 'DXP6800', 'DXP8800', 'FORT 6', 'DH2600', 'DXP480T Plus']
};

// DXP6800 Ultra and DXP8800 Ultra are newer than the vendor source; its prefixes would match them by accident.
const matches = (model: string, m: Match): boolean => !model.includes('Ultra') && (m.exact.includes(model) || m.prefix.some(p => model.startsWith(p)));

export const hasButtons = (model: string): boolean => matches(model, BUTTON_MODELS);
export const hasBeeper = (model: string): boolean => matches(model, BEEPER_MODELS);

/** UGREEN's source says the power key of the iDX6011/6012 sits on ACPI and cannot be read by address, yet it lists an address. */
export const powerDoubtful = (model: string): boolean => /^iDX601[12]/.test(model);
