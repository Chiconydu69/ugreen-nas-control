import type { Model } from './schema';

export default {
    id: 'dxp4800-base',
    name: 'DXP4800',
    series: 'DXP',
    dmiPattern: '^DXP4800$',
    fixtureDmi: 'DXP4800',
    cpu: 'Intel N100',
    bays: 4,
    m2Slots: 2,
    m2: null,
    lan: { maxMbit: [2500] },
    led: 'bays',
    caseView: 'front-rear',
    caseVariant: 'rear4800',
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
        leds: { status: 'community', by: 'users', note: 'LED support confirmed by users of the DXP4800 family.' },
        fans: {
            status: 'expected',
            note: "Same IT8613E as the DXP6800 Pro, header wiring not known: the fan probe finds which output drives which fan and fan control stays off until you confirm its result. Not run on this model yet. UGREEN's own driver drives only header 3 here, no cpu fan output."
        },
        case: { status: 'expected', note: 'The rear is drawn after the Plus and shows two LAN ports; this model has one 2.5G port.' },
        sensors: { status: 'expected' }
    },
    sources: ['https://nascompares.com/2025/11/14/new-ugreen-dxp4800-pro-and-dxp4800s-nas-revealed/'],
    note: 'Four bays, two M.2 slots, one 2.5G LAN port. Fan control after a fan probe.',
    shots: false
} satisfies Model;
