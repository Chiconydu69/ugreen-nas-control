// API answers of a DXP6800 Pro with the LED part switched on, for tests that run a page script.
import { fanConfig } from '../../models/fixture';
import type { HardwareResponse, LedBay, LedGet, StatusResponse } from '../core/contract';

export const status: StatusResponse = {
    ok: true,
    info: {
        model: 'DXP6800 Pro',
        bios: 'BIOS 1.2.3',
        bios_date: '03/24/2024',
        cpu: '12th Gen Intel(R) Core(TM) i5-1235U @ 2.50GHz',
        threads: 12,
        memory_kb: 16_000_000,
        kernel: '6.1.64-Unraid',
        unraid: '6.12.6',
        uptime: 300_000
    },
    sensors: {
        cpu: 52,
        cpu_cores: [
            { name: 'Core 0', temp: 50 },
            { name: 'Core 1', temp: 54 }
        ],
        nvme: [
            { name: 'nvme0', label: 'Samsung 970', temp: 41 },
            { name: 'nvme1', label: 'WD SN770', temp: 63 }
        ],
        nvme_extra: [{ name: 'nvme1', label: 'Sensor 1', temp: 66 }],
        nic: [
            { iface: 'eth0', label: 'PHY', temp: 58 },
            { iface: 'eth0', label: 'MAC', temp: 61 }
        ],
        gpu: 47,
        disks: [
            { name: 'disk1', device: 'sda', id: 'WDC_WD40_X1', temp: 38, standby: false, hdd: true },
            { name: 'parity', device: 'sdb', id: 'WDC_WD40_X2', temp: null, standby: true, hdd: true },
            { name: 'flash', device: 'sdc', temp: null, standby: false, hdd: false }
        ],
        board: 44
    },
    presets: {
        quiet: ['50:15,65:25,75:45,85:80,92:100', '35:20,40:25,45:40,50:70,55:100'],
        standard: ['45:25,60:35,70:55,80:80,88:100', '30:25,38:30,44:50,50:80,55:100'],
        performance: ['40:35,55:50,65:70,75:90,85:100', '30:35,36:45,42:65,48:90,52:100'],
        full: ['0:100', '0:100']
    },
    net_io: [
        { name: 'eth0', rx: 1_200_000_000, tx: 300_000_000 },
        { name: 'eth1', rx: 0, tx: 0 }
    ],
    fan_control: true,
    chip: 'IT5571',
    fans: [
        { id: 'fan2', role: 'cpu', rpm: 1500, pct: 35, auto: false },
        { id: 'fan3', role: 'case', rpm: 900, pct: 30, auto: false },
        { id: 'fan4', role: 'case', rpm: 0, pct: 30, auto: false }
    ],
    chip_temps: [42, null],
    calibration: {
        table: {
            time: 1_761_600_000,
            fans: {
                fan2: [
                    [0, 0],
                    [20, 700],
                    [50, 2000],
                    [100, 4000]
                ],
                fan3: [
                    [0, 0],
                    [25, 300],
                    [100, 1700]
                ]
            }
        },
        progress: null
    },
    daemon: {
        time: 1_761_600_000,
        mode: 'standard',
        failsafe: '',
        cpu_temp: 52,
        case_temp: 38,
        case_from: 'disk',
        case_driver: 'case',
        targets: { fan2: 35, fan3: 30, fan4: 30 },
        stalled: ['fan4'],
        chip_temps: [42, null]
    },
    fan_caps: { kind: 'fixed', channels: 3, watchdog: false, backlight: false },
    watchdog: null,
    running: true,
    version: '3.5',
    led: { enabled: true, supported: true, blocker: '', warning: '', untested: false, import_available: false, model: 'DXP6800 Pro' },
    settings: {
        mode: 'standard',
        cpu_curve: '',
        case_curve: '',
        min_pct: 25,
        cpu_crit: 90,
        disk_crit: 55,
        nvme_offset: 15,
        stall_notify: true,
        fan_control: true,
        case_follows_cpu: false,
        ignore_drives: ['nvme1'],
        ui_lang: 'auto',
        ui_theme: 'dark',
        fan_config: fanConfig()
    }
};

export const hardware: HardwareResponse = {
    ok: true,
    model: 'DXP6800 Pro',
    memory: [
        { locator: 'DIMM A', size_mb: 16384, type: 'DDR5', speed_mt: 4800, configured_mt: 4800, manufacturer: 'Kingston', part_number: 'KF548' },
        { locator: 'DIMM B', size_mb: 0, type: '', speed_mt: 0, configured_mt: 0, manufacturer: '', part_number: '' }
    ],
    nvme: [
        { id: 'nvme0', model: 'Samsung SSD 970 EVO', size_bytes: 500_107_862_016, slot: 'm2-1', pci: '0000:01:00.0', temp: 41, role: 'cache', boot: false },
        { id: 'nvme1', model: 'WD SN770', size_bytes: 1_000_204_886_016, slot: 'system', pci: '0000:02:00.0', temp: 63, boot: true }
    ],
    bays: [
        {
            bay: 1,
            present: true,
            device: 'sda',
            model: 'WDC WD40',
            size_bytes: 4_000_787_030_016,
            hdd: true,
            role: 'disk1',
            id: 'WDC_WD40_X1',
            temp: 38,
            standby: false
        },
        {
            bay: 2,
            present: true,
            device: 'sdb',
            model: 'WDC WD40',
            size_bytes: 4_000_787_030_016,
            hdd: true,
            role: 'parity',
            id: 'WDC_WD40_X2',
            temp: null,
            standby: true
        },
        { bay: 3, present: false, hdd: false, temp: null, standby: false }
    ],
    net: [
        { name: 'eth0', link: true, speed_mbit: 10000, max_mbit: 10000 },
        { name: 'eth1', link: false, speed_mbit: 0, max_mbit: 10000 }
    ]
};

export const ledBays: LedBay[] = [
    {
        bay: 1,
        led: 'disk1',
        device: 'sda',
        serial: 'S1',
        slot: 'Bay 1',
        status: 'active',
        error: false,
        standby: false,
        temp: 38,
        errors: 0,
        temp_hot: 50,
        temp_max: 60,
        smart_now: '',
        smart_key: '',
        smart_warnings: ''
    },
    {
        bay: 2,
        led: 'disk2',
        device: 'sdb',
        serial: 'S2',
        slot: 'Bay 2',
        status: 'standby',
        error: false,
        standby: true,
        temp: null,
        errors: 2,
        temp_hot: 50,
        temp_max: 60,
        smart_now: '5=8',
        smart_key: 'k',
        smart_warnings: '5=8'
    },
    {
        bay: 3,
        led: 'disk3',
        device: '',
        serial: '',
        slot: '',
        status: '',
        error: false,
        standby: false,
        temp: null,
        errors: 0,
        temp_hot: 50,
        temp_max: 60,
        smart_now: '',
        smart_key: '',
        smart_warnings: ''
    }
];

const led = (color: string, brightness: string, effect: string, on = '0', off = '0') => ({
    color,
    brightness,
    max_brightness: '255',
    effect,
    on_ms: on,
    off_ms: off
});

export const ledGet: LedGet = {
    status: {
        ok: true,
        foreign_monitor: false,
        model: 'DXP6800 Pro',
        bays: 3,
        kernel: 'ok',
        mapping: 'tested',
        mode: 'status',
        daemon: true,
        fx: '',
        fx_leds: '',
        fx_speed: 2,
        fx_reverse: false,
        fx_idle: false,
        identify: ['disk2'],
        sync_action: 'check',
        sync_percent: 40,
        sync_enabled: true,
        sync_color: '0 120 255',
        standby_mode: 'dim',
        standby_level: 30,
        smart_enabled: true,
        error_enabled: true,
        activity_enabled: true,
        smart_color: '255 140 0',
        disk_color_mode: 'own',
        netdev_status: 'activity',
        alert_enabled: true,
        alert_level: 'warning',
        alert_color: '255 59 48',
        shutdown_blink: true,
        shutdown_color: '255 140 0',
        shutdown_effect: 'blink',
        alert_count: 2,
        alert_active: false,
        night: true,
        disk_error_color: '255 59 48',
        saved_brightness: { power: 255, disk1: 200 },
        network_device: 'eth0',
        netdev_speed: 10000,
        network_ports: [
            { name: 'eth0', link: true, speed: 10000, max: 10000 },
            { name: 'eth1', link: false, speed: 0, max: 2500 }
        ],
        netdev_speed_enabled: true,
        netdev_speed_colors: { '10': '255 59 48', '10000': '0 120 255' },
        controller: 'ugreen-led-i2c',
        i2c_bus: 1,
        i2c_address: '0x3a',
        i2c_probe: 'ok',
        leds: ['power', 'netdev', 'disk1', 'disk2', 'disk3'],
        settings_saved: true
    },
    bays: { ok: true, bays: ledBays },
    all: {
        ok: true,
        leds: {
            power: led('255 255 255', '255', 'none'),
            netdev: led('0 200 83', '128', 'blink', '100', '100'),
            disk1: led('255 160 0', '200', 'none'),
            disk2: led('255 160 0', '0', 'none'),
            disk3: led('0 0 255', '255', 'breath', '1200', '900')
        }
    },
    schedule: { ok: true, enabled: true, start: '22:00', end: '07:00', action: 'dim', brightness: 20 }
};

/** What the page scripts get for a request URL. */
export function answer(url: string): unknown {
    const action = new URL(url, 'http://nas').searchParams.get('action');
    const led = url.includes('/led/');

    if (led) return ledGet[action as keyof LedGet];
    return action === 'hardware' ? hardware : status;
}
