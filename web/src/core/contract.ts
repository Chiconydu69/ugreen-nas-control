/*
 * What the two api.php endpoints return and accept, as the frontend uses it. Derived from web/plugin/api.php,
 * web/plugin/led/api.php and the JSON of the Go fan tool (backend/internal/fan) and the LED controller (ugreen-led-ctl).
 * Lists the Go tool could not fill arrive as null, hence `| null`.
 */

import type {
    FanInputsResponse,
    FanInputTestResponse,
    FanProfileApplyResponse,
    FanProfilePreviewResponse,
    FanProfileSourcesResponse
} from './contract-fan-inputs';
import type {
    Cs201xConfirmResponse,
    Cs201xReadResponse,
    FanBacklightResponse,
    FanBacklightSetResponse,
    FanCaps,
    FanProbeConfirmResponse,
    FanProbeStatusResponse,
    FanProbeTachsResponse,
    PanelBeepResponse,
    PanelButtonsResponse,
    PowerAcMode,
    PowerAcResponse,
    Watchdog
} from './contract-fan-probe';
import type { FanScheduleResponse } from './contract-fan-schedule';
import type { FanHistoryResponse } from './contract-fan-history';

export type ApiFailure = { ok: false; error: string };
export type ApiOk = { ok: true };

export const MODE_IDS = ['bios', 'quiet', 'standard', 'performance', 'full', 'custom'] as const;
export type Mode = (typeof MODE_IDS)[number];

export interface Settings {
    mode: Mode;
    cpu_curve: string;
    case_curve: string;
    min_pct: number;
    cpu_crit: number;
    disk_crit: number;
    nvme_offset: number;
    stall_notify: boolean;
    fan_control: boolean;
    case_follows_cpu: boolean;
    ignore_drives: string[];
    ui_lang: string;
    ui_theme: string;
    /** The effective v2 fan configuration (backend/internal/fan/config_v2.go View); absent until the status reports it. */
    fan_config?: FanConfigV2;
}

/* Fan config v2 (cfg_version=2): defaults repeat the v1 globals, so a v1 file behaves as before. */

export type FanOnMissing = 'fallback' | 'curve_low' | 'hold';
export type FanJobMode = 'off' | 'floor' | 'boost';

/** One group's (or one fan's, with fan.<id>.<key> overrides) tuning. `input` is a spec: auto, cpu, disk_max[:a+b], disk_avg[:a+b], nvme, gpu, hba, nic:<iface>, script:<name> or max:<a>|<b>. */
export interface FanGroupConfig {
    input: string;
    curve: string;
    /** Percent the target must move before the fan follows (0 = always). */
    deadband: number;
    /** Kick-start speed and duration from a stop (0 = none). */
    start_pct: number;
    start_ms: number;
    /** Lowest running speed, never below the global minimum. */
    min_run: number;
    /** Largest change per tick; 100 = at once. */
    up_step: number;
    down_step: number;
    on_missing: FanOnMissing;
    /** A curve target of 0 may stop the fan (default off; the global minimum, a job floor and an emergency still win). */
    allow_off: boolean;
}

export interface FanQuietConfig {
    enabled: boolean;
    /** HH:MM; `to` earlier than `from` crosses midnight. */
    from: string;
    to: string;
    /** "mon,tue,..." the weekday the window starts on. */
    days: string;
    cap: number;
    scope: 'case' | 'all';
}

export interface FanJobConfig {
    mode: FanJobMode;
    pct: number;
}

export interface FanConfigV2 {
    /** 1: the file has no cfg_version yet (it is rewritten, with fan.cfg.bak kept, by the first v2 save). */
    version: 1 | 2;
    cpu: FanGroupConfig;
    case: FanGroupConfig;
    /** Only the fans that have overrides, by id (fan1 ...). */
    channels: Record<string, FanGroupConfig>;
    quiet: FanQuietConfig;
    mover: FanJobConfig;
    parity: FanJobConfig;
}

/** An input offered for a group (fan_inputs): `spec` is what goes into <group>_input. */
export interface FanInputOption {
    spec: string;
    label: string;
    kind: 'auto' | 'cpu' | 'disk_max' | 'disk_avg' | 'nvme' | 'gpu' | 'hba' | 'nic' | 'script' | 'max';
    /** Current reading in °C, null when it cannot be read now. */
    reading: number | null;
    /** false: the source is not there on this NAS (shown greyed out). */
    usable: boolean;
}

/** fan_schedule: the quiet window and the background jobs as the daemon sees them now. */
export interface FanSchedule {
    quiet: FanQuietConfig & { active: boolean; /** HH:MM the window ends, while active. */ until?: string };
    job: { mover: boolean; parity: boolean; active: '' | 'mover' | 'parity' };
    /** Speed cap now in force (quiet window), null for none. */
    cap: number | null;
}

/** fan_profile_preview: what importing a profile would change; nothing is written until fan_profile_apply. */
export interface FanProfilePreview {
    ok: true;
    /** own = this plugin's export; otherwise the tool the file or config came from. */
    source: 'own' | 'fanctrlplus' | 'fanctrlplus2' | 'dynamix' | 'itkuny';
    /** Settings keys and values the profile would set. */
    profile: Record<string, string>;
    diff: { key: string; from: string | null; to: string }[];
    warnings: string[];
    not_imported: { key: string; reason: string }[];
    /** Passed to fan_profile_apply. */
    token: string;
}

export interface Info {
    model: string;
    bios: string;
    bios_date: string;
    cpu: string;
    threads: number;
    memory_kb: number;
    kernel: string;
    unraid: string;
    uptime: number;
}

export interface Temp {
    name: string;
    label?: string;
    temp: number | null;
}

export interface DiskTemp {
    name: string;
    device: string;
    id?: string;
    temp: number | null;
    standby: boolean;
    hdd: boolean;
}

export interface NicTemp {
    iface: string;
    label?: string;
    temp: number | null;
}

export interface Sensors {
    cpu: number | null;
    cpu_cores: Temp[] | null;
    nvme: Temp[] | null;
    nvme_extra: Temp[] | null;
    nic: NicTemp[] | null;
    gpu: number | null;
    disks: DiskTemp[] | null;
    board: number | null;
}

export type FanRole = 'cpu' | 'case' | '';

export interface Fan {
    id: string;
    role: FanRole;
    rpm: number;
    /** null: speed unknown (the chip is in auto mode). */
    pct: number | null;
    auto: boolean;
    error?: string;
}

/** The fan daemon's last tick (fan/daemon.go State). */
export interface DaemonState {
    time: number;
    mode: string;
    failsafe: string;
    cpu_temp: number | null;
    case_temp: number | null;
    case_from: string;
    case_driver: string;
    targets: Record<string, number> | null;
    stalled: string[] | null;
    chip_temps: (number | null)[] | null;
    /** Names of the rival fan controllers while the daemon only reads (fan/conflict). */
    conflict?: string[] | null;
    /** The emergency (failsafe key) the daemon did not answer with 100 % because a rival controls the fans. */
    unapplied?: string;
    /** Inputs that could not be read this tick (specs); the group used its on_missing rule. */
    inputs_missing?: string[] | null;
    /** The quiet window caps the fans now. */
    quiet_active?: boolean;
    /** The background job raising the case fans now: "mover", "parity" or "". */
    job?: '' | 'mover' | 'parity';
    /** The quiet cap in force in %, null for none. */
    cap?: number | null;
}

/** Another tool that can drive the same fan chip or LEDs (backend/internal/fan/conflict). */
export interface Conflict {
    kind: 'plugin' | 'module' | 'hwmon' | 'daemon';
    id: string;
    name: string;
    /** watchdog: a module that owns the IT8613E watchdog (it87_wdt); never a rival, never blocks. */
    controls: ('fan' | 'led' | 'watchdog')[];
    /** block: a rival writer is running, this plugin yields; warn: installed or loaded only. */
    severity: 'block' | 'warn';
    detail?: string;
}

/** Measured (percent, rpm) pairs per fan id. */
export type CalibRows = [number, number][];

export interface Calibration {
    table: { time: number; fans: Record<string, CalibRows> } | null;
    progress: {
        state: 'running' | 'done' | 'aborted';
        fan?: string;
        pct?: number;
        step: number;
        steps: number;
        reason?: string;
        time: number;
    } | null;
}

/** The LED part of the plugin as `ugreen-nas-ctl led-status` reports it. */
export interface LedPart {
    enabled: boolean;
    supported: boolean;
    blocker?: string;
    warning?: string;
    untested?: boolean;
    import_available?: boolean;
    model?: string;
}

/** Byte counters of one physical NIC since boot; the UI turns two polls into a rate. */
export interface NetIo {
    name: string;
    rx: number;
    tx: number;
}

export interface StatusResponse {
    ok: boolean;
    info: Info;
    sensors: Sensors;
    /** Preset name -> [CPU curve, case curve] as "40:30,60:50". */
    presets: Record<string, [string, string]>;
    net_io: NetIo[];
    fan_control: boolean;
    /** Rival fan and LED tools found; empty when none. */
    conflicts?: Conflict[];
    /** A rival fan controller is running: the daemon does not write the fan chip. */
    conflict?: boolean;
    chip?: string;
    chip_error?: string;
    fans?: Fan[] | null;
    chip_temps?: (number | null)[] | null;
    calibration: Calibration;
    /** EC watchdog sample; null on every model but the DXP4800 GT and the iDX6011 Pro, or before the first tick. */
    watchdog?: Watchdog | null;
    /** What this model offers; absent without fan control. */
    fan_caps?: FanCaps;
    daemon?: DaemonState | null;
    running: boolean;
    /** Plugin version of the backend that answered (the .plg version); absent from backends before 3.6. */
    version?: string;
    led: LedPart;
    settings: Settings;
}

export interface Dimm {
    locator: string;
    size_mb: number;
    type: string;
    speed_mt: number;
    configured_mt: number;
    manufacturer: string;
    part_number: string;
}

export interface Nvme {
    id: string;
    model: string;
    size_bytes: number;
    /** "m2-1", "m2-2", "m2" (a slot of unknown place), "system" or "pcie". */
    slot: string;
    pci: string;
    temp: number | null;
    role?: string;
    boot: boolean;
}

export interface Bay {
    bay: number;
    present: boolean;
    device?: string;
    model?: string;
    size_bytes?: number;
    hdd: boolean;
    role?: string;
    id?: string;
    temp: number | null;
    standby: boolean;
}

export interface NetPort {
    name: string;
    link: boolean;
    speed_mbit: number;
    max_mbit: number;
}

export interface HardwareResponse {
    ok: boolean;
    model: string;
    memory: Dimm[] | null;
    nvme: Nvme[] | null;
    bays: Bay[] | null;
    net: NetPort[] | null;
}

/** The hardware as the page holds it: lists the tool printed as null are empty. */
export type Hardware = Omit<HardwareResponse, 'memory' | 'nvme' | 'net'> & { memory: Dimm[]; nvme: Nvme[]; net: NetPort[] };

export interface NasGet {
    status: StatusResponse;
    hardware: HardwareResponse;
    fan_inputs: FanInputsResponse;
    fan_schedule: FanScheduleResponse;
    fan_history: FanHistoryResponse;
    fan_profile_sources: FanProfileSourcesResponse;
    fan_probe_status: FanProbeStatusResponse;
    fan_probe_tachs: FanProbeTachsResponse;
    fan_backlight: FanBacklightResponse;
    cs201x_read: Cs201xReadResponse;
    panel_buttons: PanelButtonsResponse;
    power_ac_get: PowerAcResponse;
}

type Flag = '0' | '1';

export interface NasPost {
    mode: { value: Mode };
    min: { min_pct: number; case_follows_cpu: Flag };
    curves: { cpu_curve: string; case_curve: string; case_follows_cpu: Flag; min_pct: number };
    safety: { cpu_crit: string; disk_crit: string; nvme_offset: string; stall_notify: Flag; ignore_drives: string };
    lang: { value: string };
    theme: { value: string };
    fan_control: { value: 'on' | 'off' };
    led_import: Record<string, never>;
    led: { value: 'on' | 'off' };
    calibrate: Record<string, never>;
    /** values[<key>]=<value> for any v2 key (backend/internal/fan/config_v2.go), reset=<key>,<key> back to the default. */
    fan_v2: { [field: `values[${string}]`]: string | number; reset?: string };
    fan_input_test: { spec: string };
    /** source=<id> (+ path= for itkuny), or content=<text>. */
    fan_profile_preview: { source?: string; path?: string; content?: string };
    fan_profile_apply: { token: string; source?: string; path?: string; content?: string };
    fan_probe_run: Record<string, never>;
    /** map[pwmN]=cpu|case */
    fan_probe_confirm: { [output: `map[pwm${number}]`]: 'cpu' | 'case' };
    fan_probe_forget: Record<string, never>;
    fan_backlight: { pct: number };
    /** fans=fan1,fan3 (any of them, comma separated); absent = both */
    cs201x_confirm: { fans?: string };
    cs201x_forget: Record<string, never>;
    panel_beep: { ms?: number };
    power_ac_set: { mode: PowerAcMode };
}

/** Answers of POSTs that carry more than {ok: true}. */
export interface NasPostReply {
    fan_input_test: FanInputTestResponse;
    fan_profile_preview: FanProfilePreviewResponse;
    fan_profile_apply: FanProfileApplyResponse;
    fan_probe_confirm: FanProbeConfirmResponse;
    fan_backlight: FanBacklightSetResponse;
    cs201x_confirm: Cs201xConfirmResponse;
    panel_beep: PanelBeepResponse;
    power_ac_set: PowerAcResponse;
}

/* ---------- LED: web/plugin/led/api.php ---------- */

/** One LED as `ugreen-led-ctl all` reports it; numbers arrive as strings, `rx` is added by ledfx stamp(). */
export interface LedState {
    color?: string;
    brightness?: number | string;
    max_brightness?: number | string;
    effect?: string;
    on_ms?: number | string;
    off_ms?: number | string;
    sent_ms?: number | string | undefined;
    now_ms?: number | string | undefined;
    rx?: number | undefined;
}

/** What ledfx needs of the LED status. */
export interface LedStatus {
    fx_leds?: string;
    fx?: string;
    fx_speed?: number;
    fx_reverse?: boolean;
}

export interface LedNetworkPort {
    name: string;
    link: boolean;
    speed: number;
    max: number;
}

/** `ugreen-led-ctl status` through led/api.php?action=status. */
export interface LedStatusResponse extends LedStatus {
    ok: boolean;
    foreign_monitor: boolean;
    model: string;
    bays: number;
    kernel: string;
    mapping: string;
    mode: 'status' | 'manual';
    daemon: boolean;
    fx: string;
    fx_leds: string;
    fx_speed: number;
    fx_reverse: boolean;
    fx_idle: boolean;
    identify: string[];
    sync_action: string;
    sync_percent: number;
    sync_enabled: boolean;
    sync_color: string;
    standby_mode: string;
    standby_level: number;
    smart_enabled: boolean;
    error_enabled: boolean;
    activity_enabled: boolean;
    smart_color: string;
    disk_color_mode: string;
    netdev_status: string;
    alert_enabled: boolean;
    alert_level: string;
    alert_color: string;
    shutdown_blink: boolean;
    shutdown_color: string;
    shutdown_effect: string;
    alert_count: number;
    alert_active: boolean;
    night: boolean;
    disk_error_color: string;
    saved_brightness: Record<string, number | string>;
    network_device: string;
    netdev_speed: number;
    network_ports: LedNetworkPort[];
    netdev_speed_enabled: boolean;
    netdev_speed_colors: Record<string, string>;
    controller: string;
    i2c_bus: number | string | null;
    i2c_address: string | null;
    i2c_probe: string;
    /** The LEDs the controller has, in drawing order. */
    leds: string[];
    settings_saved: boolean;
}

export interface LedBay {
    bay: number;
    led: string;
    device: string;
    serial: string;
    slot: string;
    status: string;
    error: boolean;
    standby: boolean;
    temp: number | null;
    errors: number;
    temp_hot: number;
    temp_max: number;
    smart_now: string;
    smart_key: string;
    smart_warnings: string;
}

export interface LedSchedule {
    ok: true;
    enabled: boolean;
    start: string;
    end: string;
    action: string;
    brightness: number;
}

export interface LedGet {
    status: LedStatusResponse;
    bays: { ok: true; bays: LedBay[] };
    all: { ok: true; leds: Record<string, LedState> };
    schedule: LedSchedule;
}

export const isMode = (value: unknown): value is Mode => (MODE_IDS as readonly unknown[]).includes(value);

export interface LedRgb {
    r: number;
    g: number;
    b: number;
}

/** Write actions of led/api.php. Values that come from a <select> are strings; api.php validates them. */
export interface LedPost {
    mode: { value: string };
    shutdown: { enabled: Flag; effect: string } & LedRgb;
    shutdown_test: Record<string, never>;
    netdev_speed: { speed_enabled: Flag; [step: `speed_${number}`]: string };
    status_display: {
        disk_color_mode: string;
        smart_enabled: Flag;
        error_enabled: Flag;
        activity_enabled: Flag;
        smart_r: number;
        smart_g: number;
        smart_b: number;
        sync_enabled: Flag;
        standby_mode: string;
        standby_level: string;
    } & LedRgb;
    netdev_status: { value: string };
    smart_ack: { led: string };
    alerts: { enabled: Flag; level: string } & LedRgb;
    identify: { led: string; value: 'start' | 'stop' };
    fx: { value: string; led: string; speed: number; reverse: Flag; idle: Flag };
    schedule: { enabled: Flag; start: string; end: string; night_action: string; brightness: number };
    error_color: LedRgb;
    /** The LED actions take one LED or a comma-separated list. */
    on: { led: string };
    off: { led: string };
    stop: { led: string };
    brightness: { led: string; value: number };
    color: { led: string } & LedRgb;
    blink: { led: string; on: number; off: number };
    breath: { led: string; on: number; off: number };
}

export type LedWriteAction = 'on' | 'off' | 'stop' | 'brightness' | 'color' | 'blink' | 'breath';
