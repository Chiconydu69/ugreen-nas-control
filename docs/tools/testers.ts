/** People credited in the README (rendered by gen-docs.ts). Add the reporter's GitHub login when a model report is recorded (docs/hardware.md). */
export const TESTERS: { login: string; did: string }[] = [
    { login: '321chills', did: 'DXP2800 GT: wiring research and fan control test' },
    { login: 'miskcoo', did: 'LED protocol and bay mapping (ugreen_leds_controller driver)' },
    { login: 'datarepos76', did: 'DXP480T Plus' },
    { login: 'Chiconydu69', did: 'DXP8800 Plus: fan wiring, header 4 confirmation, NVMe root ports, front-panel button tests' },
    { login: 'ivolution', did: 'Spanish translation, DXP4800 Plus tests' },
    { login: 'DanielRuppen', did: 'DXP2800 SUB2: ACPI and PCI data, lpss-probe runs, LED path confirmation' },
    { login: 'jsaltzman', did: 'DXP6800 Pro bug reports, the cause of the failing update check' }
];

/** Testers on the Unraid forum without a known GitHub account: named, not linked. */
export const FORUM_TESTERS = ['fkiesler', 'gigios', 'NullNull67', 'cardsdoc', 'gonzlobo'];

/** Maintainers, shown first in the README credits. */
export const MAINTAINERS: { login: string; name: string }[] = [
    { login: 'j3n57h0m45', name: 'Jens Thomas' },
    { login: 'alexanderschubert', name: 'Alexander Schubert' }
];
