import type { Model } from './schema';

export default {
    id: 'dx4700',
    name: 'DX4700+',
    series: 'DX',
    dmiPattern: '^DX4700',
    fixtureDmi: 'DX4700+',
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
            by: 'users',
            ref: 'https://github.com/miskcoo/ugreen_leds_controller/issues/22',
            note: "Reported working by users of miskcoo's driver."
        },
        fans: { status: 'unsupported' },
        case: { status: 'unsupported' },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/miskcoo/ugreen_leds_controller/issues/22'],
    note: 'Four bays, no longer listed on the UGREEN store; the board reports its name as DX4700+. No case view or fan control.',
    shots: true
} satisfies Model;
