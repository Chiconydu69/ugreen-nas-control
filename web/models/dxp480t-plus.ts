import type { Model } from './schema';

export default {
    id: 'dxp480t-plus',
    name: 'DXP480T Plus',
    series: 'DXP',
    dmiPattern: '^DXP480T',
    fixtureDmi: 'DXP480T Plus',
    cpu: 'Intel Core i5-1235U',
    bays: 0,
    m2Slots: 4,
    m2: null,
    lan: { maxMbit: [10000] },
    led: 'power',
    caseView: 'none',
    caseVariant: 'none',
    fan: null,
    verified: {
        leds: {
            status: 'partial',
            by: 'users',
            ref: 'https://github.com/miskcoo/ugreen_leds_controller/issues/6',
            note: 'Only the power LED works; the model has no drive bays.'
        },
        fans: {
            status: 'unsupported',
            note: "UGREEN's fan driver expects cpu on header 3 and case fans on headers 2 and 4; header 4 is unconfirmed, so there is no fan control and no case view."
        },
        case: { status: 'unsupported' },
        sensors: { status: 'expected' }
    },
    sources: ['https://ai.ugreen.com/products/ugreen-nasync-dxp480t-plus-nas-storage', 'https://github.com/miskcoo/ugreen_leds_controller/issues/6'],
    note: 'All-flash model: no SATA bays, four M.2 slots, one 10G LAN port. Only the power LED works.',
    shots: false
} satisfies Model;
