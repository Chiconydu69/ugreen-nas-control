import type { Model } from './schema';

export default {
    id: 'dxp480t-plus',
    name: 'DXP480T Plus',
    series: 'DXP',
    dmiPattern: '^DXP480T',
    fixtureDmi: 'DXP480T Plus',
    cpu: 'Intel Core i5-1235U',
    ram: { type: 'DDR5', mt: 4800 },
    bays: 0,
    m2Slots: 4,
    m2: { slots: ['m2-1', 'm2-2', 'm2-3', 'm2-4'], pcieSlot: false },
    lan: { maxMbit: [10000] },
    led: 'power',
    caseView: 'full',
    caseVariant: 'rear480',
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
            note: "UGREEN's fan driver expects cpu on header 3 and case fans on headers 2 and 4; header 4 is unconfirmed, so there is no fan control."
        },
        case: {
            status: 'expected',
            note: "Drawn after UGREEN's manual (front, rear ports, four M.2 slots 01 to 04, two SO-DIMM sockets); which SSD sits in which slot is assumed from the PCIe order."
        },
        sensors: { status: 'expected' },
        panel: { status: 'expected' }
    },
    sources: ['https://ai.ugreen.com/products/ugreen-nasync-dxp480t-plus-nas-storage', 'https://github.com/miskcoo/ugreen_leds_controller/issues/6'],
    note: 'All-flash model: no SATA bays, four M.2 slots (shown as drives in the case view), one 10G LAN port. Only the power LED works.',
    shots: false
} satisfies Model;
