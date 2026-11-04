// The fan actions of api.php (inputs, schedule, profiles, v2 settings, probe, backlight) for the in-browser demo; the
// answers follow web/tools/harness/api-fixture.php. Their state lives under `nas.fan` of the model's saved state.
import { fanConfig } from '../models/fixture';
import { shape } from './variants';
import { fanHistory } from './fixture-history';
import type { Model } from '../models/schema';

type Obj = Record<string, any>;
export interface Reply {
    status: number;
    json: unknown;
}

const DAYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'];
const GROUP_FIELDS = ['input', 'deadband', 'start_pct', 'start_ms', 'min_run', 'up_step', 'down_step', 'on_missing', 'allow_off'];
const V2_KEYS = [
    'quiet_enabled',
    'quiet_from',
    'quiet_to',
    'quiet_days',
    'quiet_cap',
    'quiet_scope',
    'job_mover',
    'job_parity',
    'job_mover_pct',
    'job_parity_pct'
];
const INT_FIELDS = ['deadband', 'start_pct', 'start_ms', 'min_run', 'up_step', 'down_step'];
const TOKEN = 'a'.repeat(64);
const CHANNELS = ['fan1', 'fan2', 'fan3', 'fan4'];
const V1_INT = ['min_pct', 'cpu_crit', 'disk_crit', 'nvme_offset'];
const V1_BOOL = ['case_follows_cpu', 'stall_notify'];
const V1_TEXT = ['mode', 'cpu_curve', 'case_curve'];

const fail = (error: string, status = 400): Reply => ({ status, json: { ok: false, error } });
const ok = (extra: Obj = {}): Reply => ({ status: 200, json: { ok: true, ...extra } });

const intIn = (v: string, lo: number, hi: number) => /^[+-]?\d{1,9}$/.test(v) && Number(v) >= lo && Number(v) <= hi;

function validCurveText(text: string): boolean {
    const points = text.split(',');
    if (points.length < 2 || points.length > 8) return false;
    let last = -1;
    for (const p of points) {
        const m = /^(\d{1,3}):(\d{1,3})$/.exec(p);
        if (!m) return false;
        const [t, pct] = [Number(m[1]), Number(m[2])];
        if (t > 120 || pct > 100 || t <= last) return false;
        last = t;
    }
    return true;
}

function inputPart(spec: string): boolean {
    const drive = '[A-Za-z0-9_.-]{1,40}';
    return (
        ['cpu', 'nvme', 'gpu', 'hba', 'disk_max', 'disk_avg'].includes(spec) ||
        new RegExp(`^disk_(max|avg):${drive}(\\+${drive}){0,31}$`).test(spec) ||
        /^nic:[A-Za-z0-9_.@-]{1,15}$/.test(spec) ||
        /^script:[a-z0-9_-]{1,32}$/.test(spec)
    );
}

function validInput(spec: string): boolean {
    if (spec.length > 256) return false;
    if (spec === '' || spec === 'auto') return true;
    if (spec.startsWith('max:')) {
        const parts = spec.slice(4).split('|');
        return parts.length >= 2 && parts.length <= 8 && parts.every(inputPart);
    }
    return inputPart(spec);
}

function groupField(field: string, value: string): boolean {
    if (field === 'input') return validInput(value);
    if (field === 'curve') return validCurveText(value);
    if (field === 'deadband') return intIn(value, 0, 10);
    if (field === 'start_pct') return intIn(value, 0, 100);
    if (field === 'start_ms') return intIn(value, 0, 10000);
    if (field === 'min_run') return intIn(value, 10, 100);
    if (field === 'up_step' || field === 'down_step') return intIn(value, 1, 100);
    if (field === 'on_missing') return ['fallback', 'curve_low', 'hold'].includes(value);
    if (field === 'allow_off') return value === '0' || value === '1';
    return false;
}

function known(key: string): boolean {
    let m = /^fan\.([a-z0-9]+)\.([a-z_]+)$/.exec(key);
    if (m) return CHANNELS.includes(m[1]!) && (GROUP_FIELDS.includes(m[2]!) || m[2] === 'curve');
    m = /^(cpu|case)_([a-z_]+)$/.exec(key);
    if (m) return GROUP_FIELDS.includes(m[2]!);
    return V2_KEYS.includes(key);
}

function valid(key: string, value: string): boolean {
    if (!known(key)) return false;
    const g = /^(?:fan\.[a-z0-9]+\.|(?:cpu|case)_)([a-z_]+)$/.exec(key);
    if (g) return groupField(g[1]!, value);
    switch (key) {
        case 'quiet_enabled':
            return value === '0' || value === '1';
        case 'quiet_from':
        case 'quiet_to':
            return /^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(value);
        case 'quiet_days':
            return value.split(',').every(d => DAYS.includes(d));
        case 'quiet_cap':
            return intIn(value, 10, 100);
        case 'quiet_scope':
            return value === 'case' || value === 'all';
        case 'job_mover':
        case 'job_parity':
            return ['off', 'floor', 'boost'].includes(value);
        default:
            return intIn(value, 1, 100);
    }
}

export interface FanHost {
    nas: Obj;
    persist: () => void;
    now: () => number;
}

export function createFan(model: Model, host: FanHost) {
    const shaped = shape(model);
    const samples = shaped.fan;
    const caps = (shaped.status as { fan_caps?: { kind: string; channels: number } }).fan_caps ?? null;
    const fan = (): Obj => (host.nas.fan ??= {});
    const roleOf = (id: string): string => model.fan?.fans[id] ?? (id === 'fan1' ? 'cpu' : 'case');
    const dmi = model.fixtureDmi;
    const probeModel = model.fan?.kind === 'probe';

    function config(settings: Obj): Obj {
        const cfg = fanConfig(Number(settings.min_pct ?? 25)) as unknown as Obj;
        if (settings.cpu_curve) cfg.cpu.curve = settings.cpu_curve;
        if (settings.case_curve) cfg.case.curve = settings.case_curve;
        const v2: Record<string, string> | undefined = fan().v2;
        if (!v2) return cfg;
        cfg.version = 2;
        const set = (group: Obj, field: string, value: string) => {
            group[field] = INT_FIELDS.includes(field) ? Number(value) : field === 'allow_off' ? value === '1' : value;
        };
        for (const [key, value] of Object.entries(v2)) {
            let m = /^(cpu|case)_([a-z_]+)$/.exec(key);
            if (m) {
                set(cfg[m[1]!], m[2]!, value);
                continue;
            }
            m = /^fan\.([a-z0-9]+)\.([a-z_]+)$/.exec(key);
            if (m) {
                const group = (cfg.channels[m[1]!] ??= { ...cfg[roleOf(m[1]!)] });
                set(group, m[2]!, value);
                continue;
            }
            const [area, field] = key.startsWith('quiet_') ? ['quiet', key.slice(6)] : ['job', key.slice(4)];
            if (area === 'quiet') cfg.quiet[field!] = field === 'enabled' ? value === '1' : field === 'cap' ? Number(value) : value;
            else {
                const [, job, rest] = /^(mover|parity)_?(pct)?$/.exec(field!) ?? [];
                if (job) cfg[job][rest ? 'pct' : 'mode'] = rest ? Number(value) : value;
            }
        }
        cfg.quiet.enabled = cfg.quiet.enabled && cfg.quiet.from !== cfg.quiet.to;
        return cfg;
    }

    function backlightOnly(): Reply | null {
        return samples.meta.backlight ? null : fail(`${dmi} has no backlight control: it exists for the iDX6011 Pro only`, 409);
    }

    // The DXP2800 GT only (Chipsea CS201x): the sample of `cs201x`, else the same refusal as the tool.
    function cs201x(): { read: Obj; confirmed: Obj } | Reply {
        return shaped.cs201x ?? fail(`"${dmi}" is not a DXP2800 GT`, 409);
    }

    const mapOf = (): Obj | null => fan().map ?? null;

    // Power on after AC loss: the model's sample, or the refusal the tool gives where nothing can be reached.
    function powerAc(): { readable: boolean; settable: string[]; source: string; mode: string } | Reply {
        return shaped.powerAc ?? fail('this model has no power-on setting this tool can reach', 409);
    }
    const powerAcAnswer = (ac: { readable: boolean; settable: string[]; source: string; mode: string }): Obj => ({
        model: dmi,
        mode: ac.readable ? (fan().powerAc?.mode ?? ac.mode) : 'unknown',
        readable: ac.readable,
        raw: ac.readable ? { value: 1 } : {},
        settable: ac.settable,
        source: ac.source,
        last_set: fan().powerAc?.last ?? null
    });

    function probeStatus(): Obj {
        const kind = caps ? caps.kind : '';
        const map = mapOf();
        return {
            ok: true,
            model: dmi,
            kind,
            map,
            proposal: fan().proposal ?? null,
            progress: fan().progress ?? null,
            fan_control_possible: kind === 'fixed' || (kind === 'probe' && map !== null)
        };
    }

    function previewOf(source: string, settings: Obj, profile: Record<string, string>, title: string, own: boolean): Obj {
        const cur = (key: string): string | null => {
            if (V1_BOOL.includes(key)) return settings[key] === undefined ? null : settings[key] ? '1' : '0';
            if (key === 'ignore_drives') return (settings.ignore_drives ?? []).join(',');
            return settings[key] === undefined ? null : String(settings[key]);
        };
        const diff = Object.entries(profile)
            .map(([key, to]) => ({ key, from: cur(key) ?? fan().v2?.[key] ?? null, to }))
            .filter(d => d.from !== d.to);
        return {
            ok: true,
            source,
            profile,
            diff,
            warnings: [],
            not_imported: [],
            token: TOKEN,
            title,
            unchanged: Object.keys(profile).length - diff.length,
            provenance: Object.fromEntries(Object.keys(profile).map(k => [k, own ? 'profile file' : 'dynamix.cfg'])),
            v2: Object.keys(profile).some(k => known(k))
        };
    }

    function profileOf(form: URLSearchParams): { source: string; profile: Record<string, string>; title: string } | Reply {
        const source = form.get('source') ?? '';
        if (source !== '') {
            if (!['fanctrlplus', 'fanctrlplus2', 'dynamix', 'itkuny'].includes(source)) return fail('Unknown source');
            if (source !== 'dynamix') return fail(`No ${source} settings found on this NAS`);
            return { source, profile: samples.profile, title: 'Dynamix Auto Fan Control' };
        }
        const text = form.get('content') ?? '';
        if (text === '') return fail('Give a source or a file');
        try {
            const file = JSON.parse(text);
            if (file?.kind !== 'ugreen-nas-fan-profile' || typeof file.fan !== 'object') throw new Error();
            return { source: 'own', profile: file.fan as Record<string, string>, title: 'Profile file' };
        } catch {
            return fail('Not a profile file');
        }
    }

    function values(form: URLSearchParams, name: string): Record<string, string> {
        const out: Record<string, string> = {};
        for (const [k, v] of form) {
            const m = new RegExp(`^${name}\\[(.+)\\]$`).exec(k);
            if (m) out[m[1]!] = v;
        }
        return out;
    }

    function applyChanges(changes: Record<string, string>, remove: string[] = []): void {
        const f = fan();
        const v2: Record<string, string> = { ...(f.v2 ?? {}) };
        const v1: Obj = {};
        for (const [key, value] of Object.entries(changes)) {
            if (known(key)) v2[key] = value;
            else if (V1_INT.includes(key)) v1[key] = Number(value);
            else if (V1_BOOL.includes(key)) v1[key] = value === '1';
            else if (V1_TEXT.includes(key)) v1[key] = value;
            else if (key === 'ignore_drives') v1[key] = value.split(',').filter(Boolean);
        }
        for (const key of remove) delete v2[key];
        if (Object.keys(changes).some(k => known(k)) || remove.length) f.v2 = v2;
        Object.assign(host.nas, v1);
        host.persist();
    }

    return {
        config,
        /** `fan_caps` of the status: a confirmed map gives the probe model its channels. */
        caps: () => (caps ? { ...caps, channels: mapOf() ? mapOf()!.outputs.length : caps.channels } : null),
        /** The DXP2800 GT with confirmed fans: the status fields the chip adds (the fixture's `cs201x.confirmed`). */
        confirmed: (): Obj | null => (shaped.cs201x && mapOf() ? structuredClone(shaped.cs201x.confirmed) : null),

        get(action: string, query: URLSearchParams, settings: Obj, fanControl: boolean): Reply | null {
            switch (action) {
                case 'fan_inputs': {
                    const cfg = config(settings);
                    const group = (g: 'cpu' | 'case') => {
                        const spec: string = cfg[g].input;
                        const opt = samples.inputs.options.find(o => o.spec === spec);
                        return { spec, reading: opt?.reading ?? null, usable: Boolean(opt) };
                    };
                    return ok({ ...structuredClone(samples.inputs), groups: { cpu: group('cpu'), case: group('case') } });
                }
                case 'fan_schedule': {
                    const q = config(settings).quiet;
                    return ok({ quiet: { ...q, active: false }, job: { mover: false, parity: false, active: '' }, cap: null });
                }
                case 'fan_history': {
                    const range = query.get('range') ?? '1h';
                    if (range !== '1h' && range !== '24h') return fail('Invalid range');
                    return { status: 200, json: fanHistory(model, Math.floor(host.now() / 1000), range) };
                }
                case 'fan_profile_sources':
                    return ok({ sources: samples.sources });
                case 'fan_profile_export': {
                    const cfg = config(settings);
                    const fanKeys: Record<string, string> = {
                        mode: String(settings.mode ?? 'standard'),
                        min_pct: String(settings.min_pct ?? 25),
                        cpu_curve: cfg.cpu.curve,
                        case_curve: cfg.case.curve,
                        ...(fan().v2 ?? {})
                    };
                    return {
                        status: 200,
                        json: { schema: 1, kind: 'ugreen-nas-fan-profile', model: dmi, created: new Date(host.now()).toISOString(), fan: fanKeys }
                    };
                }
                case 'fan_probe_status':
                    return { status: 200, json: probeStatus() };
                case 'fan_probe_tachs':
                    if (!probeModel) return fail(`${dmi} has no fan probe`, 409);
                    if (!fanControl) return fail('fan control is switched off in the settings', 409);
                    return ok({ tachs: samples.probe?.tachs ?? [] });
                case 'fan_backlight': {
                    const no = backlightOnly();
                    return no ?? ok({ ...(fan().backlight ?? samples.backlight) });
                }
                case 'cs201x_read': {
                    const cs = cs201x();
                    return 'status' in cs ? cs : ok({ read: cs.read });
                }
                case 'power_ac_get': {
                    const ac = powerAc();
                    return 'status' in ac ? ac : ok(powerAcAnswer(ac));
                }
                case 'panel_buttons':
                    if (!shaped.panel.buttons) return fail(`no button map for model "${dmi}"`, 409);
                    return ok({
                        model: dmi,
                        power: shaped.panel.power,
                        reset: shaped.panel.reset,
                        power_readable: shaped.panel.power_readable,
                        time: Math.floor(host.now() / 1000)
                    });
                default:
                    void query;
                    return null;
            }
        },

        post(action: string, form: URLSearchParams, settings: Obj, fanControl: boolean): Reply | null {
            switch (action) {
                case 'fan_v2': {
                    const changes = values(form, 'values');
                    for (const [k, v] of Object.entries(changes)) if (!valid(k, v)) return fail(`Invalid value for ${k}`);
                    const remove = (form.get('reset') ?? '').split(',').filter(Boolean);
                    if (!remove.every(k => known(k))) return fail('Invalid value for reset');
                    if (Object.keys(changes).length === 0 && remove.length === 0) return fail('Nothing to change');
                    const merged = {
                        ...config(settings).quiet,
                        ...Object.fromEntries(
                            Object.entries(changes)
                                .filter(([k]) => k.startsWith('quiet_'))
                                .map(([k, v]) => [k.slice(6), v])
                        )
                    };
                    const enabled = changes.quiet_enabled !== undefined ? changes.quiet_enabled === '1' : fan().v2?.quiet_enabled === '1';
                    if (enabled && merged.from === merged.to) return fail('Invalid value for quiet_to');
                    applyChanges(changes, remove);
                    return ok();
                }
                case 'fan_input_test': {
                    const spec = form.get('spec') ?? '';
                    if (spec === '' || !validInput(spec)) return fail('Invalid input');
                    const opt = samples.inputs.options.find(o => o.spec === spec);
                    if (spec.startsWith('script:')) return ok({ spec, reading: null, usable: false, detail: 'scripts run only in the daemon' });
                    return ok({ spec, reading: opt?.reading ?? null, usable: Boolean(opt) });
                }
                case 'fan_profile_preview': {
                    const p = profileOf(form);
                    return 'status' in p ? p : { status: 200, json: previewOf(p.source, settings, p.profile, p.title, p.source === 'own') };
                }
                case 'fan_profile_apply': {
                    const token = form.get('token') ?? '';
                    if (!/^[0-9a-f]{64}$/.test(token)) return fail('Invalid token');
                    const p = profileOf(form);
                    if ('status' in p) return p;
                    if (token !== TOKEN) return fail('The settings changed since the preview');
                    const entries = Object.entries(p.profile);
                    if (!entries.length) return fail('Nothing to apply');
                    for (const [k, v] of entries)
                        if (typeof v !== 'string' || (known(k) ? !valid(k, v) : ![...V1_INT, ...V1_BOOL, ...V1_TEXT, 'ignore_drives'].includes(k)))
                            return fail(`Invalid value for ${k}`);
                    applyChanges(p.profile);
                    return ok({ applied: entries.length });
                }
                case 'fan_probe_run': {
                    if (!probeModel) return fail(`${dmi} has no fan probe`, 409);
                    if (!fanControl) return fail('fan control is switched off in the settings', 409);
                    const time = Math.floor(host.now() / 1000);
                    const proposal = { ...structuredClone(samples.probe!.proposal), time };
                    const steps = proposal.outputs.length;
                    Object.assign(fan(), { proposal, progress: { state: 'done', step: steps, steps, time } });
                    host.persist();
                    return ok();
                }
                case 'fan_probe_confirm': {
                    const choice = values(form, 'map');
                    const pwms = Object.keys(choice);
                    if (
                        !pwms.length ||
                        pwms.length > 9 ||
                        pwms.some(p => !/^pwm[1-9]$/.test(p)) ||
                        Object.values(choice).some(r => r !== 'cpu' && r !== 'case')
                    )
                        return fail('Invalid map');
                    if (!probeModel) return fail(`${dmi} has no fan probe`, 409);
                    const proposal = fan().proposal;
                    if (!proposal) return fail('no fresh probe result for this NAS: run the fan probe first', 409);
                    const outputs: Obj[] = [];
                    for (const pwm of pwms.sort()) {
                        const o = proposal.outputs.find((x: Obj) => x.pwm === pwm);
                        if (!o) return fail(`${pwm} is not in the probe result`, 409);
                        if (o.status !== 'mapped') return fail(`${pwm} has no single fan (${o.status}); it cannot be confirmed`, 409);
                        outputs.push({ id: pwm.replace('pwm', 'fan'), role: choice[pwm], pwm, tach: o.tach });
                    }
                    const map = { version: 1, model: dmi, chip: proposal.chip, time: Math.floor(host.now() / 1000), outputs };
                    Object.assign(fan(), { map, proposal: null });
                    host.persist();
                    return ok({ map });
                }
                case 'fan_probe_forget':
                    delete fan().map;
                    host.persist();
                    return ok();
                case 'cs201x_confirm': {
                    const cs = cs201x();
                    if ('status' in cs) return cs;
                    const raw = form.get('fans') ?? '';
                    if (raw !== '' && !/^(fan1|fan3)(,(fan1|fan3))?$/.test(raw)) return fail('Invalid fans');
                    const ids = raw === '' ? ['fan1', 'fan3'] : [...new Set(raw.split(','))];
                    const outputs = ids.map(id => ({ id, role: id === 'fan1' ? 'cpu' : 'case', pwm: 'duty', tach: 'rpm' }));
                    const map = { version: 1, model: dmi, chip: 'CS201x', time: Math.floor(host.now() / 1000), outputs };
                    fan().map = map;
                    host.persist();
                    return ok({ map });
                }
                case 'cs201x_forget': {
                    const cs = cs201x();
                    if ('status' in cs) return cs;
                    delete fan().map;
                    host.persist();
                    return ok();
                }
                case 'power_ac_set': {
                    const mode = form.get('mode') ?? '';
                    if (!['on', 'off', 'last'].includes(mode)) return fail('Invalid mode');
                    const ac = powerAc();
                    if ('status' in ac) return ac;
                    if (!ac.settable.includes(mode)) return fail('this model does not offer that mode: no "last state" value here', 409);
                    fan().powerAc = { mode, last: { mode, time: Math.floor(host.now() / 1000) } };
                    host.persist();
                    return ok({ set: mode, ...powerAcAnswer(ac), verified: ac.readable });
                }
                case 'panel_beep': {
                    const raw = form.get('ms') ?? '200';
                    if (!/^([2-9][0-9]|[1-9][0-9]{2}|1[0-9]{3}|2000)$/.test(raw)) return fail('Invalid duration');
                    if (!shaped.panel.beeper) return fail(`no beeper known for model "${dmi}"`, 409);
                    return ok({ ms: Number(raw) });
                }
                case 'fan_backlight': {
                    const raw = form.get('pct') ?? '';
                    if (!/^(100|[1-9]?[0-9])$/.test(raw)) return fail('Invalid value');
                    const no = backlightOnly();
                    if (no) return no;
                    const pct = Number(raw);
                    fan().backlight = { pct, raw: Math.round((pct * 255) / 100) };
                    host.persist();
                    return ok({ pct });
                }
                default:
                    return null;
            }
        }
    };
}
