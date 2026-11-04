import type { Model } from './schema';

export default {
    id: 'dxp2800s',
    name: 'DXP2800S',
    series: 'DXP',
    dmiPattern: '^DXP2800S$',
    fixtureDmi: 'DXP2800S',
    cpu: null,
    bays: 2,
    m2Slots: null,
    m2: null,
    lan: null,
    led: 'bays',
    caseView: 'none',
    caseVariant: 'none',
    fan: null,
    verified: {
        leds: { status: 'expected', note: 'Same family as the DXP2800; the LED frame and bay order are not confirmed.' },
        fans: { status: 'unsupported', note: 'No fan wiring known for this model.' },
        case: { status: 'unsupported' },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12'],
    note: 'Two bays. Known from the name in UGREEN driver sources (power button, beeper); nobody has reported this model yet.',
    shots: false
} satisfies Model;
