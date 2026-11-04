/*
 * The probe, backlight and capability answers of the fan tool (web/plugin/include/fan_probe.php, fan_backlight.php;
 * backend/internal/fan/probe_cmd.go, probe.go, caps.go, ec_watchdog.go).
 *
 * Probe: only on models with fan_caps.kind === 'probe' (IT8613E boards whose header wiring is unknown). Without a
 * confirmed map the daemon never starts and the status has no fans: fan control stays off until fan_probe_confirm.
 * Only outputs with status 'mapped' can be confirmed; roles are 'cpu' or 'case'. A refused start (another fan tool
 * running, fan control off) is HTTP 409 with the reason.
 */
import type { ApiOk } from './contract';

/** `fan_caps` of the status: what the page may offer; absent for a model without fan control. */
export interface FanCaps {
    /** readonly: readings only (plain iDX6011 and iDX6012): no writes, no daemon, no curves. */
    kind: 'fixed' | 'probe' | 'readonly';
    /** Fans the plugin drives; 0 on a probe model until a map is confirmed. */
    channels: number;
    /** A watchdog petter runs here: the IT5571 EC watchdog of the DXP4800 GT and iDX6011 Pro, the IT8613E one of the DXP2800, DXP4800, DXP4800 Plus/Pro, DXP6800 Pro and DXP8800 Plus; see Watchdog. */
    watchdog: boolean;
    /** The LCD backlight can be read and set (iDX Pro). */
    backlight: boolean;
    /** The lowest speed the hardware runs at: a lower minimum is raised to it by the daemon (CS201x, 25 %). Absent: no floor. */
    min_pct_floor?: number;
}

/**
 * `watchdog` of the status: null when nothing was sampled (no petter on this model, or nothing ticked since boot).
 * The plugin never arms the watchdog, it only feeds one the BIOS armed (backend/internal/fan/ec_watchdog.go, ite_watchdog.go).
 */
export interface Watchdog {
    armed: boolean;
    /** Seconds until the EC resets the box without a feed. */
    timeout: number;
    interval: number;
    /** Unix time of the last feed, 0 = never. */
    last_pet: number;
    time: number;
    /** Timer value read back before the feed (EC: the timeout registers, IT8613E: the 16-bit value); only while armed. */
    counter?: number;
    /**
     * off: not armed; armed: fed in time; action_needed: armed, but not fed for more than twice the interval;
     * yielded (IT8613E): the it87_wdt module owns the timer, the plugin accesses nothing;
     * not_counting (IT8613E): the value is set but does not count down, so the plugin writes nothing.
     * not_effective: feeding does not change this unit's timer (see docs/watchdog.md).
     */
    state: 'off' | 'armed' | 'action_needed' | 'yielded' | 'not_counting' | 'not_effective';
}

export interface ProbeOutput {
    pwm: string;
    status: 'mapped' | 'no_fan' | 'ambiguous';
    /** Tach input that followed the output (mapped). */
    tach?: string;
    candidates?: string[];
    base_rpm: number;
    peak_rpm: number;
}

export interface ProbeProposal {
    version: 1;
    model: string;
    chip: string;
    /** Unix time; a result older than the backend's limit is dropped. */
    time: number;
    outputs: ProbeOutput[];
    /** What community reports say, never trusted: the outputs that should move a fan. */
    expected?: { pwms?: string[]; tachs?: string[] };
    agrees?: boolean;
}

export interface FanMapOutput {
    id: string;
    role: 'cpu' | 'case';
    pwm: string;
    tach: string;
}

export interface FanMap {
    version: 1;
    model: string;
    chip: string;
    time: number;
    outputs: FanMapOutput[];
}

export interface ProbeProgress {
    state: 'running' | 'done' | 'aborted';
    pwm?: string;
    step: number;
    steps: number;
    reason?: string;
    time: number;
}

/** GET fan_probe_status. */
export type FanProbeStatusResponse = ApiOk & {
    model: string;
    /** fixed, probe, readonly or '' (no fan control). */
    kind: 'fixed' | 'probe' | 'readonly' | '';
    /** The confirmed map, null until confirmed. */
    map: FanMap | null;
    /** A map file exists but does not fit this NAS. */
    map_error?: string;
    /** Last complete run, waiting for fan_probe_confirm. */
    proposal: ProbeProposal | null;
    progress: ProbeProgress | null;
    /** false until a map is confirmed (probe models). */
    fan_control_possible: boolean;
};

/** GET fan_probe_tachs: read-only rpm of every tach input. */
export type FanProbeTachsResponse = ApiOk & { tachs: { id: string; rpm: number }[] };

/** POST fan_probe_confirm with map[pwmN]=cpu|case. */
export type FanProbeConfirmResponse = ApiOk & { map: FanMap };

/** GET fan_backlight: brightness in percent (100 = full, 0 = off) and the raw register. pct is the string "default" while the register holds the vendor default (raw 0): the panel shows its own brightness. */
export type FanBacklightResponse = ApiOk & { pct: number | 'default'; raw: number };

/** POST fan_backlight pct=0..100 */
export type FanBacklightSetResponse = ApiOk & { pct: number };

/*
 * DXP2800 GT, Chipsea CS201x (web/plugin/include/fan_probe.php, backend/internal/fan/cs201x.go): kind 'probe', but not the
 * IT8613E probe. Fan control stays off until cs201x_confirm saved the map; fan_probe_status then answers with that map.
 * A refusal is HTTP 409 with the tool's reason.
 */

export interface Cs201xFan {
    id: string;
    role: 'cpu' | 'case';
    enabled_reg: number;
    duty_pct: number;
    rpm: number;
}

/** What `ugreen-nas-fan cs201x` reads; the optional parts are missing when the chip did not answer. */
export interface Cs201xReading {
    model: string;
    chip_id: string;
    revision: string;
    /** active / not active */
    host_ec: string;
    base?: string;
    fans?: Cs201xFan[];
    /** cpu (NTC2) and board (NTC4), unsigned degrees. */
    temps?: Record<string, number>;
    temp_inputs?: { ch2_cfg: number; ch4_cfg: number; configured: boolean };
    watchdog?: { cfg1: number; cfg2: number; armed: boolean; timeout: number; counter: number; counter_later: number; counting: boolean };
    argb_flow_control?: number;
    pwrsw_cfg?: number;
    ec_firmware?: string;
    notes?: string[];
}

/** GET cs201x_read: read-only diagnostics. */
export type Cs201xReadResponse = ApiOk & { read: Cs201xReading };

/** POST cs201x_confirm fans=fan1,fan3 (optional, default both). */
export type Cs201xConfirmResponse = ApiOk & { map: FanMap };

/*
 * Front panel (web/plugin/include/panel.php, backend/internal/panel): button state and beeper, from UGREEN's driver
 * source, not yet confirmed on hardware.
 */

/** GET panel_buttons: one read of the power and reset pads. */
export type PanelButtonsResponse = ApiOk & {
    model: string;
    power: 'held' | 'released';
    reset: 'held' | 'released';
    /** false where UGREEN's source says the power key cannot be read by address. */
    power_readable: boolean;
    /** Unix time of the read. */
    time: number;
};

/** POST panel_beep ms=20..2000 (optional, default 200). */
export type PanelBeepResponse = ApiOk & { ms: number };

/*
 * Power on after AC loss (web/plugin/include/power_ac.php, backend/internal/fan/acloss_cmd.go, docs/power-on-ac.md).
 * A refusal (a fan rival runs, the mode is not offered, the board holds a value the tool does not know) is HTTP 409 with the reason.
 */

export type PowerAcMode = 'on' | 'off' | 'last';

/** GET power_ac_get; POST power_ac_set adds `set` and `verified`. */
export type PowerAcResponse = ApiOk & {
    model: string;
    /** `unknown` where the board cannot tell (DXP4800 GT) or holds a value the tool does not know. */
    mode: PowerAcMode | 'unknown';
    readable: boolean;
    /** Registers as read; empty where nothing can be read. */
    raw: Record<string, number>;
    /** The modes this model offers (`last` not on the DXP4800 GT and the iDX). */
    settable: PowerAcMode[];
    source: 'it8613' | 'ec-cmd' | 'mmio-idx' | 'fch-mmio';
    /** What this plugin last sent, with the time; null when it never did. */
    last_set: { mode: PowerAcMode; time: number } | null;
    note?: string;
    /** After a set only. */
    set?: PowerAcMode;
    verified?: boolean;
};
