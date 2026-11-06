import type { Model } from './schema';

export default {
    id: 'dx4700',
    name: 'DX4700+',
    series: 'DX',
    dmiPattern: '^DX4700',
    fixtureDmi: 'DX4700+',
    cpu: null,
    ram: null,
    bays: 4,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: false },
    lan: { maxMbit: [2500, 2500] },
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear4600',
    fan: null,
    verified: {
        leds: {
            status: 'community',
            by: 'users',
            ref: 'https://github.com/miskcoo/ugreen_leds_controller/issues/22',
            note: "Reported working by users of miskcoo's driver."
        },
        fans: { status: 'unsupported' },
        case: {
            status: 'expected',
            note: "No photos or manual of this model found: drawn after the DX4600 Pro (four bays, two 2.5G LAN ports, two M.2 slots, same back); this model's hardware is not known."
        },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/miskcoo/ugreen_leds_controller/issues/22'],
    note: "Four bays, no longer listed on the UGREEN store; the board reports its name as DX4700+. Case view drawn after the DX4600 Pro, as this model's hardware is not known. No fan control.",
    shots: true,
    fixture: { cpu: 'Intel Pentium Silver N6005', ram: { type: 'DDR4', mt: 3200 } }
} satisfies Model;
