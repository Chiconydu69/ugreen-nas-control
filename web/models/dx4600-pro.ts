import type { Model } from './schema';

export default {
    id: 'dx4600-pro',
    name: 'DX4600 Pro',
    series: 'DX',
    dmiPattern: '^DX4600',
    fixtureDmi: 'DX4600 Pro',
    cpu: 'Intel Celeron N6005',
    ram: { type: 'DDR4', mt: 3200 },
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
            by: 'miskcoo',
            ref: 'https://github.com/miskcoo/ugreen_leds_controller',
            note: "LED protocol verified on this model by miskcoo's driver; bay order ata1 to ata4. Not yet reported for this plugin."
        },
        fans: { status: 'unsupported' },
        case: {
            status: 'expected',
            note: "Front, rear and drive cage after UGREEN's product renders; the inside shows no board there, so it is drawn like the DXP4800's (RAM and M.2 numbering assumed)."
        },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/miskcoo/ugreen_leds_controller', 'https://www.ugnas.com/products-detail/id-29.html'],
    note: 'First-generation four-bay model, no longer listed on the UGREEN store. Two 2.5G LAN ports, two M.2 slots, two DDR4 sockets. Case view; no fan control.',
    shots: true
} satisfies Model;
