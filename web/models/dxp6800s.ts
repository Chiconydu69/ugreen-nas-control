import type { Model } from './schema';

export default {
    id: 'dxp6800s',
    name: 'DXP6800S',
    series: 'DXP',
    dmiPattern: '^DXP6800S$',
    fixtureDmi: 'DXP6800S',
    cpu: null,
    bays: 6,
    m2Slots: null,
    m2: { slots: ['m2-1', 'm2-2', 'system'], pcieSlot: true },
    lan: null,
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear6800',
    fan: null,
    verified: {
        leds: {
            status: 'expected',
            note: "Only UGREEN's LED driver names this model (six bay LEDs, same frame as the plain DXP6800); the bay order is guessed and the plugin has not been tried on it."
        },
        fans: { status: 'unsupported', note: 'No fan wiring known for this model.' },
        case: {
            status: 'expected',
            note: "Drawn after the DXP6800 Pro; this model's hardware is not known, so ports, PCIe slot and slot numbering are assumed."
        },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12'],
    note: 'Known from the name in UGREEN driver sources only; hardware details are not known, the case view is drawn after the DXP6800 Pro. LEDs are enabled but nobody has confirmed them.',
    shots: false
} satisfies Model;
