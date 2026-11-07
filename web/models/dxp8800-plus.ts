import type { Model } from './schema';

export default {
    id: 'dxp8800-plus',
    name: 'DXP8800 Plus',
    series: 'DXP',
    dmiPattern: '^DXP8800 Plus$',
    fixtureDmi: 'DXP8800 Plus',
    cpu: 'Intel Core i5-1235U',
    ram: { type: 'DDR5', mt: 4800 },
    bays: 8,
    m2Slots: 2,
    m2: { slots: ['m2-1', 'm2-2', 'system'], pcieSlot: true },
    lan: { maxMbit: [10000, 10000] },
    led: 'bays',
    caseView: 'full',
    caseVariant: 'rear8800',
    fan: {
        chip: 'ITE IT8613E',
        fans: { fan2: 'cpu', fan3: 'case', fan4: 'case' },
        kind: 'probe',
        watchdog: {
            status: 'expected',
            note: 'Expected, from the vendor driver and the kernel it87_wdt: the plugin only feeds a timer it sees counting, never arms it, and yields to it87_wdt. Not confirmed on this hardware.'
        }
    },
    verified: {
        leds: {
            status: 'community',
            by: 'miskcoo and users',
            ref: 'https://github.com/miskcoo/ugreen_leds_controller',
            note: "LED protocol verified on this model by miskcoo's driver, confirmed by users of this plugin."
        },
        fans: {
            status: 'community',
            by: 'Chiconydu69 (issue 165)',
            version: '5.2.0',
            date: '2026-11-05',
            ref: 'https://github.com/ugreen-nas-control/ugreen-nas-control/issues/165',
            note: "Fan probe found cpu on pwm2 and the case fans on pwm3 and pwm4; fan control with custom curves confirmed by a user. On 5.2.0 header 4 (pwm4) stayed in automatic mode in the diag; it now uses the control register 0x1e of UGREEN's driver, to be confirmed."
        },
        case: {
            status: 'community',
            by: 'Chiconydu69 (issue 165)',
            version: '5.2.0',
            date: '2026-11-06',
            ref: 'https://github.com/ugreen-nas-control/ugreen-nas-control/issues/165',
            note: "M.2-1, M.2-2 and the internal system slot confirmed by the owner's root-port report (same root ports as the DXP6800 Pro). The PCIe slot port is carried over from the DXP6800 Pro."
        },
        sensors: {
            status: 'community',
            by: 'users',
            version: '5.1.0',
            date: '2026-11-05',
            ref: 'https://github.com/ugreen-nas-control/ugreen-nas-control/issues/165',
            note: 'Temperatures and the dashboard tile confirmed by a user; a one-off -21 °C board reading from the ACPI thermal zone is ignored since 5.1.1.'
        }
    },
    sources: ['https://ai.ugreen.com/products/ugreen-nasync-dxp8800-plus-nas-storage'],
    note: 'Eight bays, two M.2 slots, two 10G LAN ports and two Thunderbolt 4 ports. Fan control after a fan probe.',
    shots: true
} satisfies Model;
