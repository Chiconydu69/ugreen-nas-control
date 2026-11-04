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
    m2: null,
    lan: null,
    led: 'bays',
    caseView: 'none',
    caseVariant: 'none',
    fan: null,
    verified: {
        leds: {
            status: 'expected',
            note: "Only UGREEN's LED driver names this model (six bay LEDs, same frame as the plain DXP6800); the bay order is guessed and the plugin has not been tried on it."
        },
        fans: { status: 'unsupported', note: 'No fan wiring known for this model.' },
        case: { status: 'unsupported' },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12'],
    note: 'Known from the name in UGREEN driver sources only; hardware details are not known. LEDs are enabled but nobody has confirmed them.',
    shots: false
} satisfies Model;
