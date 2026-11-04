import type { Model } from './schema';

export default {
    id: 'dx4600-pro',
    name: 'DX4600 Pro',
    series: 'DX',
    dmiPattern: '^DX4600',
    fixtureDmi: 'DX4600 Pro',
    cpu: null,
    bays: 4,
    m2Slots: null,
    m2: null,
    lan: null,
    led: 'bays',
    caseView: 'none',
    caseVariant: 'none',
    fan: null,
    verified: {
        leds: {
            status: 'community',
            by: 'miskcoo',
            ref: 'https://github.com/miskcoo/ugreen_leds_controller',
            note: "LED protocol verified on this model by miskcoo's driver; bay order ata1 to ata4. Not yet reported for this plugin."
        },
        fans: { status: 'unsupported' },
        case: { status: 'unsupported' },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/miskcoo/ugreen_leds_controller'],
    note: 'First-generation four-bay model, no longer listed on the UGREEN store. No case view or fan control.',
    shots: true
} satisfies Model;
