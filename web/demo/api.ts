// The plugin's two api.php endpoints, answered in the browser for the site demo. The data comes from the same sample
// data the local harness serves (web/models/fixture.ts); the answers and the validation follow web/tools/harness/api-fixture.php
// (NAS) and web/plugin/led/api.php (LED). Writes change a state that is kept per model in the given store (localStorage
// in the demo), so the UI behaves like on the NAS. No DOM here: web/demo/api.test.ts runs it against the PHP harness.
import { createFan, type Reply } from './fan';
import { hardware, ledOutputs, NET_IO_RATES } from '../models/fixture';
import { shape } from './variants';
import { LANGS } from '../src/core/i18n';
import type { Model } from '../models/schema';

export interface Store {
    getItem(key: string): string | null;
    setItem(key: string, value: string): void;
    removeItem(key: string): void;
}

export interface Response {
    status: number;
    json: unknown;
}

export interface DemoOptions {
    /** Values drift a little and disk LEDs flicker; off = exactly the fixture (parity test). */
    live?: boolean;
    now?: () => number;
    /** Name of a scenario of the fixture (e.g. `conflict`), laid over the status like UNC_SCENARIO does in the harness. */
    scenario?: string | null;
}

type Obj = Record<string, unknown>;
interface Led {
    name?: string;
    brightness: string;
    max_brightness?: string;
    color: string;
    effect: string;
    on_ms: string;
    off_ms: string;
    sent_ms?: string;
    now_ms?: string;
}

interface State {
    nas: Obj;
    led: boolean;
    ledCfg: Obj;
    leds: Record<string, Partial<Led>>;
    night: { enabled: boolean; start: string; end: string; action: string; brightness: number };
    identify: Record<string, number>;
    calibStart: number | null;
}

export const KEY = 'unc-demo/v1/';
const PREFS = `${KEY}prefs`;
const UI_LANGS: readonly string[] = [...LANGS, 'auto'];
const THEMES = ['dark', 'light', 'auto'];
const MODES = ['bios', 'quiet', 'standard', 'performance', 'full', 'custom'];
const NAS_KEYS = [
    'mode',
    'min_pct',
    'case_follows_cpu',
    'cpu_curve',
    'case_curve',
    'cpu_crit',
    'disk_crit',
    'nvme_offset',
    'stall_notify',
    'ignore_drives',
    'fan_control'
];
const LED_NAME = /^(power|netdev|disk[1-8])$/;
const CALIB_STEPS = 10;
const CALIB_STEP_MS = 2000;

const read = <T>(store: Store, key: string, fallback: T): T => {
    try {
        const raw = store.getItem(key);
        return raw ? { ...fallback, ...JSON.parse(raw) } : fallback;
    } catch {
        return fallback;
    }
};
const int = (v: string | null | undefined): number | null => (v != null && /^[+-]?\d+$/.test(v.trim()) ? Number(v) : null);
const hex = (r: number, g: number, b: number) => `#${[r, g, b].map(v => v.toString(16).padStart(2, '0')).join('')}`;
const rgbText = (s: string) => {
    const m = /^(\d{1,3}) (\d{1,3}) (\d{1,3})$/.exec(s);
    return m && Math.max(Number(m[1]), Number(m[2]), Number(m[3])) <= 255 ? hex(Number(m[1]), Number(m[2]), Number(m[3])) : null;
};
function merge(into: Obj, from: Obj): void {
    for (const [k, v] of Object.entries(from)) {
        if (k === '__proto__' || k === 'constructor' || k === 'prototype') continue;
        const old = into[k];
        if (v && old && typeof v === 'object' && typeof old === 'object' && !Array.isArray(v) && !Array.isArray(old)) merge(old as Obj, v as Obj);
        else into[k] = v;
    }
}
const fail = (error: string, status = 400): Response => ({ status, json: { ok: false, error } });
const ok = (extra: Obj = {}): Response => ({ status: 200, json: { ok: true, ...extra } });

export function seedPrefs(store: Store, prefs: { lang?: string | null; theme?: string | null }): void {
    const now = read<Obj>(store, PREFS, {});
    if (prefs.lang && UI_LANGS.includes(prefs.lang)) now.ui_lang = prefs.lang;
    if (prefs.theme && THEMES.includes(prefs.theme)) now.ui_theme = prefs.theme;
    store.setItem(PREFS, JSON.stringify(now));
}

export function resetDemo(store: Store, keys: string[]): void {
    for (const k of keys) if (k.startsWith(KEY)) store.removeItem(k);
}

export function pageGlobals(store: Store, modelId: string, browserLang = 'en') {
    const prefs = read<Obj>(store, PREFS, {});
    const setting = String(prefs.ui_lang ?? 'en');
    const lang = setting === 'auto' ? (LANGS.find(l => browserLang.toLowerCase().startsWith(l)) ?? 'en') : setting;
    return { lang, setting, theme: String(prefs.ui_theme ?? 'dark'), led: read<Partial<State>>(store, `${KEY}model/${modelId}`, {}).led ?? true };
}

export function createDemo(model: Model, store: Store, opts: DemoOptions = {}) {
    const live = opts.live ?? false;
    const now = opts.now ?? Date.now;
    const key = `${KEY}model/${model.id}`;
    const state: State = {
        nas: {},
        led: true,
        ledCfg: {},
        leds: {},
        night: { enabled: false, start: '22:00', end: '07:00', action: 'dim', brightness: 20 },
        identify: {},
        calibStart: null,
        ...read<Partial<State>>(store, key, {})
    };
    const persist = () => store.setItem(key, JSON.stringify(state));
    const prefs = () => read<Obj>(store, PREFS, {});
    const setPref = (k: string, v: string) => store.setItem(PREFS, JSON.stringify({ ...prefs(), [k]: v }));

    const fan = createFan(model, { nas: state.nas, persist, now });
    const baseStatus = shape(model).status;
    // A model without a usable fan chip (probe model, no fan control) has no fans, no daemon and no chip temperatures.
    const led = ledOutputs(model);

    // Slow waves, a different phase per name: the same input gives the same number.
    const phase = (name: string) => [...name].reduce((a, c) => a + c.charCodeAt(0), 0);
    const wave = (name: string, amp: number, secs: number) => (live ? Math.round(amp * Math.sin(now() / 1000 / secs + phase(name))) : 0);
    const temp = <T extends number | null>(v: T, name: string, amp = 2): T => (v == null || !live ? v : ((v + wave(name, amp, 9)) as T));

    function nasStatus(): Obj {
        const s = structuredClone(baseStatus) as unknown as Obj & {
            settings: Obj;
            led: Obj;
            fans?: Obj[];
            fan_caps?: Obj | null;
            sensors: Obj & { cpu: number; nvme: Obj[]; nvme_extra: Obj[]; nic: Obj[]; disks: Obj[]; gpu: number; board: number };
            daemon?: Obj & { targets: Record<string, number> };
            calibration: { table: { time: number; fans: Record<string, number[][]> }; progress: Obj | null };
            chip_temps?: number[];
            net_io: { rx: number; tx: number }[];
        };
        const scenarios = (s as Obj).scenarios as Record<string, Obj> | undefined;
        delete (s as Obj).scenarios;
        const overlay = opts.scenario ? scenarios?.[opts.scenario] : undefined;
        if (overlay) merge(s, overlay);
        // DXP2800 GT after "Confirm fans": the chip answers with its fans, the reason for the missing chip is gone.
        const confirmed = fan.confirmed();
        if (confirmed) {
            delete (s as Obj).chip_error;
            Object.assign(s, confirmed);
        }
        const baseFans = (confirmed?.fans ?? (baseStatus as { fans?: unknown }).fans ?? []) as { id: string; pct: number; rpm: number }[];
        for (const k of NAS_KEYS) if (k in state.nas) s.settings[k] = state.nas[k];
        if (s.settings.fan_config) s.settings.fan_config = fan.config(s.settings);
        if (s.fan_caps) s.fan_caps = fan.caps();
        const p = prefs();
        if (p.ui_lang) s.settings.ui_lang = p.ui_lang;
        if (p.ui_theme) s.settings.ui_theme = p.ui_theme;
        s.led.enabled = state.led;
        const secs = Math.floor(now() / 1000);
        for (const [i, nic] of s.net_io.entries()) {
            nic.rx += secs * (NET_IO_RATES[i]?.[0] ?? 0);
            nic.tx += secs * (NET_IO_RATES[i]?.[1] ?? 0);
        }
        if (s.settings.fan_control === false) s.fans = [];

        if (state.calibStart !== null) {
            const step = Math.floor((now() - state.calibStart) / CALIB_STEP_MS);
            const steps = CALIB_STEPS * baseFans.length;
            if (step >= steps) {
                s.calibration.table.time = Math.floor((state.calibStart + steps * CALIB_STEP_MS) / 1000);
                s.calibration.progress = { state: 'done', step: steps, steps, time: Math.floor(now() / 1000) };
            } else {
                s.calibration.progress = {
                    state: 'running',
                    fan: baseFans[Math.floor(step / CALIB_STEPS)]?.id,
                    pct: ((step % CALIB_STEPS) + 1) * 10,
                    step,
                    steps,
                    time: Math.floor(now() / 1000)
                };
            }
        }
        if (!live) return s;

        // Drift of the readings, and the fan speed that follows the mode.
        s.sensors.cpu = temp(s.sensors.cpu, 'cpu', 4);
        s.sensors.gpu = temp(s.sensors.gpu, 'gpu');
        s.sensors.board = temp(s.sensors.board, 'board', 1);
        for (const n of s.sensors.nvme) n.temp = temp(n.temp as number, String(n.name));
        for (const n of s.sensors.nvme_extra) n.temp = temp(n.temp as number, `${n.name}${n.label}`);
        for (const n of s.sensors.nic) n.temp = temp(n.temp as number, `${n.iface}${n.label}`, 1);
        for (const d of s.sensors.disks) if (!d.standby) d.temp = temp(d.temp as number, String(d.name), 1);
        const caseTemp = Math.max(...s.sensors.disks.map(d => Number(d.temp)));
        if (s.chip_temps) s.chip_temps = s.chip_temps.map((v, i) => temp(v, `chip${i}`));
        if (s.daemon) {
            s.daemon.cpu_temp = s.sensors.cpu;
            s.daemon.case_temp = caseTemp;
            s.daemon.targets = {};
        }

        const mode = String(s.settings.mode);
        const factor = ({ quiet: 0.7, performance: 1.4 } as Record<string, number>)[mode] ?? 1;
        const hot = Math.min(1.6, Math.max(0.8, 1 + (s.sensors.cpu - baseStatus.sensors.cpu) / 20));
        for (const [i, fan] of (s.fans ?? []).entries()) {
            const f = baseFans[i]!;
            const pct = mode === 'full' ? 100 : Math.min(100, Math.max(Number(s.settings.min_pct), Math.round(f.pct * factor * hot)));
            fan.pct = mode === 'bios' ? null : pct;
            fan.auto = mode === 'bios';
            fan.rpm = Math.round((f.rpm * (mode === 'bios' ? 1 : pct / f.pct) * (1 + wave(String(fan.id), 1, 5) / 100)) / 10) * 10;
            if (mode !== 'bios' && s.daemon) s.daemon.targets[String(fan.id)] = pct;
        }
        return s;
    }

    const fanSettings = () => nasStatus().settings as Obj;

    function nasPost(form: URLSearchParams): Response {
        const settings = fanSettings();
        const fanReply: Reply | null = fan.post(form.get('action') ?? '', form, settings, settings.fan_control !== false);
        if (fanReply) return fanReply;
        const set = (changes: Obj) => {
            Object.assign(state.nas, changes);
            persist();
        };
        switch (form.get('action')) {
            case 'mode': {
                const value = form.get('value') ?? form.get('mode') ?? 'standard';
                if (!MODES.includes(value)) return fail('Invalid mode');
                set({ mode: value });
                break;
            }
            case 'min': {
                const min = int(form.get('min_pct'));
                if (min === null || min < 10 || min > 60) return fail('Invalid value for min_pct');
                set({ min_pct: min, ...(form.has('case_follows_cpu') && { case_follows_cpu: form.get('case_follows_cpu') === '1' }) });
                break;
            }
            case 'curves':
                set({
                    mode: 'custom',
                    cpu_curve: form.get('cpu_curve') ?? '',
                    case_curve: form.get('case_curve') ?? '',
                    ...(form.has('min_pct') && { min_pct: int(form.get('min_pct')) ?? 0 }),
                    ...(form.has('case_follows_cpu') && { case_follows_cpu: form.get('case_follows_cpu') === '1' })
                });
                break;
            case 'safety':
                set({
                    cpu_crit: int(form.get('cpu_crit')) ?? 90,
                    disk_crit: int(form.get('disk_crit')) ?? 55,
                    nvme_offset: int(form.get('nvme_offset')) ?? 15,
                    stall_notify: form.get('stall_notify') === '1',
                    ignore_drives: (form.get('ignore_drives') ?? '').split(',').filter(Boolean)
                });
                break;
            case 'fan_control':
                set({ fan_control: form.get('value') !== 'off' });
                break;
            case 'led':
                state.led = form.get('value') === 'on';
                persist();
                break;
            case 'lang': {
                const value = form.get('value') ?? form.get('ui_lang') ?? '';
                if (UI_LANGS.includes(value)) setPref('ui_lang', value);
                break;
            }
            case 'theme': {
                const value = form.get('value') ?? form.get('ui_theme') ?? '';
                if (THEMES.includes(value)) setPref('ui_theme', value);
                break;
            }
            case 'calibrate':
                state.calibStart = now();
                persist();
                break;
            default: // led_import and anything else: accepted, nothing to do
                break;
        }
        return ok();
    }

    const nightActive = (): boolean => {
        const n = state.night;
        if (!n.enabled) return false;
        const d = new Date(now());
        const at = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
        return n.start <= n.end ? at >= n.start && at < n.end : at >= n.start || at < n.end;
    };
    const daemonLeds = (): string[] => {
        const disks = Array.from({ length: 8 }, (_, i) => `disk${i + 1}`);
        return (state.ledCfg.netdev_status ?? led.status.netdev_status) === 'manual' ? disks : ['netdev', ...disks];
    };
    const ledStatus = (): Obj => ({ ...led.status, ...state.ledCfg });
    const statusMode = () => ledStatus().mode === 'status';

    function allLeds(): Record<string, Led> {
        const t = now();
        const out: Record<string, Led> = {};
        const night = nightActive();
        const cfg = ledStatus();
        const flicker = live && statusMode() && cfg.activity_enabled !== false;
        for (const [name, base] of Object.entries(led.all as Record<string, Led>)) {
            const l: Led = { ...base, ...state.leds[name] };
            if (flicker && /^disk/.test(name) && base.color === '255 255 255' && Number(base.brightness) > 100) {
                const slot = Math.floor(t / 700) + phase(name);
                l.brightness = String((slot * 2654435761) % 5 < 2 ? 70 : 200);
            }
            if ((state.identify[name] ?? 0) > t)
                Object.assign(l, { effect: 'blink', on_ms: '300', off_ms: '300', brightness: '255', sent_ms: String(t - 1), now_ms: String(t) });
            if (night && Number(l.brightness) > 0)
                l.brightness = String(state.night.action === 'off' ? 0 : Math.min(Number(l.brightness), state.night.brightness));
            if (l.sent_ms) l.now_ms = String(t);
            out[name] = l;
        }
        return out;
    }

    function ledGet(query: URLSearchParams): Response {
        switch (query.get('action') ?? 'status') {
            case 'status': {
                const t = now();
                const identify = Object.entries(state.identify)
                    .filter(([, until]) => until > t)
                    .map(([name]) => name);
                return {
                    status: 200,
                    json: {
                        ...ledStatus(),
                        settings_saved: true,
                        daemon: statusMode(),
                        night: nightActive(),
                        identify: identify.length ? identify : led.status.identify
                    }
                };
            }
            case 'list':
                return ok({ leds: led.list });
            case 'all':
                return ok({ leds: allLeds() });
            case 'bays':
                return ok({ bays: led.bays.map(b => (b.temp === null || b.standby ? b : { ...b, temp: temp(b.temp, b.led, 1) })) });
            case 'schedule':
                return ok({ ...state.night, action: state.night.action });
            case 'led': {
                const name = query.get('led') ?? '';
                if (!LED_NAME.test(name)) return fail('Invalid LED');
                const one = allLeds()[name];
                return one ? { status: 200, json: one } : fail(`no such LED: ${name}`, 500);
            }
            default:
                return fail('Unknown action');
        }
    }

    const rgbOf = (form: URLSearchParams, prefix = ''): [number, number, number] | null => {
        const v = ['r', 'g', 'b'].map(c => int(form.get(`${prefix}${c}`)));
        return v.every(n => n !== null && n >= 0 && n <= 255) ? (v as [number, number, number]) : null;
    };
    const RGB = 'RGB values must be 0-255';

    function ledPost(form: URLSearchParams): Response {
        const action = form.get('action') ?? '';
        const save = (changes: Obj) => {
            Object.assign(state.ledCfg, changes);
            persist();
            return ok();
        };
        const stopFx = (names: string[]) => {
            const fxLeds = String(ledStatus().fx_leds ?? '').split(',');
            if (ledStatus().fx && names.some(n => fxLeds.includes(n))) Object.assign(state.ledCfg, { fx: '', fx_leds: '' });
        };

        switch (action) {
            case 'mode': {
                const mode = form.get('value') ?? '';
                if (mode !== 'manual' && mode !== 'status') return fail('Invalid mode');
                if (mode === 'status') stopFx(daemonLeds());
                return save({ mode, daemon: mode === 'status' });
            }
            case 'shutdown': {
                const rgb = rgbOf(form);
                if (!rgb) return fail(RGB);
                const effect = form.get('effect') ?? '';
                if (effect !== 'blink' && effect !== 'breath') return fail('Invalid effect values');
                return save({ shutdown_blink: form.get('enabled') === '1', shutdown_color: hex(...rgb), shutdown_effect: effect });
            }
            case 'shutdown_test':
                return ok();
            case 'netdev_speed': {
                const colors = { ...(ledStatus().netdev_speed_colors as Record<string, string>) };
                for (const step of [10, 100, 1000, 2500, 5000, 10000]) {
                    const value = form.get(`speed_${step}`) ?? '';
                    if (value === '') continue;
                    const color = rgbText(value);
                    if (!color) return fail(RGB);
                    colors[String(step)] = color;
                }
                return save({ netdev_speed_enabled: form.get('speed_enabled') === '1', netdev_speed_colors: colors });
            }
            case 'status_display': {
                const rgb = rgbOf(form);
                const smart = rgbOf(form, 'smart_');
                if (!rgb || !smart) return fail(RGB);
                const colorMode = form.get('disk_color_mode') ?? '';
                const standbyMode = form.get('standby_mode') ?? '';
                const level = int(form.get('standby_level'));
                if (
                    !['own', 'temperature'].includes(colorMode) ||
                    !['normal', 'dim', 'off', 'pulse'].includes(standbyMode) ||
                    level === null ||
                    level < 1 ||
                    level > 100
                )
                    return fail('Invalid status display values');
                return save({
                    sync_enabled: form.get('sync_enabled') === '1',
                    sync_color: hex(...rgb),
                    standby_mode: standbyMode,
                    standby_level: level,
                    disk_color_mode: colorMode,
                    smart_enabled: form.get('smart_enabled') === '1',
                    error_enabled: form.get('error_enabled') !== '0',
                    activity_enabled: form.get('activity_enabled') !== '0',
                    smart_color: hex(...smart)
                });
            }
            case 'netdev_status': {
                const value = form.get('value') ?? '';
                if (!['activity', 'link', 'manual'].includes(value)) return fail('Invalid network LED setting');
                save({ netdev_status: value });
                if (statusMode()) stopFx(daemonLeds());
                return ok();
            }
            case 'smart_ack':
                return fail('No SMART values for this bay');
            case 'alerts': {
                const rgb = rgbOf(form);
                if (!rgb) return fail(RGB);
                const level = form.get('level') ?? '';
                if (level !== 'warning' && level !== 'alert') return fail('Invalid alert level');
                return save({ alert_enabled: form.get('enabled') === '1', alert_level: level, alert_color: hex(...rgb) });
            }
            case 'identify': {
                const name = form.get('led') ?? '';
                if (!/^disk[1-8]$/.test(name)) return fail('Invalid LED');
                if (form.get('value') === 'stop') delete state.identify[name];
                else state.identify[name] = now() + 30000;
                persist();
                return ok();
            }
            case 'fx': {
                const name = form.get('value') ?? '';
                if (name === 'none') return save({ fx: '', fx_leds: '' });
                const names = (form.get('led') ?? '').split(',');
                const speed = int(form.get('speed'));
                if (names.some(n => !LED_NAME.test(n))) return fail('Invalid LED');
                if (!['rainbow', 'temperature'].includes(name) || speed === null || speed < 0 || speed > 4) return fail('Invalid effect values');
                if (statusMode() && names.some(n => daemonLeds().includes(n)))
                    return fail('In status mode effects apply to the power LED (and a manual network LED) only');
                return save({
                    fx: name,
                    fx_leds: names.join(','),
                    fx_speed: speed,
                    fx_reverse: form.get('reverse') === '1',
                    fx_idle: form.get('idle') === '1'
                });
            }
            case 'schedule': {
                const time = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;
                const [start, end, night] = [form.get('start') ?? '', form.get('end') ?? '', form.get('night_action') ?? ''];
                const brightness = int(form.get('brightness'));
                if (!time.test(start) || !time.test(end) || !['dim', 'off'].includes(night) || brightness === null || brightness < 0 || brightness > 255)
                    return fail('Invalid schedule values');
                state.night = { enabled: form.get('enabled') === '1', start, end, action: night, brightness };
                persist();
                return ok();
            }
            case 'error_color': {
                const rgb = rgbOf(form);
                return rgb ? save({ disk_error_color: hex(...rgb) }) : fail(RGB);
            }
        }

        const names = (form.get('led') ?? '').split(',');
        if (names.some(n => !LED_NAME.test(n))) return fail('Invalid LED');
        let change: Partial<Led>;
        switch (action) {
            case 'on':
                change = { brightness: '255' };
                break;
            case 'off':
                change = { brightness: '0' };
                break;
            case 'brightness': {
                const v = int(form.get('value'));
                if (v === null || v < 0 || v > 255) return fail('Brightness must be 0-255');
                change = { brightness: String(v) };
                break;
            }
            case 'color': {
                const rgb = rgbOf(form);
                if (!rgb) return fail(RGB);
                change = { color: rgb.join(' ') };
                break;
            }
            case 'blink':
            case 'breath': {
                const [on, off] = [int(form.get('on')), int(form.get('off'))];
                const min = action === 'blink' ? 50 : 100;
                if (on === null || off === null || on < min || on > 10000 || off < min || off > 10000)
                    return fail(action === 'blink' ? 'Blink values must be 50-10000 ms' : 'Breath values must be 100-10000 ms');
                change = { effect: action, on_ms: String(on), off_ms: String(off), sent_ms: String(now()) };
                break;
            }
            case 'stop':
                change = { effect: 'none', on_ms: '0', off_ms: '0', sent_ms: '' };
                break;
            default:
                return fail('Unknown command');
        }
        const missing = names.filter(n => !(n in led.all));
        if (action !== 'brightness') stopFx(names);
        for (const n of names) if (!missing.includes(n)) state.leds[n] = { ...state.leds[n], ...change };
        persist();
        return missing.length ? fail(missing.map(n => `${n}: no such LED`).join('\n'), 500) : ok({ leds: names });
    }

    return {
        handle(method: string, path: string, query: URLSearchParams, form: URLSearchParams): Response {
            const isLed = /\/led\/api\.php$/.test(path);
            if (method === 'POST') return isLed ? ledPost(form) : nasPost(form);
            if (method !== 'GET') return fail('Method not allowed', 405);
            if (isLed) return ledGet(query);
            const settings = fanSettings();
            const fanReply = fan.get(query.get('action') ?? '', query, settings, settings.fan_control !== false);
            if (fanReply) return fanReply;
            return { status: 200, json: query.get('action') === 'hardware' ? hardware(model) : nasStatus() };
        }
    };
}
