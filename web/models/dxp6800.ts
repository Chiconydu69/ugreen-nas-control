import type { Model } from './schema';

export default {
    id: 'dxp6800',
    name: 'DXP6800 / DXP6800 Plus',
    series: 'DXP',
    dmiPattern: '^DXP6800( Plus|\\+)?$',
    fixtureDmi: 'DXP6800 Plus',
    cpu: null,
    bays: 6,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2', 'system'], pcieSlot: true },
    lan: null,
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear6800',
    fan: { chip: 'ITE IT8613E', fans: { fan2: 'cpu', fan3: 'case', fan4: 'case' }, kind: 'probe' },
    verified: {
        leds: {
            status: 'expected',
            note: "UGREEN's LED driver lists the plain DXP6800 with six bay LEDs, but with another frame than the one that works on the DXP6800 Pro; the plugin has not been tried on this model. Use Identify to confirm the bay order."
        },
        fans: {
            status: 'expected',
            note: "Same IT8613E as the DXP6800 Pro. UGREEN's own fan driver wires the whole DXP6800 family like the Pro (cpu on header 2, case fans on headers 3 and 4; header 4 control register unconfirmed), but this model is probed first: fan control stays off until you confirm the result of the fan probe. Not run on this model yet."
        },
        case: {
            status: 'expected',
            note: 'Drawn like the DXP6800 Pro, as the plugin does for every model without a drawing of its own; not compared on a device.'
        },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12'],
    note: "Six bays. UGREEN's driver sources list the DXP6800 and a DXP6800+; nobody has reported either to this plugin yet. Fan control after a fan probe.",
    shots: false
} satisfies Model;
