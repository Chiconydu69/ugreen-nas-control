import type { Model } from './schema';

export default {
    id: 'dxp8800-ultra',
    name: 'DXP8800 Ultra',
    series: 'DXP',
    dmiPattern: '^DXP8800 Ultra',
    fixtureDmi: 'DXP8800 Ultra',
    cpu: 'Intel Core 7 150U',
    bays: 8,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: true },
    lan: { maxMbit: [10000, 10000] },
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear8800',
    fan: null,
    verified: {
        leds: { status: 'expected', note: 'Bay order guessed.' },
        fans: { status: 'unsupported' },
        case: {
            status: 'expected',
            note: "Front and rear after UGREEN's product renders. No photo of the inside exists: drawn after the DXP8800 Plus, slot positions and numbering assumed."
        },
        sensors: { status: 'expected' }
    },
    sources: [
        'https://ai.ugreen.com/products/ugreen-nasync-dxp8800-ultra-nas-storage',
        'https://nascompares.com/2026/06/22/ugreen-dxp6800-ultra-and-dxp8800-ultra-nas-revealed/'
    ],
    note: 'Eight bays, two M.2 slots, two 10G LAN ports and two Thunderbolt 4 ports. LEDs are enabled but nobody has confirmed them.',
    shots: true
} satisfies Model;
