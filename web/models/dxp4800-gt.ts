import type { Model } from './schema';

export default {
    id: 'dxp4800-gt',
    name: 'DXP4800 GT',
    series: 'DXP',
    dmiPattern: '^DXP4800.*GT',
    fixtureDmi: 'DXP4800 GT',
    cpu: 'AMD Ryzen Embedded R2514',
    bays: 4,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2'], pcieSlot: false },
    lan: { maxMbit: [10000, 10000] },
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear4800',
    fan: {
        chip: 'ITE IT5571 embedded controller',
        fans: { fan1: 'cpu', fan3: 'case' },
        watchdog: { status: 'expected', note: 'Registers from the driver UGREEN ships; no armed watchdog seen on a device with this plugin yet.' }
    },
    verified: {
        leds: {
            status: 'verified',
            by: 'maintainer (tower)',
            version: '3.6',
            date: '2026-10-29',
            note: 'Through /dev/mem on the AMD DesignWare I2C controller.'
        },
        fans: {
            status: 'verified',
            by: 'maintainer (tower)',
            version: '3.6',
            date: '2026-10-29',
            note: 'CPU fan and case fan measured; the roles are not swapped.'
        },
        case: {
            status: 'verified',
            by: 'maintainer (tower)',
            version: '3.6',
            date: '2026-10-29',
            note: 'Inside view drawn from the service panel; RAM/M.2 slot numbering assumed, open an issue if your box differs.'
        },
        sensors: {
            status: 'verified',
            by: 'maintainer (tower)',
            version: '3.6',
            date: '2026-10-29',
            note: 'CPU temperature (k10temp) and board temperature from the IT5571, read while developing the support.'
        }
    },
    sources: ['https://ai.ugreen.com/products/ugreen-nasync-dxp4800-gt-4-bay-nas-storage', 'https://github.com/miskcoo/ugreen_leds_controller/issues/100'],
    note: 'Four bays (SATA or U.2), two M.2 slots, two 10G LAN ports. Fan control through the IT5571 embedded controller; LEDs through /dev/mem.',
    shots: true
} satisfies Model;
