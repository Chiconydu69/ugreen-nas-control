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
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: false },
    lan: null,
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear2800',
    fan: null,
    verified: {
        leds: { status: 'expected', note: 'Same family as the DXP2800; the LED frame and bay order are not confirmed.' },
        fans: { status: 'unsupported', note: 'No fan wiring known for this model.' },
        case: {
            status: 'expected',
            note: "Drawn after the DXP2800 (front, back) and the DXP2800 GT (inside); this model's hardware is not known, so ports, LAN speed and slot numbering are assumed."
        },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12'],
    note: 'Two bays; the case view is drawn after the DXP2800. Known from the name in UGREEN driver sources (power button, beeper); nobody has reported this model yet.',
    shots: false
} satisfies Model;
