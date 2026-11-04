import type { Model } from './schema';

export default {
    id: 'dxp2800-gt',
    name: 'DXP2800 GT',
    series: 'DXP',
    dmiPattern: '^DXP2800 GT',
    fixtureDmi: 'DXP2800 GT',
    cpu: 'AMD Ryzen Embedded R2514',
    bays: 2,
    m2Slots: 2,
    m2: null,
    lan: { maxMbit: [10000] },
    led: 'bays',
    caseView: 'front-rear',
    caseVariant: 'rear2800',
    fan: { chip: 'Chipsea CS201x embedded controller', fans: { fan1: 'cpu', fan3: 'case' }, kind: 'probe', minPctFloor: 25 },
    verified: {
        leds: {
            status: 'expected',
            note: "AMD DesignWare I2C controller like the DXP4800 GT, no Intel SMBus; miskcoo's driver for it is work in progress (issues 122, 123). Other tools control all LEDs after the boot-finish write of 1 to 0x20005440 (issue 123), a user reports a month on TrueNAS; our CS201x path does the same write but nobody has run it. Bay order guessed."
        },
        fans: {
            status: 'community',
            by: 'users',
            ref: 'https://github.com/ugreen-nas-control/ugreen-nas-control/issues/95',
            note: "Fan control stays off until you run `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan cs201x confirm`. A community tester (2026-10-31, next channel) saw both fans follow the modes and a custom curve, 1834 rpm (CPU) and 603 rpm (case) at 25 %, and the BIOS speed (about 60 %) back after removing the plugin. Registers and fan roles are from UGREEN's own CS201x driver. 25 % floor, stall point not measured."
        },
        case: { status: 'expected', note: 'Drawn after the product photos, not compared on a device.' },
        sensors: { status: 'expected' }
    },
    sources: ['https://ai.ugreen.com/products/ugreen-nasync-dxp2800-gt-2-bay-nas-storage', 'https://github.com/miskcoo/ugreen_leds_controller/issues/122'],
    note: 'Two bays (U.2 capable), two M.2 slots, one 10G LAN port. LEDs are enabled but nobody has confirmed them. Case view (front, rear) after the product photos; fan control after `/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan cs201x confirm` (Chipsea CS201x embedded controller; wiring from the UGREEN driver, confirmed on a device by a community tester).',
    shots: true
} satisfies Model;
