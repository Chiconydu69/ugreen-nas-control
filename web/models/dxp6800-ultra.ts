import type { Model } from './schema';

export default {
    id: 'dxp6800-ultra',
    name: 'DXP6800 Ultra',
    series: 'DXP',
    dmiPattern: '^DXP6800 Ultra',
    fixtureDmi: 'DXP6800 Ultra',
    cpu: 'Intel Core 5 120U',
    ram: { type: 'DDR5', mt: 5600 },
    bays: 6,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2', 'system'], pcieSlot: true },
    lan: { maxMbit: [10000, 10000] },
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear6800',
    fan: null,
    verified: {
        leds: { status: 'expected', note: 'Bay order guessed.' },
        fans: { status: 'unsupported', note: 'Other CPU than the DXP6800 Pro: its fan wiring is not proven for this model.' },
        case: {
            status: 'expected',
            note: "Front and rear after UGREEN's product renders. No photo of the inside exists: drawn after the DXP6800 Pro, slot positions and numbering assumed."
        },
        sensors: { status: 'expected' },
        panel: { status: 'unsupported', note: "Newer than UGREEN's driver source, which names no button or beeper path for it." }
    },
    sources: [
        'https://ai.ugreen.com/products/ugreen-nasync-dxp6800-ultra-nas-storage',
        'https://nascompares.com/2026/06/22/ugreen-dxp6800-ultra-and-dxp8800-ultra-nas-revealed/'
    ],
    note: 'Six bays, two M.2 slots, two 10G LAN ports and two Thunderbolt 4 ports. LEDs are enabled but nobody has confirmed them.',
    shots: true
} satisfies Model;
