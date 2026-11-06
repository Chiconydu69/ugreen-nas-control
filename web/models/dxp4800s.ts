import type { Model } from './schema';

export default {
    id: 'dxp4800s',
    name: 'DXP4800S',
    series: 'DXP',
    dmiPattern: '^DXP4800S$',
    fixtureDmi: 'DXP4800S',
    cpu: 'Intel N150',
    ram: { type: 'DDR5', mt: 5600 },
    bays: 4,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: false },
    lan: null,
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear4800',
    fan: null,
    verified: {
        leds: {
            status: 'expected',
            note: 'Same family as the DXP4800; the name DXP4800S appears in the driver sources of UGREEN, the LED frame is not confirmed.'
        },
        fans: { status: 'unsupported' },
        case: {
            status: 'expected',
            note: "Drawn after the DXP4800 Plus (UGREEN's chassis, front and back from NASCompares' photos of the Plus; the S itself was not photographed). One RAM slot as the reveal article lists it; slot numbering assumed. The rear draws one or two 2.5G LAN ports as the system finds them."
        },
        sensors: { status: 'expected' }
    },
    sources: ['https://nascompares.com/2025/11/14/new-ugreen-dxp4800-pro-and-dxp4800s-nas-revealed/'],
    note: 'Four bays, two M.2 slots. Sources disagree on the LAN ports (one or two 2.5G); the rear shows as many as the system finds. Nobody has reported this model yet.',
    shots: false
} satisfies Model;
