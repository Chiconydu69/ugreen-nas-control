import type { Model } from './schema';

export default {
    id: 'dh2600',
    name: 'DH2600',
    series: 'DH',
    dmiPattern: '^DH2600',
    fixtureDmi: 'DH2600',
    cpu: null,
    bays: 2,
    m2Slots: null,
    m2: null,
    lan: null,
    led: 'none',
    caseView: 'none',
    caseVariant: 'none',
    fan: null,
    verified: {
        leds: { status: 'unsupported', note: 'UGREEN has a LED driver for it (power and two bay LEDs); the plugin does not drive it yet.' },
        fans: {
            status: 'unsupported',
            note: "UGREEN's fan driver uses an IT8613E (cpu on header 3, case fan on header 2); no case view, so no fan control here."
        },
        case: { status: 'unsupported' },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12'],
    note: 'Two bays. Known from UGREEN driver sources only; the plugin shows sensors and drives nothing else on it.',
    shots: false
} satisfies Model;
