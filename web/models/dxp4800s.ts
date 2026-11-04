import type { Model } from './schema';

export default {
    id: 'dxp4800s',
    name: 'DXP4800S',
    series: 'DXP',
    dmiPattern: '^DXP4800S$',
    fixtureDmi: 'DXP4800S',
    cpu: 'Intel N150',
    bays: 4,
    m2Slots: 2,
    m2: null,
    lan: null,
    led: 'bays',
    caseView: 'none',
    caseVariant: 'none',
    fan: null,
    verified: {
        leds: {
            status: 'expected',
            note: 'Same family as the DXP4800; the name DXP4800S appears in the driver sources of UGREEN, the LED frame is not confirmed.'
        },
        fans: { status: 'unsupported' },
        case: { status: 'unsupported' },
        sensors: { status: 'expected' }
    },
    sources: ['https://nascompares.com/2025/11/14/new-ugreen-dxp4800-pro-and-dxp4800s-nas-revealed/'],
    note: 'Four bays, two M.2 slots. Sources disagree on the LAN ports (one or two 2.5G). Nobody has reported this model yet.',
    shots: false
} satisfies Model;
