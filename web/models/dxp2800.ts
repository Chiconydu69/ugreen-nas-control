import type { Model } from './schema';

export default {
    id: 'dxp2800',
    name: 'DXP2800',
    series: 'DXP',
    dmiPattern: '^DXP2800$',
    fixtureDmi: 'DXP2800',
    cpu: 'Intel N100',
    ram: { type: 'DDR5', mt: 4800 },
    bays: 2,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: false },
    lan: { maxMbit: [2500] },
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear2800',
    fan: {
        chip: 'ITE IT8613E',
        fans: { fan3: 'case' },
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
            note: "Reported working by users. Boards from December 2025 (V1.2, rev 251215) use LED chip 0xc5b2, which ignores the DX/DXP write frame (miskcoo issue 128). The driver UGREEN ships serves this model with the frame without LED ID, so the plugin tries that frame when the first one is not acknowledged. An owner confirmed the LEDs of a V1.2 board with miskcoo's tool and that SMBus block frame (issue 128; his README marks V1.2 experimental); not yet run with this plugin."
        },
        fans: {
            status: 'expected',
            note: "Same IT8613E as the DXP6800 Pro, header wiring not known: the fan probe finds which output drives which fan and fan control stays off until you confirm its result. Not run on this model yet. One fan was reported on tach 3 (1726 rpm), none on tach 2; UGREEN's own driver also drives only header 3."
        },
        case: {
            status: 'expected',
            note: "Drawn after UGREEN's manual and photos (front, rear with 12V inlet, two M.2 slots on the inner left wall of the cage); one SO-DIMM socket in the bottom panel; M.2 slot numbering assumed. Not compared on a device."
        },
        sensors: { status: 'expected' }
    },
    sources: [
        'https://datacenterdisk.com/nas-devices/ugreen',
        'https://nascompares.com/2024/02/05/the-ugreen-nasync-dxp-nas-series-should-you-buy/',
        'https://github.com/miskcoo/ugreen_leds_controller/issues/128',
        'https://github.com/miskcoo/ugreen_leds_controller/issues/133',
        'https://github.com/IT-Kuny/UGREEN-DXP-FAN-NAS-Driver'
    ],
    note: "Two bays, two M.2 slots, one 2.5G LAN port. The V1.2 board (December 2025) has a new LED chip: the plugin falls back to the frame UGREEN ships for it, confirmed on a V1.2 board with miskcoo's tool, not yet with this plugin.",
    shots: true
} satisfies Model;
