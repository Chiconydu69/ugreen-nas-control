import type { Model } from './schema';

export default {
    id: 'dh2600',
    name: 'DH2600',
    series: 'DH',
    dmiPattern: '^DH2600',
    fixtureDmi: 'DH2600',
    cpu: 'Intel Celeron N5105',
    ram: { type: 'DDR4', mt: 2933 },
    bays: 2,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: false },
    lan: { maxMbit: [2500] },
    led: 'none',
    caseView: 'full',
    caseVariant: 'rear2600',
    fan: null,
    verified: {
        leds: { status: 'unsupported', note: 'UGREEN has a LED driver for it (power and two bay LEDs); the plugin does not drive it yet.' },
        fans: {
            status: 'unsupported',
            note: "UGREEN's fan driver uses an IT8613E (cpu on header 3, case fan on header 2); the plugin does not drive it."
        },
        case: {
            status: 'expected',
            note: "Front, rear and inside after UGREEN's product renders (two M.2 on the side of the drive cage, two RAM sockets on the mainboard below); slot numbering and the bay port order are assumed. The photos show one SO-DIMM; the second RAM socket is taken from UGREEN's spec text."
        },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12', 'https://www.ugnas.com/products-detail/id-31.html'],
    note: "Two bays, two M.2 slots, one 2.5G LAN port. Case view after UGREEN's product renders; sensors only, the plugin drives nothing else on it.",
    shots: false
} satisfies Model;
