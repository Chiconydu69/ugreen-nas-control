import type { Model } from './schema';

export default {
    id: 'dxp4800',
    name: 'DXP4800 Plus / Pro',
    series: 'DXP',
    dmiPattern: '^DXP4800 (Plus|Pro)$',
    fixtureDmi: 'DXP4800 Plus',
    cpu: 'Intel Pentium Gold 8505 (Plus), Core i3-1315U (Pro)',
    bays: 4,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: false },
    lan: { maxMbit: [10000, 2500] },
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear4800',
    fan: {
        chip: 'ITE IT8613E',
        fans: { fan2: 'cpu', fan3: 'case' },
        kind: 'probe',
        watchdog: {
            status: 'expected',
            note: 'Expected, from the vendor driver and the kernel it87_wdt: the plugin only feeds a timer it sees counting, never arms it, and yields to it87_wdt. Not confirmed on this hardware.'
        }
    },
    verified: {
        leds: {
            status: 'community',
            by: 'users',
            ref: 'https://github.com/miskcoo/ugreen_leds_controller/issues/121',
            note: 'Reported working by users of the Plus and the Pro (miskcoo issue 121).'
        },
        fans: {
            status: 'expected',
            note: "Same IT8613E as the DXP6800 Pro, header wiring not known: the fan probe finds which output drives which fan and fan control stays off until you confirm its result. Not run on this model yet. The Plus was reported to use outputs 2 and 3; UGREEN's own driver expects cpu on header 2 and case on header 3."
        },
        case: {
            status: 'community',
            by: 'ivolution (issue 184)',
            version: '5.2.0',
            date: '2026-11-05',
            ref: 'https://github.com/ugreen-nas-control/ugreen-nas-control/issues/184',
            note: "Front and rear confirmed by a Plus owner. The two M.2 slots of the underside panel are printed M2-KEYM1 (next to the RAM) and M2-KEYM3 on the board, shown as M.2-1 and M.2-3 (UGREEN's photos agree); the third slot, M2-KEYM2, is inside the case. Inside view otherwise after UGREEN's manual, the Pro was not compared."
        },
        sensors: { status: 'expected' }
    },
    sources: [
        'https://ai.ugreen.com/products/ugreen-nasync-dxp4800-plus-nas-storage',
        'https://ai.ugreen.com/products/ugreen-nasync-dxp4800-pro-4-bay-nas',
        'https://nascompares.com/2025/11/14/new-ugreen-dxp4800-pro-and-dxp4800s-nas-revealed/',
        'https://github.com/smashah/ugreen-dxp-fan-cli'
    ],
    note: 'Four bays, two M.2 slots, one 10G and one 2.5G LAN port. Fan control after a fan probe.',
    shots: true
} satisfies Model;
