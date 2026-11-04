import type { Model } from './schema';

export default {
    id: 'dxp8800',
    name: 'DXP8800 / DXP8800 Pro',
    series: 'DXP',
    dmiPattern: '^DXP8800( Pro)?$',
    fixtureDmi: 'DXP8800 Pro',
    cpu: null,
    bays: 8,
    m2Slots: 2,
    m2: null,
    lan: null,
    led: 'bays',
    caseView: 'front-rear',
    caseVariant: 'rear8800',
    fan: { chip: 'ITE IT8613E', fans: { fan2: 'cpu', fan3: 'case', fan4: 'case' }, kind: 'probe' },
    verified: {
        leds: { status: 'expected', note: 'Same family as the DXP8800 Plus, which works; bay order guessed. Use Identify to confirm.' },
        fans: {
            status: 'expected',
            note: "Same IT8613E as the DXP8800 Plus: the fan probe finds which output drives which fan and fan control stays off until you confirm its result. UGREEN's own driver expects cpu on header 2 and case fans on headers 3 and 4 (header 4 control register unconfirmed). Not run on this model yet."
        },
        case: { status: 'expected', note: 'Drawn like the DXP8800 Plus (eight bays, ports in a row); not compared on a device.' },
        sensors: { status: 'expected' }
    },
    sources: ['https://github.com/ugreen-opensource/kernel-6.12'],
    note: "Eight bays. UGREEN's driver sources list a plain DXP8800 and a DXP8800 Pro next to the Plus; nobody has reported either to this plugin yet. Fan control after a fan probe.",
    shots: false
} satisfies Model;
