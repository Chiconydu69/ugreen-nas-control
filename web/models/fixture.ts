// Sample data the harness serves for a model: the shapes of `ugreen-nas-fan` (status, hardware) and
// `ugreen-led-ctl` (status, all, bays, list). Fixed values, no serial numbers; the plugin shows them as if from a NAS.
import type { FanConfigV2, FanGroupConfig } from '../src/core/contract';
import { caseModel } from '../src/features/nas/components/front';
import type { Model } from './schema';

const GB = 1e9;

interface BayRow {
    role?: string;
    id?: string;
    model?: string;
    size: number;
    hdd: boolean;
    temp: number | null;
    standby?: boolean;
}

const HDD = (role: string, id: string, temp: number | null, standby = false): BayRow => ({
    role,
    id,
    model: 'ST8000VN004',
    size: 8000 * GB,
    hdd: true,
    temp,
    standby
});
const SSD = (role: string, id: string, temp: number): BayRow => ({ role, id, model: 'Samsung SSD 870 EVO 4TB', size: 4000 * GB, hdd: false, temp });

function bayRows(n: number): (BayRow | null)[] {
    const rows: (BayRow | null)[] = [HDD('disk1', 'a', 36), HDD('disk2', 'b', 47), HDD('parity', 'c', null, true)];
    if (n === 2) return [HDD('disk1', 'a', 36), HDD('parity', 'c', null, true)];
    if (n === 4) return [...rows, SSD('cache', 'd', 33)];
    if (n === 8) return [...rows, HDD('disk3', 'e', 41), SSD('cache', 'd', 33), HDD('disk4', 'f', 39), null, null];
    return [...rows, null, SSD('cache', 'd', 33), null];
}

const bayCount = (m: Model) => m.fixture?.ledBays ?? (m.bays || 4);

export function hardware(m: Model) {
    const rows = bayRows(bayCount(m));
    const bays = rows.map((r, i) =>
        r
            ? {
                  bay: i + 1,
                  present: true,
                  device: `sd${'cdefghij'[i]}`,
                  model: r.model,
                  size_bytes: r.size,
                  hdd: r.hdd,
                  role: r.role,
                  id: r.id,
                  temp: r.temp,
                  standby: Boolean(r.standby)
              }
            : { bay: i + 1, present: false, hdd: false }
    );
    const slots = m.m2?.slots ?? [];
    const second = slots.includes('system') ? 'system' : (slots[1] ?? 'm2');
    const nvmeSlots = m.caseView === 'none' ? ['pcie', 'pcie'] : [slots[0] ?? 'm2', second];
    const ports = m.lan?.maxMbit ?? [2500];
    // All flash: the four M.2 slots stand in for the bays (the plugin lists them by PCIe address, front.ts withM2Bays).
    const flash = !m.bays && m.caseView !== 'none';
    const flashNvme = [
        { id: 'nvme0', model: 'WD Red SN700 1000GB', size_bytes: 1000 * GB, slot: 'm2', pci: '0000:01:00.0', temp: 38, role: 'disk1', boot: false },
        { id: 'nvme1', model: 'WD Red SN700 1000GB', size_bytes: 1000 * GB, slot: 'm2', pci: '0000:02:00.0', temp: 41, role: 'parity', boot: false },
        { id: 'nvme2', model: 'Samsung SSD 990 PRO 2TB', size_bytes: 2000 * GB, slot: 'm2', pci: '0000:03:00.0', temp: 47, role: 'cache', boot: false }
    ];
    return {
        ok: true,
        model: m.fixtureDmi,
        memory: ['D0', 'D1'].slice(0, caseModel({ model: m.fixtureDmi }).ramSlots ?? 2).map(locator => ({
            locator,
            size_mb: 32768,
            type: ram(m).type,
            speed_mt: ram(m).mt,
            configured_mt: ram(m).mt,
            manufacturer: 'Micron',
            part_number: 'CT32G56C46S5'
        })),
        nvme: flash
            ? flashNvme
            : m.id === 'dxp8800-plus'
              ? // The first report (issue 165): three SSDs and no root-port map, so all three are "m2".
                [
                    {
                        id: 'nvme0',
                        model: 'Samsung SSD 990 PRO 2TB',
                        size_bytes: 2000 * GB,
                        slot: 'm2',
                        pci: '0000:01:00.0',
                        temp: 41,
                        role: 'cache',
                        boot: false
                    },
                    { id: 'nvme1', model: 'WD Red SN700 1000GB', size_bytes: 1000 * GB, slot: 'm2', pci: '0000:56:00.0', temp: 33, role: 'pool1', boot: false },
                    { id: 'nvme2', model: 'WD Red SN700 1000GB', size_bytes: 1000 * GB, slot: 'm2', pci: '0000:57:00.0', temp: 32, role: '', boot: true }
                ]
              : [
                    {
                        id: 'nvme3',
                        model: 'Samsung SSD 990 PRO 2TB',
                        size_bytes: 2000 * GB,
                        slot: nvmeSlots[0],
                        pci: 'x',
                        temp: 44,
                        role: 'cache',
                        boot: false
                    },
                    { id: 'nvme1', model: 'WD Red SN700 1000GB', size_bytes: 1000 * GB, slot: nvmeSlots[1], pci: 'y', temp: 38, role: '', boot: true }
                ],
        // The plugin knows the bays of the models with a case view only (backend/internal/fan/hardware.go layoutFor).
        bays: m.caseView === 'none' || flash ? [] : bays,
        net: ports.map((max, i) => ({ name: `eth${i}`, link: i === 0, speed_mbit: i === 0 ? (max ?? 2500) : 0, max_mbit: max ?? 0 }))
    };
}

type Rival = { kind: string; id: string; name: string; controls: string[]; severity: string; detail?: string };

const FAN_RIVALS: Rival[] = [
    { kind: 'daemon', id: 'fanctrlplus2_loop.sh', name: 'FanCtrl Plus 2', controls: ['fan'], severity: 'block' },
    { kind: 'plugin', id: 'fanctrlplus2', name: 'FanCtrl Plus 2', controls: ['fan'], severity: 'warn' },
    { kind: 'module', id: 'it87', name: 'it87 kernel driver', controls: ['fan'], severity: 'warn' },
    { kind: 'hwmon', id: 'it8613', name: 'it8613 hwmon', controls: ['fan'], severity: 'warn', detail: 'pwm2, pwm3 manual' }
];
const LED_RIVALS: Rival[] = [{ kind: 'daemon', id: 'ugreen-leds', name: 'UGREEN LEDs driver', controls: ['led'], severity: 'block' }];

const cpuName = (m: Model) => m.fixture?.cpu ?? m.cpu ?? 'Intel Core i5-1235U';
// Threads per CPU name (the demo's cpuName). Intel ARK / intel.com SKU pages; the N6005 page calls it Pentium Silver.
const THREADS: Record<string, number> = {
    'Intel Celeron N5105': 4,
    'Intel Celeron N6005': 4,
    'Intel N100': 4,
    'Intel N150': 4,
    'Intel Pentium Gold 8505': 6,
    'Intel Core i5-1235U': 12,
    'Intel Core 5 120U': 12,
    'Intel Core 7 150U': 12,
    'Intel Core Ultra 5 125H': 18,
    'Intel Core Ultra 7 255H': 16,
    // AMD's spec page could not be read yet; 0 hides the count in the UI (app.ts shows it only when truthy).
    'AMD Ryzen Embedded R2514': 0
};
const threadsOf = (cpu: string) => {
    const n = THREADS[cpu];
    if (n === undefined) throw new Error(`no thread count for "${cpu}": add it to THREADS in web/models/fixture.ts`);
    return n;
};
const ram = (m: Model) => m.fixture?.ram ?? m.ram ?? { type: 'DDR5', mt: 4800 };

/** Fan control answers with a chip: a fixed-wiring model. A probe model has no confirmed fan map in the sample data, a model without `fan` has no fan control. */
export const fixtureHasChip = (m: Model) => m.fan !== null && m.fan.kind !== 'probe';

/** Bytes per second the sample NICs move (eth0 busy, eth1 nearly idle); the harness and the demo grow the counters by it so the LAN sockets flicker. Same numbers in tools/harness/api-fixture.php. */
export const NET_IO_RATES: [number, number][] = [
    [40_000_000, 9_000_000],
    [300_000, 120_000]
];

/** `fan_caps` of the status (backend/internal/fan/caps.go CapsFor); absent for a model without fan control. A probe model has no confirmed map here: 0 channels. */
export function fanCaps(m: Model) {
    if (!m.fan) return null;
    const probe = m.fan.kind === 'probe';
    return {
        kind: probe ? 'probe' : 'fixed',
        channels: probe ? 0 : Object.keys(m.fan.fans).length,
        watchdog: Boolean(m.fan.watchdog),
        backlight: m.series === 'iDX',
        ...(m.fan.minPctFloor ? { min_pct_floor: m.fan.minPctFloor } : {})
    };
}

/** `settings.fan_config` (config_v2.go View) of a file without v2 keys: the defaults, the v1 values as the groups' tuning. */
export function fanConfig(min = 25, cpuCurve = '40:30,70:100', caseCurve = '30:30,50:100'): FanConfigV2 {
    const group = (curve: string): FanGroupConfig => ({
        input: 'auto',
        curve,
        deadband: 0,
        start_pct: 0,
        start_ms: 0,
        min_run: min,
        up_step: 100,
        down_step: 2,
        on_missing: 'fallback',
        allow_off: false
    });
    return {
        version: 1,
        cpu: group(cpuCurve),
        case: group(caseCurve),
        channels: {},
        quiet: { enabled: false, from: '23:00', to: '07:00', days: 'mon,tue,wed,thu,fri,sat,sun', cap: 40, scope: 'case' },
        mover: { mode: 'off', pct: 40 },
        parity: { mode: 'off', pct: 40 }
    };
}

export const fanWatchdog = (m: Model) =>
    m.fan?.watchdog ? { armed: true, timeout: 30, interval: 10, last_pet: 1761555598, time: 1761555600, state: 'armed' } : null;

export function fanSamples(m: Model) {
    const probe = m.fan?.kind === 'probe';
    return {
        meta: { dmi: m.fixtureDmi, kind: fanCaps(m)?.kind ?? '', backlight: Boolean(fanCaps(m)?.backlight), roles: m.fan?.fans ?? {} },
        config: fanConfig(),
        inputs: {
            options: [
                { spec: 'auto', label: 'Automatic', kind: 'auto', reading: 54, usable: true },
                { spec: 'cpu', label: 'CPU package', kind: 'cpu', reading: 54, usable: true },
                { spec: 'disk_max', label: 'Hottest drive', kind: 'disk_max', reading: 47, usable: true },
                { spec: 'disk_avg', label: 'Average of the drives', kind: 'disk_avg', reading: 47, usable: true },
                { spec: 'nvme', label: 'Hottest NVMe', kind: 'nvme', reading: 44, usable: true },
                { spec: 'gpu', label: 'GPU', kind: 'gpu', reading: 51, usable: true },
                { spec: 'nic:eth0', label: 'eth0 (NIC)', kind: 'nic', reading: 66, usable: true }
            ],
            disks: [
                { name: 'disk2', device: 'sdd', temp: 47, standby: false, nvme: false },
                { name: 'parity', device: 'sdc', temp: null, standby: true, nvme: false },
                { name: 'nvme3', device: 'nvme3n1', temp: 44, standby: false, nvme: true }
            ]
        },
        sources: [{ id: 'dynamix', name: 'Dynamix Auto Fan Control', files: ['/boot/config/plugins/dynamix/dynamix.cfg'], running: false }],
        profile: { mode: 'custom', min_pct: '30', cpu_curve: '40:25,60:50,80:100', case_curve: '35:25,50:100' } as Record<string, string>,
        probe: probe
            ? {
                  proposal: {
                      version: 1,
                      model: m.fixtureDmi,
                      chip: 'IT8613E',
                      time: 1761555600,
                      outputs: [
                          { pwm: 'pwm1', status: 'no_fan', base_rpm: 0, peak_rpm: 0 },
                          { pwm: 'pwm2', status: 'mapped', tach: 'fan2', base_rpm: 800, peak_rpm: 2100 },
                          { pwm: 'pwm3', status: 'mapped', tach: 'fan3', base_rpm: 900, peak_rpm: 2400 }
                      ]
                  },
                  tachs: [
                      { id: 'fan1', rpm: 0 },
                      { id: 'fan2', rpm: 0 },
                      { id: 'fan3', rpm: 1700 }
                  ]
              }
            : null,
        backlight: { pct: 80, raw: 204 }
    };
}

export function status(m: Model) {
    const hasChip = fixtureHasChip(m);
    const caps = fanCaps(m);
    const fanIds = hasChip ? Object.keys(m.fan?.fans ?? {}) : [];
    const chip = hasChip ? (m.fixture?.chip ?? m.fan?.chip.match(/IT\d{4}/)?.[0] ?? '') : '';
    // What backend/internal/fan/info.go requireSupportedModel says, as the status reports it in chip_error.
    const chipError =
        m.fan?.kind === 'probe'
            ? `${m.fixtureDmi} has no confirmed fan map yet: run the fan probe first (/usr/local/emhttp/plugins/ugreen-nas-control/backend/ugreen-nas-fan probe-fans run)`
            : `${m.fixtureDmi} has no fan control`;
    const fans = fanIds.map(id => {
        const cpu = m.fan?.fans[id] === 'cpu';
        return { id, role: m.fan?.fans[id], rpm: cpu ? 1500 : 900, pct: cpu ? 40 : 30, auto: false };
    });
    const curve = (top: number) =>
        [
            [10, top / 6],
            [20, top / 3],
            [40, top / 2],
            [70, top * 0.78],
            [100, top]
        ].map(([p, r]) => [p, Math.round(r ?? 0)]);
    return {
        ok: true,
        running: true,
        fan_control: true,
        info: {
            model: m.fixtureDmi,
            bios: '1.0',
            bios_date: '01/01/2024',
            cpu: cpuName(m),
            threads: threadsOf(cpuName(m)),
            memory_kb: 33554432 * (caseModel({ model: m.fixtureDmi }).ramSlots ?? 2),
            kernel: '6.18.38-Unraid',
            unraid: '7.3.2',
            uptime: 400000
        },
        sensors: {
            cpu: 54,
            cpu_cores: [],
            nvme: [{ name: 'nvme3', temp: 44 }],
            gpu: 51,
            nvme_extra: [
                { name: 'nvme1', label: 'Sensor 1', temp: 64 },
                { name: 'nvme1', label: 'Sensor 2', temp: 42 }
            ],
            nic: [
                { iface: 'eth0', label: 'PHY', temp: 66 },
                { iface: 'eth0', label: 'MAC', temp: 66 },
                { iface: 'eth1', label: 'PHY', temp: 64 },
                { iface: 'eth1', label: 'MAC', temp: 64 }
            ],
            disks: [{ name: 'disk2', device: 'sdd', temp: 47, standby: false, hdd: true }],
            board: 30,
            not_connected: []
        },
        net_io: (m.lan?.maxMbit ?? [2500]).map((_, i) => ({ name: `eth${i}`, rx: 0, tx: 0 })),
        presets: {
            quiet: ['50:15,80:100', '40:20,55:100'],
            standard: ['40:30,70:100', '30:30,50:100'],
            performance: ['35:35,65:100', '30:35,50:100'],
            full: ['0:100', '0:100']
        },
        // Without a usable chip the real answer has chip_error instead of chip, fans and chip_temps, and no daemon runs.
        ...(hasChip ? { chip, fans, chip_temps: [38] } : { chip_error: chipError }),
        calibration: hasChip
            ? {
                  table: { time: 1761555600, fans: Object.fromEntries(fanIds.map(id => [id, curve(m.fan?.fans[id] === 'cpu' ? 4100 : 1680)])) },
                  progress: null
              }
            : { table: null, progress: null },
        watchdog: fanWatchdog(m),
        ...(caps ? { fan_caps: caps } : {}),
        ...(hasChip
            ? {
                  daemon: {
                      time: 1761555600,
                      mode: 'standard',
                      cpu_temp: 54,
                      case_temp: 47,
                      case_from: 'disk',
                      case_driver: 'case',
                      failsafe: '',
                      stalled: [],
                      chip_temps: [38],
                      quiet_active: false,
                      job: '',
                      cap: null,
                      targets: Object.fromEntries(fans.map(f => [f.id, f.pct]))
                  }
              }
            : {}),
        settings: {
            mode: 'standard',
            min_pct: 25,
            cpu_crit: 90,
            disk_crit: 55,
            nvme_offset: 15,
            stall_notify: true,
            fan_control: true,
            case_follows_cpu: false,
            ignore_drives: ['cache'],
            ui_lang: 'en',
            ...(caps ? { fan_config: fanConfig() } : {})
        },
        // Nothing else drives the chips; the scenarios below are merged over this when the harness runs with UNC_SCENARIO.
        conflicts: [],
        conflict: false,
        scenarios: {
            // FanCtrl Plus 2 writes the fan chip: our daemon only reads.
            conflict: { conflicts: FAN_RIVALS, conflict: true, daemon: { conflict: ['FanCtrl Plus 2'] } },
            // A rival LED tool runs while the LED part is on.
            'conflict-led': { conflicts: LED_RIVALS }
        },
        led: {
            enabled: m.led !== 'none',
            supported: m.led !== 'none',
            blocker: '',
            untested: m.verified.leds.status === 'expected',
            warning: '',
            import_available: false,
            model: m.fixtureDmi
        }
    };
}

export function ledOutputs(m: Model) {
    const rows = bayRows(bayCount(m));
    const led = (name: string, color: string, brightness: number) => ({
        name,
        brightness: String(brightness),
        max_brightness: '255',
        color,
        effect: 'none',
        on_ms: '0',
        off_ms: '0'
    });
    const all: Record<string, unknown> = { power: led('power', '255 255 255', 255), netdev: led('netdev', '0 160 255', 255) };
    rows.forEach((r, i) => (all[`disk${i + 1}`] = led(`disk${i + 1}`, r?.standby ? '255 160 0' : '255 255 255', r ? (r.standby ? 60 : 200) : 0)));
    const sc = Object.fromEntries(['100', '1000', '2500', '10000'].map((s, i) => [s, ['#ff3b30', '#ffcc00', '#34c759', '#0a84ff'][i]]));
    return {
        status: {
            ok: true,
            foreign_monitor: false,
            model: m.fixtureDmi,
            bays: rows.length,
            kernel: '6.18.38-Unraid',
            mapping: ({ verified: 'tested', community: 'reported', partial: 'reported' } as Record<string, string>)[m.verified.leds.status] ?? 'guessed',
            mode: 'status',
            daemon: true,
            fx: '',
            fx_leds: '',
            identify: [],
            sync_action: '',
            sync_percent: 0,
            sync_enabled: true,
            sync_color: '#ffa500',
            standby_mode: 'dim',
            standby_level: 30,
            smart_enabled: true,
            error_enabled: true,
            activity_enabled: true,
            smart_color: '#ffa500',
            disk_color_mode: 'own',
            netdev_status: 'activity',
            alert_enabled: true,
            alert_level: 'warning',
            alert_color: '#ff0000',
            shutdown_blink: true,
            shutdown_color: '#ff0000',
            shutdown_effect: 'blink',
            alert_count: 0,
            alert_active: false,
            fx_speed: 2,
            fx_reverse: false,
            fx_idle: false,
            night: false,
            disk_error_color: '#ff0000',
            saved_brightness: {},
            network_device: 'eth0',
            netdev_speed: m.lan?.maxMbit[0] ?? 2500,
            network_ports: (m.lan?.maxMbit ?? [2500]).map((max, i) => ({ name: `eth${i}`, link: i === 0, speed: i === 0 ? (max ?? 2500) : 0, max: max ?? 0 })),
            netdev_speed_enabled: true,
            netdev_speed_colors: sc,
            controller: 'ugreen-led-i2c',
            i2c_bus: 0,
            i2c_address: '0x3a',
            i2c_probe: 'ok',
            leds: Object.keys(all)
        },
        all,
        list: Object.keys(all),
        bays: rows.map((r, i) => ({
            bay: i + 1,
            led: `disk${i + 1}`,
            device: r ? `sd${'cdefghij'[i]}` : '',
            serial: '',
            slot: r?.role ?? '',
            status: r ? 'DISK_OK' : '',
            error: false,
            standby: Boolean(r?.standby),
            temp: r?.temp ?? null,
            errors: 0,
            temp_hot: 45,
            temp_max: 55,
            smart_now: '',
            smart_key: '',
            smart_warnings: ''
        }))
    };
}
