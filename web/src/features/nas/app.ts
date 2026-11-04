// The settings page: Overview, Case, Sensors, Fans, Safety, LEDs, Settings, About. One closure over the page state;
// the pure parts (curves, formats, texts, api client) live in ../shared and ./names.
import { createClient, errorMessage } from '../../core/api';
import { isMode } from '../../core/contract';
import { isHardwareResponse, isLedAllResponse, isLedStatusResponse, isStaleBundle, isStatusResponse } from '../../core/guard';
import type {
    Bay,
    Dimm,
    NetIo,
    NetPort,
    LedPart,
    CalibRows,
    Calibration,
    Conflict,
    Fan,
    Info,
    Nvme,
    Sensors,
    Hardware,
    LedGet,
    LedSchedule,
    LedState,
    LedStatusResponse,
    Mode,
    NasGet,
    NasPost,
    NasPostReply,
    Settings,
    StatusResponse
} from '../../core/contract';
import { caseModel, frontHtml, socket, SOCKET, type SocketKind } from './components/front';
import { berlin, biosDate, chipName, deg, fmtRam, fmtSize, friendly, LIMITS, shortCpu, speedText, tempClass, type TempKind } from '../../core/format';
import { curveAt, curveText, effectivePoints, parseCurve, rpmAt as rpmAtRows, rpmTicks as rpmTicksOf, validCurve, type CurvePoint } from '../../core/curve';
import { hit, setRangeFill } from '../../core/dom';
import { fanIcon } from '../../core/fanspin';
import { escapeHtml } from '../../core/html';
import {
    btn,
    cardHead,
    chipCheck,
    emptyText,
    glyph,
    icon,
    iconOf,
    linkBtn,
    linkRow,
    meter,
    note,
    pill,
    rangeField,
    saveBar,
    sectionHead,
    switchRow,
    type Ico,
    type IconName
} from './ui/components';
import { resolveTheme } from '../../core/theme';
import { fanName, ssdRoleText } from './names';
import { type Key, t, tEnum, tRaw } from './text';
import './fan/index';
import { isCs201xModel } from './fan/cs201x';
import { hasBeeper, hasButtons } from './panel-models';
import { fanModules, type FanCtx, type FanModule } from './fan/module';
import * as components from './ui/components';

function main(root: HTMLElement): void {
    const API = '/plugins/ugreen-nas-control/api.php';
    const LED_API = '/plugins/ugreen-nas-control/led/api.php';

    root.dataset.theme = resolveTheme(window.UGREEN_NAS_THEME);

    const find = <T extends HTMLElement = HTMLElement>(selector: string): T | null => root.querySelector<T>(selector);
    const q = <T extends HTMLElement = HTMLElement>(selector: string, within: ParentNode = root): T => {
        const element = within.querySelector<T>(selector);
        if (!element) throw new Error(`Missing ${selector}`);
        return element;
    };
    const qa = <T extends HTMLElement = HTMLElement>(selector: string): T[] => [...root.querySelectorAll<T>(selector)];

    const MODES: { id: Mode; icon: IconName }[] = [
        { id: 'bios', icon: 'bios' },
        { id: 'quiet', icon: 'leaf' },
        { id: 'standard', icon: 'gauge' },
        { id: 'performance', icon: 'flame' },
        { id: 'full', icon: 'max' },
        { id: 'custom', icon: 'pen' }
    ];

    type Kind = 'cpu' | 'case';
    // 'curves' and 'safety' are the built-in save pages; every fan module adds one named by its id.
    type FanPage = string;

    const CHART: Record<Kind, { min: number; max: number }> = {
        cpu: { min: 20, max: 100 },
        case: { min: 20, max: 70 }
    };

    interface State {
        data: StatusResponse | null;
        curves: Record<Kind, CurvePoint[]>;
        caseFollows: boolean;
        minEdit: number | null;
        flowStep: string | null;
        calibError: string;
        calibStarting: boolean;
        arm: boolean;
        curvesDirty: boolean;
        safetyDirty: boolean;
        moduleDirty: Record<FanPage, boolean>;
        saveError: Record<FanPage, string>;
        edits: Record<FanPage, number>;
        touched: Record<FanPage, Set<string>>;
        saved: Record<FanPage, boolean>;
        busy: boolean;
        hw: Hardware | null;
        hwError: boolean;
        view: string;
        nav: string;
        leds: Record<string, LedState> | null;
        ledMode: { mode: string; fx: string } | null;
        ledSchedule: LedSchedule | null;
        ledStatus: LedStatusResponse | null;
        ledMisses: number;
        ledBays: number;
    }

    const state: State = {
        data: null,
        curves: { cpu: [], case: [] },
        caseFollows: false,
        minEdit: null,
        flowStep: null,
        calibError: '',
        calibStarting: false,
        // "Customize this curve" in a preset mode: the editor holds a copy and the next save switches to Custom.
        arm: false,
        curvesDirty: false,
        safetyDirty: false,
        moduleDirty: {},
        saveError: { curves: '', safety: '' },
        edits: { curves: 0, safety: 0 },
        touched: { curves: new Set(), safety: new Set() },
        saved: { curves: false, safety: false },
        busy: false,
        hw: null,
        hwError: false,
        view: 'overview',
        nav: 'overview',
        leds: null,
        ledMode: null,
        ledSchedule: null,
        ledStatus: null,
        ledMisses: 0,
        ledBays: 0
    };

    const api = createClient<NasGet, NasPost, NasPostReply>({
        base: API,
        csrf: () => window.UGREEN_NAS_CSRF_TOKEN || '',
        reads: 'direct',
        checks: { status: isStatusResponse, hardware: isHardwareResponse }
    });
    const ledApi = createClient<LedGet, object>({
        base: LED_API,
        csrf: () => '',
        reads: 'shared',
        checks: { status: isLedStatusResponse, all: isLedAllResponse }
    });

    const loaded = (): StatusResponse => {
        if (!state.data) throw new Error('No data');
        return state.data;
    };

    // Components: one render function and one CSS family (.unc-<name> in app.css) each. Static markup in
    // UGREENNASControl.page holds <span data-unc="<name>" data-unc-<param>="…"> instead; mountStatic() swaps it in
    // at start. Attributes without the data-unc prefix go to the component's main element.

    function fanTile(fan: Fan, fans: Fan[], { sub, hint = '', why = '' }: { sub: string; hint?: string; why?: string | undefined }): string {
        const running = fan.rpm > 0 && !fan.error;
        const rpm = fan.rpm > 0 ? `${fan.rpm} <small>${escapeHtml(t('fan.rpm'))}</small>` : escapeHtml(t(fan.error ? 'fan.error' : 'fan.stopped'));
        const power = fan.error ? '' : fan.pct == null ? (fanReadOnly() ? '' : t('fan.auto')) : t('fan.speed', { pct: fan.pct });

        return `
            <div class="unc-fantile ${running ? '' : 'stopped'}" ${hint ? `title="${escapeHtml(hint)}"` : ''}>
                ${fanIcon(fan, icon('fan'))}
                <div class="unc-fantile-text">
                    <strong>${escapeHtml(fanName(fan, fans))}</strong>
                    <span>${escapeHtml(sub)}</span>
                    ${why ? `<em>${escapeHtml(why)}</em>` : ''}
                </div>
                <div class="unc-fantile-val"><b>${rpm}</b><span>${escapeHtml(power)}</span></div>
                ${meter(fan.pct ?? 0)}
            </div>`;
    }

    const navLink = (nav: string, key: Key): string => linkBtn(t(key), `data-nav="${nav}"`);

    // Static spots in the page that hold a component: <span data-unc="btn" data-unc-label="<i18n key>" …>. Parameters are
    // data-unc-*; every other attribute is passed on. Inner html (card head actions) is kept. Buttons first: they sit in heads.
    function mountStatic() {
        interface Mounted {
            p: (key: string) => string | null;
            text: (key: string) => string;
            ico: () => Ico;
            attrs: string;
            inner: string;
        }
        const mount = (name: string, render: (parts: Mounted) => string): void =>
            qa(`[data-unc="${name}"]`).forEach(el => {
                const p = (key: string): string | null => el.getAttribute(`data-unc-${key}`);
                const text = (key: string): string => {
                    const textKey = p(key);
                    return textKey ? tRaw(textKey) : '';
                };
                const ico = (): Ico => (p('ico') || '') as Ico;
                const attrs = [...el.attributes]
                    .filter(a => !a.name.startsWith('data-unc'))
                    .map(a => `${a.name}="${escapeHtml(a.value)}"`)
                    .join(' ');

                el.outerHTML = render({ p, text, ico, attrs, inner: el.innerHTML });
            });

        mount('btn', ({ p, text, ico, attrs }) => btn({ label: text('label'), ico: ico(), tone: p('tone') || '', small: p('small') != null, attrs }));
        mount('cardhead', ({ p, text, ico, inner }) =>
            cardHead(text('title'), { ico: ico(), actions: inner, tag: p('tag') || 'div', titleId: p('title-id') || '' })
        );
        mount('sechead', ({ p, text, inner }) => sectionHead(text('title'), { actions: inner, id: p('id') || '' }));
        mount('switch', ({ p, text, attrs }) =>
            switchRow({ id: p('input'), title: text('title'), hint: text('hint'), hintId: p('hint-id') || '', cls: p('cls') || '', attrs })
        );
        mount('range', ({ p }) =>
            rangeField({
                id: p('input'),
                min: p('min'),
                max: p('max'),
                step: p('step') || 1,
                value: p('value'),
                unit: p('unit'),
                derived: p('derived') || '',
                labelledby: p('labelledby') || ''
            })
        );
        mount('savebar', ({ p }) => saveBar(p('name'), p('save')));
        mount('glyph', ({ p }) => glyph(iconOf(p('ico')), 'lg'));
        q('#unc-case-src-link').innerHTML = linkRow(navLink('safety', 'curves.case_src_link'));
    }

    const mounted: { module: FanModule; ctx: FanCtx; el: HTMLElement }[] = [];
    const hostOf = (name: FanPage): string => mounted.find(m => m.module.id === name)?.module.host ?? name;

    function mountFanModules(): void {
        for (const module of fanModules()) {
            const slot = find(`[data-fan-slot="${module.host}"]`);
            if (!slot) throw new Error(`No fan slot for ${module.host}`);
            const id = module.id;
            const el = document.createElement('div');
            el.className = 'unc-fanmod';
            el.id = `unc-fm-${id}`;
            state.moduleDirty[id] = false;
            state.saveError[id] = '';
            state.edits[id] = 0;
            state.touched[id] = new Set();
            state.saved[id] = false;
            const ctx: FanCtx = {
                el,
                api,
                ui: components,
                t,
                tRaw,
                tEnum,
                status: () => state.data,
                run,
                toast,
                saved,
                markDirty: what => {
                    markDirty(id, what);
                    state.saveError[id] = '';
                    renderSaveBars();
                },
                clearDirty: () => {
                    setDirty(id, false);
                    state.saveError[id] = '';
                    renderSaveBars();
                },
                isDirty: () => isDirty(id),
                save: (call, success) => saveFanPage(id, call, success),
                redraw: () => drawFanModules()
            };
            el.innerHTML = module.render(ctx);
            slot.appendChild(el);
            mounted.push({ module, ctx, el });
            el.addEventListener('input', event => {
                if (event.target instanceof HTMLInputElement && event.target.type === 'range') setRangeFill(event.target);
            });
            module.bind?.(ctx);
        }
    }

    function drawFanModules(): void {
        const data = state.data;
        if (!data?.ok) return;
        for (const { module, ctx, el } of mounted) {
            // Readings only: no inputs, tuning, schedule or profiles to offer.
            el.hidden = (fanReadOnly() && module.host === 'curves') || (module.visible ? !module.visible(data) : false);
            if (!el.hidden) module.update?.(data, ctx);
        }
    }

    const driveLabel = (name: string): string => (name === 'flash' ? t('sensors.boot_usb') : name);
    const driveSerial = (bay: Bay): string => {
        const id = friendly(bay.id);
        return id && id !== friendly(bay.model) ? (id.split(' ').pop() ?? '') : '';
    };

    let toastTimer: ReturnType<typeof setTimeout> | undefined;
    function toast(message: string, type = 'success'): void {
        const element = q('#unc-toast');

        element.textContent = message;
        element.className = `unc-toast show ${type}`;

        if (type === 'error') console.error('[UGREEN NAS]', message);

        clearTimeout(toastTimer);
        toastTimer = setTimeout(
            () => {
                element.className = 'unc-toast';
            },
            type === 'error' ? 6000 : 2500
        );
    }

    // The confirmation for a setting that saves on change. Settings that reload the page leave a flag for the next load.
    const saved = () => toast(t('save.saved'));

    function reloadSaved() {
        try {
            sessionStorage.setItem('unc-saved', '1');
        } catch (error) {
            /* then no confirmation */
        }
        location.reload();
    }

    async function run(action: () => Promise<unknown>, success?: string): Promise<void> {
        state.busy = true;
        try {
            await action();
            if (success) toast(success);
        } catch (error) {
            toast(errorMessage(error), 'error');
        }
        state.busy = false;
        await load();
    }

    function activeCurves(): Record<Kind, CurvePoint[]> {
        const settings: Partial<Settings> = state.data?.settings || {};
        const presets: Partial<Record<string, [string, string]>> = state.data?.presets || {};
        const preset = (settings.mode && presets[settings.mode]) || presets.standard || ['', ''];

        if (settings.mode === 'custom') {
            return {
                cpu: parseCurve(settings.cpu_curve || preset[0]),
                case: parseCurve(settings.case_curve || preset[1])
            };
        }
        if (settings.mode === 'bios' || settings.mode === 'full') {
            return { cpu: parseCurve(presets.standard?.[0]), case: parseCurve(presets.standard?.[1]) };
        }
        return { cpu: parseCurve(preset[0]), case: parseCurve(preset[1]) };
    }

    const caseFans = (fans: Fan[] | null | undefined): Fan[] => (fans || []).filter(f => f.role === 'case');
    const caseGroupName = () => t(caseFans(state.data?.fans).length === 1 ? 'fan.case_one' : 'curves.case');

    const SUBTITLES: Record<string, Key> = {
        overview: 'sub.overview',
        case: 'case.intro',
        sensors: 'sub.sensors',
        curves: 'curves.intro',
        safety: 'safety.intro',
        history: 'hist.sub',
        panel: 'sub.panel',
        'led:overview': 'leds.intro',
        'led:drives': 'sub.led_drives',
        'led:schedules': 'sub.led_schedules',
        'led:system': 'sub.led_system',
        settings: 'sub.settings',
        about: 'sub.about'
    };

    function renderTitle() {
        const nav = state.nav;

        q('#unc-title').textContent = tRaw(`nav.${nav.replace(':', '_')}`);
        q('#unc-subtitle').innerHTML = t(
            nav === 'curves' && fanProbing()
                ? isCs201xModel(modelName())
                    ? 'cs201x.title'
                    : 'probe.title'
                : nav === 'curves' && fanReadOnly()
                  ? 'readonly.sub'
                  : SUBTITLES[nav] || 'sub.overview'
        );
        qa('[data-model]').forEach(element => {
            element.textContent = state.data?.info?.model || 'UGREEN NAS';
        });
        renderBanners();
    }

    // The controller chips in the title row, the same everywhere: the fan controller on the fan pages, the LED controller
    // on the LED pages, both elsewhere. Only a problem shows here (red, with the reason); chip and bus of a controller
    // that works are facts of the System card (renderSystem).
    function renderBanners() {
        const data = state.data;
        const nav = state.nav;
        const fanPage = nav === 'curves' || nav === 'safety' || nav === 'history';
        const ledPage = nav.startsWith('led:');
        const show: [text: string, sub: string][] = [];

        if (data?.ok) {
            // A model without fan control shows no fan controller chip, found or not: the Settings page says so.
            if (!ledPage && (!modelName() || fanKnown())) {
                if (!data.chip && data.chip_error) show.push([t('warn.chip'), data.chip_error]);
            }
            // A single missed probe is not "not found": see ledControllerState().
            const controller = ledControllerState();
            if (!fanPage && controller) {
                if (controller !== 'found') show.push([t('banner.led_off'), '']);
            }
        }

        q('#unc-banners').innerHTML = show.map(([text, sub]) => pill({ tone: 'bad', ico: 'alert', disc: true, text, sub })).join('');
    }

    function renderNightBadge() {
        const badge = find('#unc-night');

        if (badge) badge.hidden = !(ledsOn() && state.ledStatus?.night);
    }

    function renderHead() {
        const data = state.data;
        const online = Boolean(data?.ok);
        // An update replaced the backend under this open tab: the old page may misread it, so ask for a reload.
        const stale = online && isStaleBundle(window.UGREEN_NAS_VERSION, data?.version);
        const badge = q('#unc-online');

        badge.textContent = stale ? t('head.reload') : online ? t('head.online') : t('head.offline');
        badge.classList.toggle('is-ok', online && !stale);
        badge.classList.toggle('is-warn', stale);
        badge.classList.toggle('is-bad', !online);
        renderNightBadge();

        if (!data?.ok) return;

        renderTitle();
        renderConflicts();
        q('#unc-conflict-list').textContent = `${t('system.conflicts')}: ${conflictFact()}`;

        const daemon = data.running ? data.daemon : null;
        q('#unc-failsafe').hidden = !daemon?.failsafe;
        q('#unc-failsafe-text').textContent = daemon?.failsafe ? tEnum(`failsafe.${daemon.failsafe}`, daemon.failsafe) : '';

        const stalled = (daemon?.stalled || []).map(id => {
            const fan = (data.fans || []).find(f => f.id === id);
            return fan ? fanName(fan, data.fans) : id;
        });
        q('#unc-stalled').hidden = !stalled.length;
        q('#unc-stalled-text').textContent = stalled.length ? t('stalled.text', { list: stalled.join(', ') }) : '';
    }

    // Rival fan or LED tools that write the same chips: one panel per running rival, naming what it controls.
    // "Use ours" only points to the Plugins page (other plugins are never removed from here); "Keep theirs" turns our part off.
    const conflictRelevant = (c: Conflict): boolean =>
        c.severity === 'block' &&
        ((c.controls.includes('fan') && !fanOff() && (fanKnown() || !modelName())) || (c.controls.includes('led') && Boolean(state.data?.led?.enabled)));

    function renderConflicts() {
        const box = q('#unc-conflicts');
        const rivals = (state.data?.conflicts || []).filter(conflictRelevant);
        const unapplied = state.data?.running ? state.data.daemon?.unapplied : '';

        box.hidden = !rivals.length;
        box.innerHTML = rivals
            .map(c => {
                const name = escapeHtml(c.name);
                const controls = c.controls.filter(what => what !== 'watchdog');
                const lines = controls.map(what => `<p>${escapeHtml(t(what === 'fan' ? 'conflict.fan' : 'conflict.led'))}</p>`).join('');
                const alert =
                    unapplied && c.controls.includes('fan')
                        ? `<p><strong>${escapeHtml(t('conflict.unapplied', { name: c.name, reason: tEnum(`failsafe.${unapplied}`, unapplied) }))}</strong></p>`
                        : '';
                const keep = controls
                    .filter(what => (what === 'fan' ? !fanOff() : Boolean(state.data?.led?.enabled)))
                    .map(what =>
                        btn({ label: t(what === 'fan' ? 'conflict.keep_fan' : 'conflict.keep_led', { name: c.name }), attrs: `data-conflict-off="${what}"` })
                    )
                    .join(' ');

                return note(
                    `
                    <p><strong>${escapeHtml(t('conflict.title', { name: c.name }))}</strong></p>
                    ${alert}${lines}
                    <p class="unc-muted">${escapeHtml(t('conflict.use_ours_hint', { name: c.name }))}</p>
                    <div class="unc-actions"><a class="unc-btn primary" href="/Plugins"><span>${escapeHtml(t('conflict.use_ours'))}</span></a> ${keep}</div>`,
                    { tone: 'bad', box: true, tag: 'div', ico: 'alert', attrs: `role="alert" data-conflict="${escapeHtml(c.id)}" title="${name}"` }
                );
            })
            .join('');
    }

    function renderFanSwitch() {
        const input = q<HTMLInputElement>('#unc-fan-enabled');

        input.checked = !fanOff();
        q('#unc-fan-state').textContent = t(fanOff() ? 'fanctl.off' : 'fanctl.on');
    }

    function renderFans() {
        const data = loaded();
        const fans = data.fans || [];
        const off = fanKnown() && fanOff();
        const heat = heatRow();

        q('.unc-ov-cool').classList.toggle('fan-off', off);
        q('.unc-ov-cool').hidden = (Boolean(modelName()) && !fanShown()) || (!heat && (off || !fans.length));
        q('.unc-ov').classList.toggle('no-cool', Boolean(q('.unc-ov-cool').hidden));
        q('#unc-heat').innerHTML = heat;

        if (off) {
            q('#unc-fans').innerHTML = emptyText(t('fanctl.off_note'));
            q('#unc-follow').textContent = '';
            return;
        }

        if (fanReadOnly()) {
            q('#unc-fans').innerHTML = fans.map(fan => fanTile(fan, fans, { sub: t('readonly.tile') })).join('');
            q('#unc-follow').innerHTML = linkRow(navLink('curves', 'readonly.more'), navLink('sensors', 'ov.all_sensors'));
            return;
        }
        q('#unc-fans').innerHTML = fans.map(fan => fanTile(fan, fans, { sub: fanSourceShort(fan), hint: fanSource(fan) })).join('');

        q('#unc-follow').innerHTML = followLinks();
    }

    function heatRow() {
        const data = loaded();
        const s: Partial<Sensors> = data.sensors || {};
        const chip = (data.chip_temps || []).find(v => v != null);
        const item = (label: string, value: number | null | undefined, kind: TempKind, iconName: IconName, hint = ''): string => {
            return value == null ? '' : statChip({ ico: icon(iconName), label, value: tempOf(value, kind), hint, cls: 'is-stack is-lg' });
        };

        return [
            item(t('temps.cpu'), s.cpu, 'cpu', 'chip'),
            item(t('temps.gpu'), s.gpu, 'cpu', 'pcie'),
            item(t('temps.board'), s.board, 'other', 'board', chip == null ? '' : `${t('cc.chip')} ${chip} °C`)
        ].join('');
    }

    const presetMode = (mode: Mode | undefined): boolean => mode === 'bios' || mode === 'full';

    function fanSource(fan: Fan): string {
        const data = loaded();
        const daemon = data.running ? data.daemon : null;
        const mode = data.settings?.mode;
        const kind = fan.role === 'cpu' ? 'cpu' : 'case';
        const info = daemon && !daemon.failsafe && !presetMode(mode) ? nowInfo(kind) : null;

        if (!data.running) return t('follow.stopped');
        if (daemon?.failsafe) return t('curves.why_emergency');
        if (mode === 'bios') return t('follow.bios');
        if (mode === 'full') return t('mode.note_full');
        if (kind === 'cpu' || info?.byCpu) return t('follow.cpu', { temp: deg(daemon?.cpu_temp ?? data.sensors?.cpu) });
        return caseFollowText();
    }

    function fanSourceShort(fan: Fan): string {
        const data = loaded();
        const daemon = data.running ? data.daemon : null;
        const mode = data.settings?.mode;
        const kind = fan.role === 'cpu' ? 'cpu' : 'case';
        const info = daemon && !daemon.failsafe && !presetMode(mode) ? nowInfo(kind) : null;

        if (!data.running || daemon?.failsafe) return fanSource(fan);
        if (mode === 'bios' || mode === 'full') return t(`mode.${mode}`);
        if (kind === 'cpu' || info?.byCpu) return t('ov.follows_cpu');

        const src = caseSource();

        return src.from === 'disk' ? t('ov.follows_disk') : src.from === 'nvme' ? t('ov.follows_nvme', { name: src.label }) : t('ov.follows_none');
    }

    function renderNow() {
        const data = loaded();
        const fans = data.fans || [];
        const box = q('#unc-now');

        if (fanOff() && !fanReadOnly()) {
            box.innerHTML = emptyText(t('fanctl.off_note'));
            return;
        }
        if (fanReadOnly()) {
            box.innerHTML = fans.map(fan => fanTile(fan, fans, { sub: t('readonly.tile') })).join('');
            return;
        }

        const daemon = data.running ? data.daemon : null;
        const mode = data.settings?.mode;

        box.innerHTML = fans
            .map(fan => {
                const kind = fan.role === 'cpu' ? 'cpu' : 'case';
                const info = daemon && !daemon.failsafe && !presetMode(mode) ? nowInfo(kind) : null;

                return fanTile(fan, fans, { sub: fanSource(fan), why: info?.why });
            })
            .join('');
    }

    function followLinks() {
        return linkRow(navLink('curves', 'follow.edit'), navLink('safety', 'follow.safety'), navLink('sensors', 'ov.all_sensors'));
    }

    function modeSub(id: Mode): string {
        const preset: [string, string] | undefined = state.data?.presets?.[id];
        const first = preset ? preset.map(text => parseCurve(text)[0]?.pct ?? 0) : [0, 0];

        return t(`mode.${id}_sub`, { cpu: first[0], case: first[1] });
    }

    function modeTiles(mode: Mode, disabled: string): string {
        return MODES.map(
            m => `
            <button type="button" class="unc-effect ${m.id === mode ? 'active' : ''}" data-mode="${m.id}" title="${escapeHtml(modeSub(m.id))}" ${disabled}>
                ${icon(m.icon)}
                <strong>${escapeHtml(t(`mode.${m.id}`))}</strong>
                <small>${escapeHtml(modeSub(m.id))}</small>
            </button>`
        ).join('');
    }

    function renderModes() {
        // Readings only: there is no daemon to take a mode.
        for (const id of ['#unc-modes', '#unc-modes-fans']) q(id).hidden = fanReadOnly();
        if (fanReadOnly()) {
            q('#unc-mode-hint').hidden = true;
            q('#unc-mode-note').textContent = '';
            return;
        }
        const mode = state.data?.settings?.mode || 'standard';
        const disabled = state.data?.chip ? '' : 'disabled';
        q('#unc-modes').innerHTML = modeTiles(mode, disabled);

        const ramp = mode === 'bios' || mode === 'full' ? '' : t('mode.hint_curve');
        const hint = q('#unc-mode-hint');
        hint.textContent = mode === 'bios' ? t('mode.hint_bios') : mode === 'full' ? t('mode.note_full') : ramp;
        hint.hidden = !hint.textContent;

        q('#unc-modes-fans').innerHTML = modeTiles(mode, disabled);
        q('#unc-mode-note').textContent =
            mode === 'bios'
                ? t('mode.hint_bios')
                : mode === 'full'
                  ? t('mode.note_full')
                  : [
                        mode === 'custom' || (['quiet', 'standard', 'performance'].includes(mode) && !state.data?.presets?.[mode]) ? '' : modeSub(mode) + '.',
                        ramp
                    ]
                        .filter(Boolean)
                        .join(' ');
    }

    const ssdSlotLabel = (n: Nvme): string =>
        n.slot === 'pcie'
            ? t('in.pcie_card')
            : n.slot === 'system'
              ? t('slot.system')
              : n.slot === 'm2'
                ? 'M.2'
                : t('case.m2_slot', { n: n.slot.replace('m2-', '') });

    function uptime(seconds: number): string {
        const d = Math.floor(seconds / 86400);
        const h = Math.floor((seconds % 86400) / 3600);
        const m = Math.floor((seconds % 3600) / 60);
        return d > 0 ? t('uptime.days', { d, h }) : t('uptime.hours', { h, m });
    }

    // 'found' | 'missing' | '' (unknown, or one miss that may be transient). The backend retries and
    // remembers the bus; "missing" needs an empty probe in three reads in a row and no LED answering.
    function ledControllerState(): 'found' | 'missing' | '' {
        const status = state.ledStatus;
        if (!ledsOn() || !status) return '';
        if (status.i2c_address || Object.keys(state.leds || {}).length) return 'found';
        return (state.ledMisses || 0) >= 3 ? 'missing' : '';
    }

    function ledController() {
        const led = state.data?.led;
        const status = state.ledStatus;

        if (ledsOn()) {
            const controller = ledControllerState();
            if (!controller) return '';
            return controller === 'found'
                ? status?.i2c_address
                    ? t('ledfacts.controller_on', { bus: status.i2c_bus, address: status.i2c_address })
                    : t('ledfacts.controller_on_short')
                : t('ledfacts.controller_off');
        }
        if (!led) return '';
        if (led.blocker === 'model') return t('system.ledctl_model');
        if (led.blocker) return t('system.ledctl_blocked');
        return t(led.warning === 'standalone' && !led.enabled ? 'system.ledctl_standalone' : 'system.ledctl_off');
    }

    // Every UGREEN NAS model by series and what this plugin does for it: [series, model, test regex of the DMI name, LEDs, case view, fans].
    // LEDs: verified | confirmed | reported (by users) | untested | power (power LED only) | none; case: full | fr | ''.
    type LedSupport = 'verified' | 'confirmed' | 'reported' | 'untested' | 'power' | 'none';
    const MODELS: [series: string, model: string, test: RegExp, leds: LedSupport, view: '' | 'fr' | 'full', fans: boolean][] = [
        ['DH', 'DH2600', /^DH2600/, 'none', '', false],
        ['DX', 'DX4600 Pro', /^DX4600/, 'confirmed', '', false],
        ['DX', 'DX4700+', /^DX4700/, 'reported', '', false],
        ['DXP', 'DXP2800', /^DXP2800$/, 'reported', 'fr', false],
        ['DXP', 'DXP2800S', /^DXP2800S$/, 'untested', '', false],
        ['DXP', 'DXP2800 GT', /^DXP2800 GT/, 'untested', 'fr', true],
        ['DXP', 'DXP480T Plus', /^DXP480T/, 'power', '', false],
        ['DXP', 'DXP4800', /^DXP4800$/, 'confirmed', 'fr', false],
        ['DXP', 'DXP4800S', /^DXP4800S$/, 'untested', '', false],
        ['DXP', 'DXP4800 Plus / Pro', /^DXP4800 (Plus|Pro)$/, 'confirmed', 'fr', false],
        ['DXP', 'DXP4800 GT', /^DXP4800.*GT/, 'verified', 'full', true],
        ['DXP', 'DXP6800 / Plus', /^DXP6800( Plus|\+)?$/, 'untested', 'full', true],
        ['DXP', 'DXP6800S', /^DXP6800S$/, 'untested', '', false],
        ['DXP', 'DXP6800 Pro', /^DXP6800 Pro$/, 'verified', 'full', true],
        ['DXP', 'DXP6800 Ultra', /^DXP6800 Ultra/, 'untested', '', false],
        ['DXP', 'DXP8800 / Pro', /^DXP8800( Pro)?$/, 'untested', 'fr', true],
        ['DXP', 'DXP8800 Plus', /^DXP8800 Plus$/, 'confirmed', 'fr', false],
        ['DXP', 'DXP8800 Ultra', /^DXP8800 Ultra/, 'untested', '', false],
        ['iDX', 'iDX6011 / iDX6012 / Pro', /^iDX601[12]/, 'untested', 'fr', false]
    ];

    function bindModelsFold() {
        const fold = q<HTMLDetailsElement>('#unc-models-fold');

        try {
            fold.open = localStorage.getItem('unc-models-open') === '1';
        } catch (error) {
            /* closed */
        }
        fold.addEventListener('toggle', () => {
            try {
                localStorage.setItem('unc-models-open', fold.open ? '1' : '0');
            } catch (error) {
                /* not remembered */
            }
        });
    }

    const CONFIRMS: [key: string, text: Key, when: () => boolean, line: string][] = [
        ['leds', 'models.confirm_leds', () => state.data?.led?.supported !== false, 'LEDs: colours and effects work'],
        ['bays', 'models.confirm_bays', () => state.data?.led?.supported !== false, 'LEDs: the right bay blinks with "Identify" (bay order is right)'],
        ['case', 'models.confirm_case', () => caseKnown(), 'Case view: front and rear look like my device'],
        [
            'inside',
            'models.confirm_inside',
            () => Boolean(state.hw) && !caseModel(state.hw as Hardware).noInside && caseKnown(),
            'Case view: the inside matches (M.2 slots, RAM)'
        ],
        ['fans', 'models.confirm_fans', () => fanKnown() && !fanOff(), 'Fan control: modes and curves work, the fans react'],
        ['temps', 'models.confirm_temps', () => true, 'Temperatures and the dashboard tile show the right values']
    ];

    function renderConfirm() {
        const box = q('#unc-confirm-list');
        const ticked = new Set(qa('#unc-confirm-list input:checked').map(input => input.dataset.confirm));

        box.innerHTML = CONFIRMS.filter(([, , when]) => when())
            .map(([key, text]) => chipCheck(`data-confirm="${key}"`, ticked.has(key), escapeHtml(t(text))))
            .join('');
    }

    function openConfirmIssue() {
        const info: Partial<Info> = state.data?.info || {};
        const model = modelName() || 'unknown';
        const marked = new Set(qa('#unc-confirm-list input:checked').map(input => input.dataset.confirm));
        const lines = CONFIRMS.filter(([, , when]) => when()).map(([key, , , line]) => `- [${marked.has(key) ? 'x' : ' '}] ${line}`);
        const comment = q<HTMLTextAreaElement>('#unc-confirm-text').value.trim();
        const body = [
            `Model: ${model}`,
            `Plugin: ${window.UGREEN_NAS_VERSION || '?'}`,
            `Unraid: ${info.unraid || '?'}, kernel ${info.kernel || '?'}`,
            `Fan chip: ${state.data?.chip || 'none'}; LED controller: ${state.ledStatus?.i2c_address ? `I2C ${state.ledStatus.i2c_address}` : 'not found'}`,
            '',
            ...lines,
            ...(comment ? ['', comment] : [])
        ].join('\n');
        const url = `https://github.com/ugreen-nas-control/ugreen-nas-control/issues/new?labels=${encodeURIComponent('model-report')}&title=${encodeURIComponent(`Model report: ${model}`)}&body=${encodeURIComponent(body)}`;

        window.open(url, '_blank', 'noopener');
    }

    function renderModels() {
        const mine = modelName();
        const cell = (kind: string, text: string): string => `<td class="m-${kind}">${escapeHtml(text)}</td>`;

        q('#unc-models').innerHTML = `
            <table class="unc-table unc-models">
                <thead><tr>
                    <th>${escapeHtml(t('models.col_model'))}</th><th>${escapeHtml(t('models.col_leds'))}</th>
                    <th>${escapeHtml(t('models.col_case'))}</th><th>${escapeHtml(t('models.col_fans'))}</th>
                </tr></thead>
                <tbody>${MODELS.map(
                    ([series, name, re, leds, view, fans], i) => `${
                        i === 0 || MODELS[i - 1]?.[0] !== series
                            ? `
                    <tr class="series"><th colspan="4">${escapeHtml(t('models.series', { s: series }))}</th></tr>`
                            : ''
                    }
                    <tr class="${re.test(mine) ? 'mine' : ''}">
                        <td><b>${escapeHtml(name)}</b>${re.test(mine) ? ` <small class="unc-muted">${escapeHtml(t('models.mine'))}</small>` : ''}</td>
                        ${cell(leds, t(`models.st_${leds}`))}
                        ${cell(view || 'none', view ? t(`models.case_${view}`) : '–')}
                        ${cell(fans ? 'yes' : 'none', fans ? '✓' : '–')}
                    </tr>`
                ).join('')}
                </tbody>
            </table>
            ${note(escapeHtml(t('models.footnote')))}`;
    }

    function conflictFact(): string {
        const list = state.data?.conflicts || [];

        return list.length
            ? list
                  .map(
                      c =>
                          `${c.name} (${c.controls.map(w => t(w === 'fan' ? 'conflict.c_fan' : w === 'watchdog' ? 'conflict.c_watchdog' : 'conflict.c_led')).join(', ')}; ${t(`conflict.k_${c.kind}` as Key)}${c.detail ? `, ${c.detail}` : ''})`
                  )
                  .join(' · ')
            : t('conflict.none');
    }

    function renderSystem() {
        const data = loaded();
        const info: Partial<Info> = data.info || {};
        const facts: [Key, string | undefined][] = [
            ['system.model', info.model],
            ['system.bios', [info.bios, biosDate(info.bios_date)].filter(Boolean).join(' · ')],
            ['system.cpu', [info.cpu, info.threads ? t('system.threads', { n: info.threads }) : ''].filter(Boolean).join(' · ')],
            ['system.memory', ramMB(info) ? `${Math.round(ramMB(info) / 1024)} GB` : ''],
            ['system.unraid', info.unraid],
            ['system.kernel', info.kernel],
            ['system.uptime', info.uptime ? uptime(info.uptime) : ''],
            ['system.chip', data.chip ? chipName(data.chip) : data.chip_error ? '–' : ''],
            ['system.ledctl', ledController()],
            ['system.conflicts', conflictFact()],
            ...ledFacts()
        ];

        q('#unc-system').innerHTML = facts
            .filter(([, value]) => value)
            .map(([key, value]) => `<dt>${escapeHtml(t(key))}</dt><dd>${escapeHtml(value)}</dd>`)
            .join('');
    }

    // What the case fans follow. The daemon compares every tick the hottest awake, non-ignored disk with the hottest
    // non-ignored NVMe minus the offset: the disk wins when it is at least as warm, otherwise the SSD; with all
    // disks asleep only the SSD is left. The daemon's values count; while its state is missing (stopped,
    // restarting) the same rule runs on the sensors.
    function caseSource() {
        const settings: Partial<Settings> = state.data?.settings || {};
        const sensors: Partial<Sensors> = state.data?.sensors || {};
        const daemon = state.data?.daemon;
        const ignored = new Set(settings.ignore_drives || []);
        const hottestDrive = <D extends { name: string; temp: number | null; standby?: boolean }>(list: D[] | null | undefined): D | null =>
            (list || [])
                .filter(d => !ignored.has(d.name) && !d.standby && d.temp != null)
                .reduce<D | null>((top, d) => (!top || (d.temp as number) > (top.temp as number) ? d : top), null);
        const diskDrive = hottestDrive(sensors.disks);
        const disk = diskDrive?.temp ?? null;
        const nvmeDrive = hottestDrive(sensors.nvme);
        const nvme = nvmeDrive?.temp ?? null;
        // "NVMe cache": the Unraid role of the drive (cache, a pool name); plain "NVMe" while the roles are unknown, never the device name (nvme0).
        const role = String((state.hw?.nvme || []).find(n => n.id === nvmeDrive?.name)?.role || '');
        const label = !role ? 'NVMe' : /^nvme/i.test(role) ? role : `NVMe ${role}`;
        const offset = settings.nvme_offset ?? 15;
        const eff = nvme == null ? null : nvme - offset;
        const calc = disk != null && (eff == null || disk >= eff) ? 'disk' : eff != null ? 'nvme' : '';

        if (daemon?.case_temp != null) {
            const from = daemon.case_from || calc;
            return {
                from,
                drive: (from === 'disk' ? diskDrive : nvmeDrive)?.name,
                label,
                temp: daemon.case_temp,
                disk,
                nvme: nvme ?? (from === 'nvme' ? daemon.case_temp + offset : null),
                offset,
                eff: eff ?? (from === 'nvme' ? daemon.case_temp : null)
            };
        }
        return { from: calc, drive: (calc === 'disk' ? diskDrive : nvmeDrive)?.name, label, temp: calc === 'disk' ? disk : eff, disk, nvme, offset, eff };
    }

    const caseTemp = () => caseSource().temp;

    function caseFollowText() {
        const src = caseSource();

        if (src.from === 'disk') return t('follow.disk', { temp: deg(src.temp) });
        if (src.from === 'nvme') return t(src.disk == null ? 'follow.nvme' : 'follow.nvme_awake', { name: src.label, nvme: deg(src.nvme), temp: deg(src.eff) });
        return t('follow.none');
    }

    // The daemon never runs a fan below this speed (Safety page), whatever the curve says.
    // An unsaved value from the field on the curves page counts for the preview.
    // The hardware floor of the model (fan_caps.min_pct_floor): the daemon raises a lower minimum to it.
    const minFloor = (): number => state.data?.fan_caps?.min_pct_floor ?? 10;
    const minPct = (): number => Math.min(60, Math.max(minFloor(), Number(state.minEdit ?? state.data?.settings?.min_pct ?? 25) || 25));

    function paintMix() {
        const line = q('#unc-mix-now');
        const cpu = cpuDemand();
        const temp = caseTemp();

        const foot = q('#unc-mix-cpu');

        foot.classList.toggle('on', cpu != null);
        q('#unc-mix-cpu-title').textContent = t(cpu != null ? 'curves.mix_cpu_on' : 'curves.mix_cpu_off');
        q('#unc-mix-cpu-hint').textContent = t(cpu != null ? 'curves.mix_cpu_on_hint' : 'curves.mix_cpu_off_hint');
        line.hidden = cpu == null;
        if (cpu == null) return;
        const curve = curveAt(state.curves.case, temp ?? -1);
        const pct = Math.max(curve, cpu, minPct());
        // Everything in case-fan terms: the CPU temperature is only the reason for the speed.
        line.textContent =
            cpu > curve && cpu >= minPct()
                ? t('curves.mix_now_cpu', { pct, rpm: rpmNote('case', pct), temp: loaded().sensors.cpu })
                : t('curves.mix_now', { pct, rpm: rpmNote('case', pct), cpu, cpurpm: rpmNote('case', cpu) });
    }

    function showCurveMin() {
        const input = q<HTMLInputElement>('#unc-curve-min');
        setRangeFill(input);
        q('#unc-curve-min-value').textContent = `${input.value} %`;
    }

    // The case fans with "help cool the CPU" on: what the CPU curve asks for at the current CPU temperature, else null.
    // The daemon runs the case fans at the higher of this and the case curve, then raises it to the minimum.
    function cpuDemand() {
        const cpuNow = state.data?.sensors?.cpu;
        return state.caseFollows && cpuNow != null ? curveAt(state.curves.cpu, cpuNow) : null;
    }

    function nowInfo(kind: Kind) {
        const now = kind === 'cpu' ? state.data?.sensors?.cpu : caseTemp();
        if (now == null) return null;

        const points = state.curves[kind];
        const curve = curveAt(points, now);
        const daemon = state.data?.daemon;
        const fan = fanOfKind(kind);
        const cpuNow = state.data?.sensors?.cpu;
        const cpuCurve = cpuNow == null ? 0 : curveAt(state.curves.cpu, cpuNow);
        const byCpu = kind === 'case' && (daemon?.case_driver && !state.curvesDirty ? daemon.case_driver === 'cpu' : state.caseFollows && cpuCurve > curve);
        const wanted = Math.max(byCpu ? cpuCurve : curve, minPct());
        // What the fan really gets: the daemon's target. Without one the daemon is not driving this fan:
        // a measurement runs or the BIOS is in charge.
        const driven = daemon?.failsafe ? 100 : fan ? daemon?.targets?.[fan.id] : undefined;
        const idle = driven == null && Boolean(daemon) && Boolean(fan);
        // The daemon ramps down slowly (after a spike or any drop of the curve): the fan is above the curve.
        const slowing = !idle && !state.curvesDirty && !daemon?.failsafe && driven != null && driven > wanted;
        let why = '';

        if (daemon?.failsafe) why = t('curves.why_emergency');
        else if (idle)
            why = state.data?.calibration?.progress?.state === 'running' ? t('curves.why_calibrating') : daemon?.mode === 'bios' ? t('curves.why_bios') : '';
        else if (slowing) why = t('curves.why_slowing');
        else if (byCpu) why = t('curves.why_cpu', { temp: cpuNow, cpu: cpuCurve, curve, rpm: rpmNote('case', cpuCurve) });
        else if (curve < minPct()) why = t('curves.why_min', { curve });

        return { now, byCpu, wanted, target: state.curvesDirty ? wanted : (driven ?? wanted), idle, slowing, why };
    }

    function drawChart(kind: Kind): void {
        const box = q(`[data-curve="${kind}"]`);
        const svg = box.querySelector<SVGSVGElement>('[data-chart]');
        if (!svg) return;
        const points = state.curves[kind];
        const { min, max } = CHART[kind];
        // The chart is as wide as its card, drawn 1:1 so the text keeps its size.
        const W = Math.round(svg.clientWidth) || 320;
        const H = 200;
        svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
        svg.dataset.w = String(W);
        const L = 34;
        const ticks = rpmTicks(kind);
        const R = ticks.length ? 44 : 10;
        const T = 24;
        const B = 26;
        const x = (temp: number): number => L + ((Math.max(min, Math.min(max, temp)) - min) / (max - min)) * (W - L - R);
        const y = (pct: number): number => T + (1 - pct / 100) * (H - T - B);

        let grid = '';
        for (let pct = 0; pct <= 100; pct += 25) {
            grid += `<line class="grid" x1="${L}" x2="${W - R}" y1="${y(pct)}" y2="${y(pct)}"/>`;
            grid += `<text class="axis" x="${L - 6}" y="${y(pct) + 4}" text-anchor="end">${pct}%</text>`;
        }
        if (!ticks.length && fanOfKind(kind) && !state.data?.calibration?.table) {
            grid += `<text class="axis hint" x="${L}" y="${T - 12}">${escapeHtml(t('curves.rpm_unmeasured'))}</text>`;
        }
        if (ticks.length) {
            grid += `<text class="axis" x="${W - R + 6}" y="${T - 14}">${escapeHtml(t('fan.rpm'))}</text>`;
            grid += ticks
                .map(
                    k => `<line class="tick" x1="${W - R}" x2="${W - R + 4}" y1="${y(k.pct)}" y2="${y(k.pct)}"/>
                <text class="axis" x="${W - R + 6}" y="${y(k.pct) + 4}">${k.rpm}</text>`
                )
                .join('');
        }
        const step = kind === 'cpu' ? 20 : 10;
        for (let temp = min; temp <= max; temp += step) {
            grid += `<text class="axis" x="${x(temp)}" y="${H - 8}" text-anchor="middle">${temp}°</text>`;
        }

        // The line runs flat to both edges, as the daemon holds the end values.
        const head = points[0];
        const tail = points[points.length - 1];
        const line = head && tail ? [`${x(min)},${y(head.pct)}`, ...points.map(p => `${x(p.temp)},${y(p.pct)}`), `${x(max)},${y(tail.pct)}`].join(' ') : '';

        const info = nowInfo(kind);
        let marker = '';
        if (info) {
            const { now, target, wanted, slowing, idle } = info;
            const text = `${t('curves.now')} ${now}°${idle ? '' : ` → ${target}%`}${info.byCpu && !idle && !slowing ? ` ${t('curves.by_cpu')}` : ''}`;
            const crit = state.data?.settings?.[kind === 'cpu' ? 'cpu_crit' : 'disk_crit'] ?? Number.NaN;
            const width = text.length * 6.3;
            const fits = (side: number): boolean => (side > 0 ? x(now) + 6 + width < W - R : x(now) - 6 - width > L);
            const hits = (side: number): boolean =>
                crit >= min &&
                crit <= max &&
                (side > 0 ? x(crit) > x(now) && x(crit) < x(now) + 12 + width : x(crit) < x(now) && x(crit) > x(now) - 12 - width);
            const right = fits(1) && !hits(1) ? true : fits(-1) && !hits(-1) ? false : fits(1);
            const tx = right ? x(now) + 6 : x(now) - 6;
            marker = `<line class="now" x1="${x(now)}" x2="${x(now)}" y1="${T}" y2="${H - B}"/>
                <circle class="now-dot" cx="${x(now)}" cy="${y(slowing ? target : wanted)}" r="4"/>
                <text class="now-text" x="${tx}" y="${T + 12}" text-anchor="${right ? 'start' : 'end'}">${escapeHtml(text)}</text>`;
        }

        // Below the minimum speed the curve is only a wish: the band is shaded, the wish is drawn faded and dashed,
        // its points hollow; the solid line runs along the limit line, as the fan does.
        const minimum = minPct();
        // The case chart with the CPU help on: the CPU curve's demand at today's CPU temperature is a second floor.
        const demand = kind === 'case' ? cpuDemand() : null;
        const floor = Math.max(minimum, demand ?? 0);
        const effectiveList = effectivePoints(points, min, max, floor);
        const effective = effectiveList.map(p => `${x(p.temp)},${y(p.pct)}`).join(' ');
        let cpuWins = '';
        let cpuLine = '';
        if (demand != null) {
            // Shaded: where the CPU decides, between the case curve and the line the fans really follow.
            if (demand > minimum && head && tail) {
                const plain = [{ temp: min, pct: head.pct }, ...points, { temp: max, pct: tail.pct }];
                cpuWins = `<polygon class="cpuwins" points="${[...effectiveList, ...plain.slice().reverse()].map(p => `${x(p.temp)},${y(p.pct)}`).join(' ')}"/>`;
            }
            const above = demand < 88;
            cpuLine = `<line class="cpuline" x1="${L}" x2="${W - R}" y1="${y(demand)}" y2="${y(demand)}"/>
                <text class="cpu-text" x="${W - R - 4}" y="${y(demand) + (above ? -5 : 13)}" text-anchor="end">${escapeHtml(t('curves.cpu_asks', { temp: loaded().sensors.cpu, pct: demand, rpm: rpmNote('case', demand) }))}</text>`;
        }
        const band = `<rect class="minband" x="${L}" y="${y(minimum)}" width="${W - L - R}" height="${H - B - y(minimum)}"/>`;
        const limit = `<line class="minline" x1="${L}" x2="${W - R}" y1="${y(minimum)}" y2="${y(minimum)}"/>`;
        const minLabel = `<text class="min-text" x="${L + 4}" y="${y(minimum) + 11}">${escapeHtml(t('curves.min', { pct: minimum }))}</text>`;

        // The emergency limit: from here on all fans run at 100 %. The case axis is the case temperature, so
        // an NVMe drive (minus the offset) trips it at the same line.
        const crit = state.data?.settings?.[kind === 'cpu' ? 'cpu_crit' : 'disk_crit'] ?? Number.NaN;
        let emergency = '';
        if (crit >= min && crit <= max) {
            const left = x(crit) > L + 100;
            emergency = `<line class="crit" x1="${x(crit)}" x2="${x(crit)}" y1="${T - 6}" y2="${H - B}"/>
                <text class="crit-text" x="${x(crit) + (left ? -4 : 4)}" y="${T - 10}" text-anchor="${left ? 'end' : 'start'}">${escapeHtml(t('curves.crit', { temp: crit }))}</text>`;
        }

        svg.innerHTML = `${grid}${band}${cpuWins}${emergency}${cpuLine}
            <polyline class="curve clipped" points="${line}"/>
            <polyline class="curve" points="${effective}"/>
            ${points.map(p => `<circle class="point${p.pct < floor ? ' clipped' : ''}" cx="${x(p.temp)}" cy="${y(p.pct)}" r="4"/>`).join('')}
            ${limit}
            ${minLabel}
            ${marker}`;

        // Not blocking: a curve that ends below 100 % leaves the emergency limit as the only backstop.
        const last = points[points.length - 1];
        q('[data-curve-warn]', box).hidden = !last || last.pct >= 100;
    }

    function fanOfKind(kind: Kind): Fan | undefined {
        return (state.data?.fans || []).find(f => f.role === kind);
    }

    function calibRows(kind: Kind): CalibRows | undefined {
        const id = fanOfKind(kind)?.id;
        return id === undefined ? undefined : state.data?.calibration?.table?.fans?.[id];
    }

    // Expected rpm of a fan at a speed: linear between the measured points; below the lowest one only the
    // measured 0 counts. null: not measured.
    const rpmAt = (kind: Kind, pct: number): number | null => rpmAtRows(calibRows(kind), pct);
    const rpmTicks = (kind: Kind) => rpmTicksOf(calibRows(kind));

    const rpmNote = (kind: Kind, pct: number): string => {
        const rpm = rpmAt(kind, pct);

        return rpm == null ? '' : ` ≈ ${rpm} ${t('fan.rpm')}`;
    };

    const rpmHtml = (kind: Kind, pct: number): string => {
        const floor = minPct();
        const rpm = rpmAt(kind, Math.max(pct, floor));
        const text = rpm == null ? '—' : String(rpm);
        return escapeHtml(text) + (pct < floor ? `<small>${escapeHtml(t('curves.raised', { pct: floor }))}</small>` : '');
    };

    const curvesEditable = () => state.data?.settings?.mode === 'custom' || state.arm;

    function buildPoints(kind: Kind): void {
        const box = q(`[data-curve="${kind}"] [data-points]`);
        const lock = curvesEditable() ? '' : 'disabled';

        box.innerHTML =
            `
            <div class="unc-point unc-point-head" aria-hidden="true"><span></span><span>°C</span><span>%</span><span>${escapeHtml(t('fan.rpm'))}</span></div>` +
            state.curves[kind]
                .map(
                    (p, i) => `
            <div class="unc-point">
                <span class="unc-muted">${i + 1}</span>
                <input type="number" min="0" max="120" step="1" value="${p.temp}" data-i="${i}" data-field="temp" aria-label="°C ${i + 1}" ${lock}>
                <input type="number" min="0" max="100" step="1" value="${p.pct}" data-i="${i}" data-field="pct" aria-label="% ${i + 1}" ${lock}>
                <span class="unc-rpm" data-rpm>${rpmHtml(kind, p.pct)}</span>
            </div>`
                )
                .join('');

        box.querySelectorAll('input').forEach(input => {
            const field = input.dataset.field as 'temp' | 'pct';
            const point = (): CurvePoint | undefined => state.curves[kind][Number(input.dataset.i)];
            // An empty or out-of-range field keeps the last good value (Number('') would be 0); leaving the field shows it again.
            input.addEventListener('change', () => {
                const current = point();
                if (current) input.value = String(current[field]);
            });
            input.addEventListener('input', () => {
                if (input.value.trim() === '' || !input.validity.valid) return;
                const value = Number(input.value);
                if (!Number.isFinite(value)) return;
                const current = point();
                if (current) current[field] = value;
                markDirty('curves', kind);
                state.saveError.curves = '';
                renderSaveBars();
                drawChart(kind);
                // The CPU curve feeds the case chart when the case fans help cool the CPU.
                if (kind === 'cpu') drawChart('case');
                paintMix();
                paintRpm(kind);
            });
        });
    }

    function paintRpm(kind: Kind): void {
        const box = q(`[data-curve="${kind}"]`);
        box.querySelectorAll('[data-rpm]').forEach((element, i) => {
            const point = state.curves[kind][i];
            if (point) element.innerHTML = rpmHtml(kind, point.pct);
        });
    }

    // Custom curves start with measuring the fans: a full explanation until it was done once, a summary line
    // afterwards, progress while it runs, the reason when it stopped.
    const CALIB_STEPS_PER_FAN = 12;
    const CALIB_SECONDS_PER_STEP = 11;
    const calibMinutes = (steps: number): number => Math.max(1, Math.round((steps * CALIB_SECONDS_PER_STEP) / 60));
    const calibTotalSteps = () => (state.data?.fans?.length || 0) * CALIB_STEPS_PER_FAN;

    function measuredFans(): { kind: Kind; label: string; rows: CalibRows }[] {
        const kinds: [Kind, string][] = [
            ['cpu', t('fan.cpu')],
            ['case', caseGroupName()]
        ];
        return kinds.flatMap(([kind, label]) => {
            const rows = calibRows(kind);
            return rows?.length ? [{ kind, label, rows }] : [];
        });
    }

    const calibOf = (): Partial<Calibration> => state.data?.calibration || {};

    const rpmRange = (rows: CalibRows): [number, number] => [rows[0]?.[1] ?? 0, rows[rows.length - 1]?.[1] ?? 0];

    const spinsFrom = (rows: CalibRows): number | null => rows.find(r => r[1] > 0)?.[0] ?? null;

    const calibRunning = () => state.data?.calibration?.progress?.state === 'running' || state.calibStarting;
    const calibFailed = (): boolean => {
        const calib = calibOf();
        const p = calib.progress;

        return !calibRunning() && Boolean(state.calibError || (p?.state === 'aborted' && p.time > (calib.table?.time || 0)));
    };

    // The stepper shows one step: the one clicked, else the first thing to do (measuring, then the curves).
    // A running measurement always shows itself; a failed one shows its error on the measure step but does not lock the stepper.
    function selectedStep(): string {
        if (calibRunning()) return 'measure';

        return state.flowStep || (measuredFans().length ? 'curves' : 'measure');
    }

    function minStall() {
        return measuredFans().filter(fan => minPct() < (spinsFrom(fan.rows) ?? 101));
    }

    async function startMeasure(confirm: boolean): Promise<void> {
        if (confirm && !window.confirm(t('calib.confirm', { min: calibMinutes(calibTotalSteps()) }))) return;
        state.calibStarting = true;
        state.calibError = '';
        renderMeasure();
        try {
            await api.post('calibrate', {});
        } catch (error) {
            state.calibError = errorMessage(error);
        }
        state.calibStarting = false;
        await load();
    }

    function measureBodyHtml(flow: boolean): string {
        const calib = calibOf();
        const p = calib.progress;
        const hasFans = Boolean(state.data?.fans?.length);
        const measureAttrs = (confirm: boolean): string => `data-measure${confirm ? ' data-confirm' : ''}${hasFans ? '' : ' disabled'}`;
        const button = (label: string, primary: boolean): string => btn({ label, ico: 'fan', tone: primary ? 'primary' : '', attrs: measureAttrs(false) });
        const measured = measuredFans();

        if (calibRunning()) {
            const steps = p?.steps || calibTotalSteps();
            const running = p?.state === 'running';
            const step = running ? p.step : 0;
            const fan = (state.data?.fans || []).find(x => x.id === p?.fan);
            const live = fan && fan.rpm > 0 ? ` <span class="unc-muted">· ${escapeHtml(t('calib.live', { rpm: fan.rpm }))}</span>` : '';
            const now = running
                ? escapeHtml(t('calib.running', { fan: fan ? fanName(fan, loaded().fans) : p?.fan, pct: p?.pct })) + live
                : escapeHtml(t('calib.starting'));

            return note(
                `
                <p><strong>${now}</strong></p>
                <div class="unc-measure-bar" role="progressbar" aria-valuemin="0" aria-valuemax="${steps}" aria-valuenow="${step}"><i style="width:${steps ? Math.round((step / steps) * 100) : 0}%"></i></div>
                <p class="unc-muted">${escapeHtml(t('calib.step', { step, steps, min: calibMinutes(steps - step) }))}</p>
                <p class="unc-muted">${escapeHtml(t('calib.running_note'))}</p>`,
                { tone: 'warn', box: true, tag: 'div', attrs: 'role="status"' }
            );
        }

        if (calibFailed()) {
            const text = state.calibError || t('calib.aborted', { reason: tEnum(`calib.reason.${p?.reason}`, p?.reason ?? '') });

            return note(
                `
                <p><strong>${escapeHtml(text)}</strong></p>
                <div>${button(t('calib.retry'), false)}</div>`,
                { tone: 'bad', box: true, tag: 'div', attrs: 'role="alert"' }
            );
        }

        if (!flow) {
            return `<p class="unc-measure-line">${
                measured.length
                    ? `<span class="unc-muted">${escapeHtml(measureSummary())}</span> ${linkBtn(t('calib.again'), measureAttrs(true))}`
                    : `<span class="unc-muted">${escapeHtml(t('calib.hint_preset'))}</span> ${linkBtn(t('calib.button'), measureAttrs(false))}`
            }</p>`;
        }

        if (measured.length) {
            return note(
                `
                <p><strong>${escapeHtml(t('calib.done', { time: berlin(calib.table?.time ?? 0) }))}</strong></p>
                <ul class="unc-min-fans">${measured.map(fan => `<li>${escapeHtml(t('calib.fan_range', { fan: fan.label, lo: rpmRange(fan.rows)[0], hi: rpmRange(fan.rows)[1], pct: spinsFrom(fan.rows) ?? 100 }))}</li>`).join('')}</ul>
                <p class="unc-muted">${escapeHtml(t('calib.gain_short'))}</p>
                <div>${linkBtn(t('calib.again'), measureAttrs(true))}</div>`,
                { box: true, tag: 'div' }
            );
        }

        return note(
            `
            <p><strong>${escapeHtml(t('calib.lead', { min: calibMinutes(calibTotalSteps()) }))}</strong></p>
            <ul>
                <li>${escapeHtml(t('calib.gain_rpm'))}</li>
                <li>${escapeHtml(t('calib.gain_floor'))}</li>
            </ul>
            <p class="unc-muted">${escapeHtml(t('calib.what'))}</p>
            <div>${button(t('calib.button'), true)}</div>`,
            { box: true, tag: 'div' }
        );
    }

    function measureSummary(): string {
        const ranges = measuredFans().map(fan => `${fan.label} ${rpmRange(fan.rows)[0]}–${rpmRange(fan.rows)[1]} ${t('fan.rpm')}`);

        return [t('calib.done', { time: berlin(calibOf().table?.time ?? 0) }), ...ranges].join(' · ');
    }

    function minFansHtml(): string {
        const measured = measuredFans();
        const min = minPct();

        if (!measured.length) return emptyText(t('curves.min_unmeasured'));

        return `<ul class="unc-min-fans">${measured
            .map(fan => {
                const from = spinsFrom(fan.rows);
                const stall = min < (from ?? 101);
                const text = stall
                    ? t('curves.min_fan_stall', { fan: fan.label, from: from ?? 100, pct: min })
                    : t('curves.min_fan', { fan: fan.label, from, rpm: rpmAt(fan.kind, min) ?? '—', pct: min });

                return `<li class="${stall ? 'is-warn' : ''}">${escapeHtml(text)}</li>`;
            })
            .join('')}</ul>`;
    }

    function renderMeasure() {
        const flow = curvesEditable();
        const measured = measuredFans();
        const running = calibRunning();
        const failed = calibFailed();
        const stalls = minStall().length > 0;
        const set = (id: string, html: string): void => {
            const el = q(id);

            // Rebuilding only on change keeps a focused button focused between polls.
            if (el.dataset.html !== html) {
                el.dataset.html = html;
                el.innerHTML = html;
            }
        };

        q('#unc-flow').classList.toggle('on', flow);

        const points = (name: Kind): number => state.curves?.[name]?.length || 0;
        const dirty = state.curvesDirty;
        const steps = {
            measure: {
                done: measured.length > 0 && !running && !failed,
                warn: failed || (!running && !measured.length),
                sum: running
                    ? t('calib.running_title')
                    : failed
                      ? t('calib.failed_title')
                      : measured.length
                        ? t('calib.done', { time: berlin(calibOf().table?.time ?? 0) })
                        : t('flow.unmeasured')
            },
            min: {
                done: measured.length > 0 && !stalls,
                warn: stalls,
                sum: [
                    `${minPct()} %`,
                    ...(measured.length ? [`${measured.map(fan => rpmAt(fan.kind, minPct()) ?? '—').join(' / ')} ${t('fan.rpm')}`] : [])
                ].join(' · ')
            },
            curves: {
                done: !dirty,
                warn: dirty,
                sum: [t('flow.curves_sum', { cpu: points('cpu'), case: points('case') }), ...(dirty ? [t('flow.unsaved')] : [])].join(' · ')
            }
        };
        const current = selectedStep();

        set('#unc-measure', measureBodyHtml(flow));
        set('#unc-min-measured', minFansHtml());

        Object.entries(steps).forEach(([step, info]) => {
            const selected = step === current;
            const tab = q(`#unc-t-${step}`);
            const sum = q(`#unc-sum-${step}`);

            tab.classList.toggle('done', info.done);
            tab.classList.toggle('warn', Boolean(info.warn));
            tab.setAttribute('aria-selected', String(selected));
            tab.tabIndex = selected ? 0 : -1;
            sum.textContent = info.sum;
            q(`#unc-p-${step}`).classList.toggle('sel', selected);
        });
        queueDock();
    }

    function selectStep(step: string): void {
        state.flowStep = step;
        renderMeasure();
    }

    function renderSaveBars() {
        const pages: [FanPage, boolean][] = [
            ['curves', state.curvesDirty],
            ['safety', state.safetyDirty],
            ...mounted.map((m): [FanPage, boolean] => [m.module.id, isDirty(m.module.id)])
        ];
        const navDirty = new Set<string>();
        pages.forEach(([name, dirty]) => {
            const bar = find(`[data-savebar="${name}"]`);
            if (!bar) return;
            const error = dirty ? state.saveError[name] : '';

            bar.classList.toggle('dirty', dirty);
            bar.classList.toggle('failed', Boolean(error));
            if (!dirty) touchedOf(name).clear();
            const what = [...touchedOf(name)].map(w => tRaw(`save.what.${w}`)).join(', ');
            q('.unc-savestate', bar).textContent = error
                ? t('save.failed', { error })
                : dirty
                  ? name === 'curves' && state.arm
                      ? t('save.arm')
                      : t('save.dirty', { what })
                  : t(state.saved[name] ? 'save.done' : 'save.clean');
            bar.querySelectorAll('button').forEach(button => {
                button.disabled = !dirty;
            });
            if (dirty) navDirty.add(hostOf(name));
        });
        ['curves', 'safety', 'settings'].forEach(nav => q(`.unc-nav [data-nav="${nav}"]`).classList.toggle('dirty', navDirty.has(nav)));
        if (state.data) renderMeasure();
        queueDock();
    }

    // Keeps the save bar of the visible page in view: fixed above Unraid's footer while its place in the card is
    // below the bottom of the window.
    let dockFrame = 0;
    function floatSaveBars() {
        dockFrame = 0;
        const footer = document.getElementById('footer')?.offsetHeight || 0;
        const limit = window.innerHeight - footer;

        qa('.unc-savedock').forEach(dock => {
            const bar = dock.firstElementChild;
            if (!dock.offsetParent || !(bar instanceof HTMLElement)) return;

            bar.classList.remove('float');
            bar.style.cssText = '';
            dock.style.minHeight = `${bar.offsetHeight}px`;
            const rect = dock.getBoundingClientRect();
            if (rect.bottom <= limit) return;

            bar.classList.add('float');
            // Unraid's scroll-to-top arrow sits at the right edge of the window: the bar stops short of it.
            const width = Math.min(rect.width, window.innerWidth - 56 - rect.left);
            bar.style.cssText = `left:${rect.left}px;width:${width}px;bottom:${footer}px`;
        });
    }

    const queueDock = () => {
        dockFrame = dockFrame || requestAnimationFrame(floatSaveBars);
    };

    // Every edit counts, so a save only clears the flag if nothing changed while it was running.
    const touchedOf = (name: FanPage): Set<string> => (state.touched[name] ??= new Set());
    const isDirty = (name: FanPage): boolean =>
        name === 'curves' ? state.curvesDirty : name === 'safety' ? state.safetyDirty : Boolean(state.moduleDirty[name]);
    function setDirty(name: FanPage, value: boolean): void {
        if (name === 'curves') state.curvesDirty = value;
        else if (name === 'safety') state.safetyDirty = value;
        else state.moduleDirty[name] = value;
    }
    const anyDirty = (): boolean => state.curvesDirty || state.safetyDirty || Object.values(state.moduleDirty).some(Boolean);

    function markDirty(name: FanPage, what?: string): void {
        setDirty(name, true);
        state.saved[name] = false;
        if (what) touchedOf(name).add(what);
        state.edits[name] = (state.edits[name] || 0) + 1;
    }

    async function saveFanPage(name: FanPage, call: () => Promise<unknown>, success: string): Promise<void> {
        const edits = state.edits[name];
        state.saveError[name] = '';

        await run(async () => {
            try {
                await call();
            } catch (error) {
                state.saveError[name] = errorMessage(error);
                throw error;
            }

            if (state.edits[name] === edits) {
                setDirty(name, false);
                state.saved[name] = true;
            }
        }, success);
        renderSaveBars();
    }

    function renderCurvesMode() {
        const mode = state.data?.settings?.mode || 'standard';
        const noCurves = mode === 'bios' || mode === 'full';
        const editable = curvesEditable();
        const name = t(`mode.${mode}`);

        q('#unc-curves-block').hidden = noCurves;
        q('#unc-limits-block').hidden = noCurves || editable;
        const limits = q('#unc-limits-body');
        const home = editable ? q('#unc-step-min-body') : q('#unc-limits-block');

        if (limits.parentNode !== home) home.appendChild(limits);
        q('#unc-curves-title').textContent = mode === 'custom' ? t('curves.heading') : t('curves.heading_mode', { mode: name });
        q('#unc-curves-note').textContent = state.arm
            ? t('curves.armed', { mode: name })
            : editable
              ? ''
              : t(state.caseFollows ? 'curves.readonly_mix' : 'curves.readonly', { mode: name });
        q('#unc-curves-note').hidden = !q('#unc-curves-note').textContent;
        q('#unc-customize').hidden = editable;
        q('#unc-curve-tools').hidden = !editable;
        qa<HTMLInputElement>('.unc-curves .unc-points input').forEach(input => {
            input.disabled = !editable;
        });
        qa('.unc-curves').forEach(el => el.classList.toggle('readonly', !editable));
    }

    function discardCurves() {
        state.curvesDirty = false;
        state.arm = false;
        state.saveError.curves = '';
        state.curves = activeCurves();
        state.caseFollows = state.data?.settings?.case_follows_cpu === true;
        state.minEdit = null;
        buildPoints('cpu');
        buildPoints('case');
        renderCurves();
        renderSaveBars();
    }

    function renderCurves() {
        // Rebuilding would take the focus out of a field the user just clicked into.
        if (!state.curvesDirty && !q('.unc-curves').contains(document.activeElement)) {
            state.curves = activeCurves();
            state.caseFollows = state.data?.settings?.case_follows_cpu === true;
            state.minEdit = null;
            buildPoints('cpu');
            buildPoints('case');
        }
        renderCurvesMode();
        q<HTMLInputElement>('#unc-case-cpu').checked = state.caseFollows;
        q<HTMLInputElement>('#unc-curve-min').min = String(minFloor());
        q<HTMLInputElement>('#unc-curve-min').value = String(minPct());
        const floorHint = q('#unc-min-floor');
        floorHint.hidden = minFloor() <= 10;
        floorHint.textContent = t('curves.min_floor', { pct: minFloor() });
        showCurveMin();
        drawChart('cpu');
        drawChart('case');
        paintMix();
        paintRpm('cpu');
        paintRpm('case');
        q('#unc-case-title').textContent = caseGroupName();
        q('#unc-case-src').textContent = t('curves.case_source', { offset: caseSource().offset });
        renderNow();
        renderMeasure();
    }

    interface SafetyField {
        id: string;
        key: 'cpu_crit' | 'disk_crit' | 'nvme_offset';
        unit: string;
    }
    const SAFETY: SafetyField[] = [
        { id: 'cpu-crit', key: 'cpu_crit', unit: '°C' },
        { id: 'disk-crit', key: 'disk_crit', unit: '°C' },
        { id: 'nvme-offset', key: 'nvme_offset', unit: '°C' }
    ];

    function showSafetyValue(field: SafetyField): void {
        const input = q<HTMLInputElement>(`#unc-${field.id}`);
        setRangeFill(input);
        q(`#unc-${field.id}-value`).textContent = `${input.value} ${field.unit}`;
    }

    function renderIgnoreList() {
        const sensors: Partial<Sensors> = state.data?.sensors || {};
        const ignored = new Set(state.data?.settings?.ignore_drives || []);
        const bays = state.hw?.bays || [];
        const slots = state.hw?.nvme || [];
        const drives = [
            ...(sensors.disks || [])
                .filter(d => d.name !== 'flash')
                .map(d => {
                    const bay = bays.find(b => b.device === d.device);

                    return {
                        name: d.name,
                        ico: icon('drives'),
                        label: bay ? `${t('sensors.bay', { n: bay.bay })} · ${d.name}` : d.name,
                        sub: [d.device, friendly(bay?.model)],
                        temp: d.standby ? standbyBadge() : tempOf(d.temp, d.hdd ? 'disk' : 'nvme')
                    };
                }),
            ...(sensors.nvme || []).map(n => {
                const slot = slots.find(h => h.id === n.name);

                return {
                    name: n.name,
                    ico: slot?.boot ? UNRAID_MARK : icon('m2'),
                    label: slot ? [ssdSlotLabel(slot), slot.role].filter(Boolean).join(' · ') : n.name,
                    sub: [n.name, friendly(slot?.model)],
                    temp: tempOf(n.temp, 'nvme')
                };
            })
        ].filter(d => d.name);

        q('#unc-ignore-list').innerHTML = drives.length
            ? drives
                  .map(d =>
                      chipCheck(
                          `data-ignore="${escapeHtml(d.name)}"`,
                          ignored.has(d.name),
                          `
            ${glyph(d.ico, 'lg')}
            <span class="unc-cc-name"><strong>${escapeHtml(d.label)}</strong><small class="unc-muted">${escapeHtml(d.sub.filter(Boolean).join(' · '))}</small></span>
            <span class="unc-cc-temp">${d.temp}</span>`
                      )
                  )
                  .join('')
            : emptyText(t('safety.ignore_none'));
    }

    // The ticked names, plus saved ones whose drive isn't listed right now (unplugged, asleep): they stay saved.
    function ignoredDrives(): string {
        const shown = new Set(qa('#unc-ignore-list [data-ignore]').map(box => box.dataset.ignore));
        const kept = (state.data?.settings?.ignore_drives || []).filter(name => !shown.has(name));

        return [...qa('#unc-ignore-list [data-ignore]:checked').map(box => box.dataset.ignore), ...kept].join(',');
    }

    // The allowance with today's numbers, using the slider's value so the example follows the edit: the hottest NVMe
    // minus the offset is what the case curve sees, as long as no disk is warmer (the daemon's rule).
    function renderNvmeEquation() {
        const { nvme, label, disk } = caseSource();
        const offset = Number(q<HTMLInputElement>('#unc-nvme-offset').value);
        const eff = nvme == null ? null : nvme - offset;
        const term = (name: string, value: string, cls = ''): string =>
            `<span class="unc-eq-term ${cls}"><small>${escapeHtml(name)}</small><b>${escapeHtml(value)}</b></span>`;
        const op = (sign: string): string => `<span class="unc-eq-op">${sign}</span>`;
        const drives = disk == null || (eff !== null && disk < eff);

        q('#unc-nvme-eq').innerHTML =
            nvme == null
                ? emptyText(t('safety.eq_none'))
                : `
            ${term(label, deg(nvme))}${op('−')}${term(t('safety.nvme_offset'), deg(offset))}${op('=')}${term(t('safety.eq_case'), deg(eff), 'is-result')}
            ${drives ? pill({ tone: 'accent', ico: 'fan', text: t('sensors.drives_case') }) : pill({ text: t('safety.eq_disk') })}`;
    }

    // Each limit with the live temperature: how far below it is (a bar of temperature over limit), amber when the
    // limit is at or below it and the fans would run at 100 % right now. The limits count as shown in the sliders,
    // so an edit is judged before it is saved. The NVMe limit is the disk limit plus the offset (the daemon's rule).
    function renderLimitsLive() {
        const { disk, nvme } = caseSource();
        const diskCrit = Number(q<HTMLInputElement>('#unc-disk-crit').value);
        const offset = Number(q<HTMLInputElement>('#unc-nvme-offset').value);
        const rows: [string, number | null | undefined, number, Key][] = [
            ['cpu', state.data?.sensors?.cpu, Number(q<HTMLInputElement>('#unc-cpu-crit').value), 'safety.now_none_cpu'],
            ['disk', disk, diskCrit, 'safety.now_disks_asleep'],
            ['nvme', nvme, diskCrit + offset, 'safety.now_no_nvme']
        ];

        q('#unc-nvme-crit-value').textContent = `${diskCrit + offset} °C`;
        q('#unc-nvme-derived').textContent = t('safety.derived', { disk: diskCrit, offset });
        rows.forEach(([id, now, limit, none]) => {
            const reached = now != null && now >= limit;
            const near = now != null && limit - now <= 10;
            const live = q(`#unc-live-${id}`);

            live.textContent = now == null ? t(none) : t(reached ? 'safety.reached' : 'safety.headroom', { temp: deg(now), gap: limit - now });
            // An unreadable CPU puts the fans at 100 % too.
            live.classList.toggle('is-warn', reached || (id === 'cpu' && now == null));
            q(`#unc-meter-${id}`).innerHTML = meter(now == null ? 0 : Math.min(100, Math.round((now / limit) * 100)), reached ? 'bad' : near ? 'warn' : '');
        });
    }

    function renderSafetyLive() {
        renderNvmeEquation();
        renderLimitsLive();
    }

    function renderSafety() {
        if (state.safetyDirty) return renderSafetyLive();
        const settings: Partial<Settings> = state.data?.settings || {};
        renderIgnoreList();

        SAFETY.forEach(field => {
            const value = settings[field.key];
            if (value != null) q<HTMLInputElement>(`#unc-${field.id}`).value = String(value);
            showSafetyValue(field);
        });
        q<HTMLInputElement>('#unc-stall-notify').checked = settings.stall_notify !== false;
        renderSafetyLive();
    }

    function discardSafety() {
        state.safetyDirty = false;
        state.saveError.safety = '';
        renderSafety();
        renderSaveBars();
    }

    function renderLeds() {
        const led: Partial<LedPart> = loaded().led || {};
        const input = q<HTMLInputElement>('#unc-led-enabled');
        const blocker = q('#unc-led-blocker');

        input.checked = Boolean(led.enabled);
        input.disabled = !led.enabled && Boolean(led.blocker);
        q('#unc-led-row').classList.toggle('unc-disabled', input.disabled);
        q('#unc-led-state').textContent = t(led.enabled ? 'leds.on' : 'leds.off');
        q('#unc-led-import').hidden = !led.import_available;
        // The LED pages come with the page itself: they are there once it has been loaded with the switch on.
        // Without them the LEDs group is the overview with the switch.
        qa('[data-led-only]').forEach(button => {
            button.hidden = !(led.enabled && window.UNC_LED);
        });

        q('#unc-led-limited').hidden = fanKnown();
        q('#unc-led-limited').textContent = fanKnown() ? '' : t(caseKnown() ? 'leds.limited_fans' : 'leds.limited', { model: led.model || '?' });
        q('#unc-led-untested').hidden = !led.untested;
        q('#unc-led-untested').textContent = led.untested ? t('leds.untested', { model: led.model || '?' }) : '';

        // A blocker keeps the part off; a warning (the plugin UGREEN LED Control is installed too) only tells.
        const note = led.blocker ? `leds.blocker_${led.blocker}` : led.warning ? `leds.warn_${led.warning}` : '';
        blocker.hidden = !note;
        blocker.textContent = note ? tRaw(note, { model: led.model || '?' }) : '';
    }

    function render(): void {
        keepScroll(drawAll);
    }

    function drawAll() {
        renderHead();
        if (!state.data?.ok) return;

        renderFans();
        renderModes();
        renderSystem();
        renderModels();
        renderConfirm();
        renderCurves();
        renderSafety();
        drawFanModules();
        renderSaveBars();
        renderLeds();
        renderHero();
        renderOverview();
        renderSensors();
        qa('[data-nav="case"]').forEach(el => {
            el.hidden = !caseKnown();
        });
        qa('[data-fan-only]').forEach(el => {
            // The probe and the read-only fans keep the group and the Fans page; history and safety need a daemon.
            const keep = (fanProbing() || fanReadOnly()) && el.matches('[data-nav="curves"], .unc-nav-group');
            el.hidden = (!fanKnown() || fanOff()) && !keep;
        });
        qa('[data-view="curves"] > :not(.unc-fanslot)').forEach(el => {
            el.hidden = fanProbing();
        });
        q('[data-view="curves"]').classList.toggle('is-readonly', fanReadOnly());
        q('#unc-fan-readonly').hidden = !fanReadOnly();
        qa('[data-fan-model]').forEach(el => {
            el.hidden = !fanKnown();
        });
        qa('[data-fan-readonly]').forEach(el => {
            el.hidden = !fanReadOnly();
        });
        qa('[data-nofan-model]').forEach(el => {
            el.hidden = fanShown() || !modelName();
        });
        qa('[data-panel-only]').forEach(el => {
            el.hidden = !panelKnown();
        });
        renderFanSwitch();
        if (state.hw && state.nav === 'case') renderRear(state.hw);
    }

    // Installed memory in MB: the sum of the modules, else what the kernel reports (a bit less).
    const ramMB = (info: Partial<Info> | null | undefined): number =>
        (state.hw?.memory || []).reduce((sum, m) => sum + (m.size_mb > 0 ? m.size_mb : 0), 0) || (info?.memory_kb ? info.memory_kb / 1024 : 0);

    const tempBadge = ({ text, level = '' }: { text: string; level?: string }): string => `<b class="unc-t${level ? ` ${level}` : ''}">${escapeHtml(text)}</b>`;

    const tempOf = (temp: number | null | undefined, kind: TempKind): string =>
        temp == null ? '' : tempBadge({ text: deg(temp), level: tempClass(temp, kind) });

    const standbyVal = (): SensorVal => ({ label: '', text: t('case.standby'), level: 'na' });
    const standbyBadge = () => tempBadge(standbyVal());

    // A label (with icon, sub text) and a value. cls: is-stack (label over value), is-row (label left, value right),
    // is-lg (big figure), is-end (right aligned). value is html; text and level make a tempBadge. after: html behind the value.
    const statChip = ({
        ico = '',
        label = '',
        sub = '',
        value = '',
        text = '',
        level = '',
        hint = '',
        cls = '',
        after = ''
    }: {
        ico?: string;
        label?: string;
        sub?: string;
        value?: string;
        text?: string;
        level?: string;
        hint?: string;
        cls?: string;
        after?: string;
    }): string => `
        <span class="unc-stat${cls ? ` ${cls}` : ''}"${hint ? ` title="${escapeHtml(hint)}"` : ''}>${label || ico ? `<small class="unc-muted">${ico ? glyph(ico) : ''}${escapeHtml(label)}${sub ? ` <em>${escapeHtml(sub)}</em>` : ''}</small>` : ''}${text ? tempBadge({ text, level }) : value}${after}</span>`;

    // Traffic level of a NIC (0 idle, 1 any, 2 from 1 MB/s, 3 from 50 MB/s), from the byte counters of the status poll.
    const netPrev = new Map<string, { rx: number; tx: number; t: number }>();
    const netAct = new Map<string, number>();
    const NET_ACT_STEPS = [50e6, 1e6, 1];

    function updateNetAct(list: NetIo[] | undefined): void {
        const now = Date.now();

        for (const n of Array.isArray(list) ? list : []) {
            const prev = netPrev.get(n.name);

            if (prev && now - prev.t < 1000) continue; // the same response again (shared poll): keep the level
            netPrev.set(n.name, { rx: n.rx, tx: n.tx, t: now });
            if (!prev || n.rx < prev.rx || n.tx < prev.tx) {
                netAct.set(n.name, 0); // first sample or counter reset
                continue;
            }
            const rate = (n.rx - prev.rx + n.tx - prev.tx) / ((now - prev.t) / 1000);
            const i = NET_ACT_STEPS.findIndex(min => rate >= min);

            netAct.set(n.name, i < 0 ? 0 : 3 - i);
        }
    }

    function paintNetAct(): void {
        qa<SVGSVGElement & HTMLElement>('.rj[data-nic]').forEach(el => {
            const level = netAct.get(el.dataset.nic ?? '') || 0;

            el.classList.remove('act-1', 'act-2', 'act-3');
            if (level) el.classList.add(`act-${level}`);
        });
    }

    const rj45 = (link: boolean, speed: number, nic = ''): string => `
        <svg viewBox="0 0 56 52" class="rj ${link ? 'up' : ''}${netAct.get(nic) ? ` act-${netAct.get(nic)}` : ''}"${nic ? ` data-nic="${escapeHtml(nic)}"` : ''}>
            <rect x="2" y="2" width="52" height="48" rx="3" class="shell"/>
            <rect x="9" y="14" width="38" height="28" rx="2" class="tongue"/>
            <path d="M14 14v8M20 14v8M26 14v8M32 14v8M38 14v8M44 14v8" class="pins"/>
            <rect x="6" y="5" width="10" height="5" rx="1" class="led link"/>
            <rect x="40" y="5" width="10" height="5" rx="1" class="led act ${speed >= 10000 ? 'fast' : ''}"/>
        </svg>`;

    // Fan control the page may offer comes from the status (fan_caps); the case pictures also for the DXP4800 family,
    // drawn after the DXP4800 Plus; elsewhere the pages stay out of the way.
    const modelName = () => state.hw?.model || state.data?.info?.model || '';
    const fanKnown = () => state.data?.fan_caps?.kind === 'fixed' || (state.data?.fan_caps?.kind === 'probe' && state.data.fan_caps.channels > 0);
    const fanProbing = () => state.data?.fan_caps?.kind === 'probe' && state.data.fan_caps.channels === 0 && !fanOff();
    const fanOff = () => state.data?.settings?.fan_control === false;
    // The plain iDX6011/6012: readings only (fan_caps.kind 'readonly'): no daemon, no writes, so no modes, curves, limits or history.
    const fanReadOnly = () => state.data?.fan_caps?.kind === 'readonly';
    // A model whose fan readings the page shows: driven (fixed, confirmed probe) or read only.
    const fanShown = () => fanKnown() || fanReadOnly();
    const panelKnown = () => hasButtons(modelName()) || hasBeeper(modelName());
    const caseKnown = () => fanKnown() || /^(DXP4800( Plus| Pro)?|DXP8800( Plus| Pro)?|DXP2800( GT)?|iDX601[12]( Pro)?)$/.test(modelName());

    function renderFront(hw: Hardware, target = '#unc-front'): void {
        q(target).innerHTML = frontHtml(hw);
        applyLeds(q(target));
    }

    const DRIVE_HDD_SVG =
        '<svg viewBox="0 0 72 48" aria-hidden="true"><rect x="2" y="2" width="68" height="44" rx="6" class="d-body"/><circle cx="26" cy="24" r="15" class="d-plat"/><circle cx="26" cy="24" r="4" class="d-hub"/><path d="M26 24 52 9" class="d-arm"/><rect x="50" y="30" width="16" height="11" rx="2" class="d-label"/></svg>';
    const DRIVE_SSD_SVG =
        '<svg viewBox="0 0 72 48" aria-hidden="true"><rect x="2" y="8" width="68" height="32" rx="5" class="d-ssd"/><rect x="9" y="15" width="14" height="18" rx="2" class="d-chip"/><rect x="27" y="15" width="14" height="18" rx="2" class="d-chip"/><rect x="45" y="15" width="18" height="18" rx="2" class="d-label"/></svg>';

    function driveRoleText(role: string | undefined): string {
        const disk = /^disk(\d+)$/.exec(role || '');

        if (disk) return t('drv.role_array', { n: disk[1] });
        if (role === 'parity' || role === 'parity2') return t('drv.role_parity');
        if (role === 'cache') return t('drv.role_cache');
        return role || '';
    }

    function driveCard(bay: Bay): string {
        const temp = bay.standby ? standbyBadge() : tempOf(bay.temp, bay.hdd ? 'disk' : 'nvme') || tempBadge({ text: '–', level: 'na' });

        return `
            <div class="unc-dcard">
                <div class="d-top"><b class="d-bay">${String(bay.bay).padStart(2, '0')}</b>${pill({ text: driveRoleText(bay.role) || '–' })}</div>
                <div class="d-main">
                    <div class="d-pic ${bay.hdd ? 'hdd' : 'ssd'}">${bay.hdd ? DRIVE_HDD_SVG : DRIVE_SSD_SVG}</div>
                    <div class="d-info">
                        <strong title="${escapeHtml(friendly(bay.model || bay.id))}">${escapeHtml(friendly(bay.model || bay.id) || '–')}</strong>
                        <span>${escapeHtml([fmtSize(bay.size_bytes), t(bay.hdd ? 'case.hdd' : 'case.ssd')].filter(Boolean).join(' · '))}</span>
                        <small class="unc-muted unc-id" title="${escapeHtml([bay.device, friendly(bay.id)].filter(Boolean).join(' · '))}">${escapeHtml(driveSerial(bay))}</small>
                    </div>
                </div>
                <div class="d-temp">${meter(!bay.standby && bay.temp != null ? Math.min(100, bay.temp) : 0)}${temp}</div>
            </div>`;
    }

    function renderDriveList(hw: Hardware): void {
        const bays = hw.bays || [];

        q('#unc-drive-list').innerHTML = bays.some(b => b.present)
            ? `
            <div class="unc-dcards">${bays
                .map(b =>
                    b.present
                        ? driveCard(b)
                        : `
                <div class="unc-dcard empty"><div class="d-top"><b class="d-bay">${String(b.bay).padStart(2, '0')}</b>${pill({ text: t('case.empty_bay') })}</div></div>`
                )
                .join('')}
            </div>`
            : emptyText(t('case.empty'));
    }

    const REAR_FAN_SVG =
        '<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="11" class="ring"/><circle cx="12" cy="12" r="2"/><path d="M12 10c0-4 1-7 4-7 2 0 3 2 2 4l-4 3.5M14 12c4 0 7 1 7 4 0 2-2 3-4 2l-3.5-4M12 14c0 4-1 7-4 7-2 0-3-2-2-4l4-3.5M10 12c-4 0-7-1-7-4 0-2 2-3 4-2l3.5 4"/></svg>';

    const fanStalled = (fan: Fan | undefined): boolean => Boolean(fan) && Boolean(state.data?.running && fan && state.data.daemon?.stalled?.includes(fan.id));

    function rearFanText(fan: Fan | undefined, n: number): string {
        if (!fan) return t('fan.case', { n });
        const rpm = fan.rpm > 0 ? `${fan.rpm} ${t('fan.rpm')}` : t('fan.stopped');
        return [fanName(fan, loaded().fans), rpm, fan.pct != null ? `${fan.pct} %` : ''].filter(Boolean).join(' · ');
    }

    function rearFansHtml(count: number): string {
        const fans = (state.data?.fans || []).filter(f => f.role === 'case').slice(0, count);

        return Array.from({ length: count }, (_, i) => {
            const fan = fans[i];
            const spin = fan ? fanIcon(fan, REAR_FAN_SVG) : `<span class="unc-fan-icon">${REAR_FAN_SVG}</span>`;

            return `<div class="r-fan ${fanStalled(fan) ? 'stalled' : ''}">${spin}<small>${escapeHtml(rearFanText(fan, i + 1))}</small></div>`;
        }).join('');
    }

    // After a photo of the DXP6800 Pro.
    // One socket, one screw and one LAN port of the rear drawings; x and y are per cent of the picture.
    const portHtml = (x: number, y: number, kind: SocketKind, label: string, cls = ''): string =>
        `<div class="r-abs r-${cls || kind}" style="left:${x}%;top:${y}%">${SOCKET[kind]}<b>${label}</b></div>`;
    const screwHtml = (x: number, y: number): string => `<i class="r-screw" style="left:${x}%;top:${y}%"></i>`;
    const lanHtml = (p: NetPort | undefined, x: number, y: number, label: string): string => {
        const up = Boolean(p && p.link);

        return `<div class="r-abs r-lan ${up ? 'up' : ''}" style="left:${x}%;top:${y}%">
                ${rj45(up, p ? p.speed_mbit : 0, p ? p.name : '')}<b>${label}</b><em>${escapeHtml(up && p ? speedText(p.speed_mbit) : t('case.no_link'))}</em></div>`;
    };

    function rear6800Html(hw: Hardware): string {
        const lan = (n: number): string => lanHtml(hw.net[n], n ? 39.9 : 32.7, 82, 'LAN 10G');
        const port = portHtml;
        const screw = screwHtml;
        const dots = (r: number): string =>
            `<circle r="${r}" fill="none" stroke="#8a939e" stroke-width="2.7" stroke-linecap="round" stroke-dasharray="0.1 ${((2 * Math.PI * r) / Math.round((2 * Math.PI * r) / 6.5)).toFixed(2)}"/>`;

        return `
            <div class="unc-rear6800">
                <div class="r-body"></div>
                ${screw(3.4, 4.2)}${screw(50.1, 4.2)}${screw(96.5, 4.2)}${screw(3.4, 89.7)}${screw(50.1, 89.7)}${screw(96.5, 89.7)}
                <div class="r-blende">
                    <div class="r-fans">${rearFansHtml(2)}</div>
                    <div class="r-mesh"></div>
                    <svg class="r-brand" viewBox="0 0 190 40" aria-label="UGREEN"><text x="95" y="31" text-anchor="middle" font-family="Arial Black, Helvetica, Arial, sans-serif" font-size="37" font-weight="900" fill="rgba(255,255,255,.1)" stroke="#e9edf1" stroke-width="1.2" stroke-dasharray="1 2.2">UGREEN</text></svg>
                </div>
                <div class="r-pcie"><b>PCIE</b><i></i></div>
                ${port(11.4, 82.2, 'hdmi', 'HDMI')}
                ${port(18.3, 82, 'usba', 'USB3.2')}
                <div class="r-abs r-usb2" style="left:25.4%;top:79.8%">${SOCKET.usb2}<b>USB2.0</b>${SOCKET.usb2}<b>USB2.0</b></div>
                ${lan(0)}${lan(1)}
                ${port(45.3, 80.9, 'reset', 'RESET')}
                <div class="r-psu"></div>
                <div class="r-abs r-inlet" style="left:69.7%;top:81.7%">
                    <svg viewBox="0 0 56 72" aria-hidden="true"><rect x="2" y="2" width="52" height="68" rx="5" fill="#1d2127" stroke="#9aa3ad" stroke-width="2.600"/><rect x="7" y="7" width="42" height="58" rx="3" fill="#0b0d10" stroke="#59626e" stroke-width="1.400"/><path d="M12 13h32v28a7 7 0 0 1-7 7H19a7 7 0 0 1-7-7z" fill="#171b20" stroke="#9aa3ad" stroke-width="1.600"/><rect x="23.500" y="17" width="9" height="14" rx="1.800" fill="#aeb6bf"/><rect x="13.500" y="30" width="9" height="14" rx="1.800" fill="#aeb6bf" transform="rotate(-22 18 37)"/><rect x="33.500" y="30" width="9" height="14" rx="1.800" fill="#aeb6bf" transform="rotate(22 38 37)"/></svg>
                </div>
                <div class="r-abs r-psufan" style="left:82%;top:81.5%">
                    <svg viewBox="-44 -44 88 88" aria-hidden="true"><title>${escapeHtml(t('case.psu_fan_hint'))}</title><circle r="42" fill="#101317" stroke="#8f99a4" stroke-width="2.600"/><g class="r-psu-blades" style="animation-delay:-${((performance.now() / 1000) % 1.1).toFixed(3)}s" fill="none" stroke="#69747f" stroke-width="1.700" stroke-linecap="round" stroke-linejoin="round"><g transform="translate(-37 -37) scale(3.083)"><circle cx="12" cy="12" r="2"/><path d="M12 10c0-4 1-7 4-7 2 0 3 2 2 4l-4 3.500M14 12c4 0 7 1 7 4 0 2-2 3-4 2l-3.500-4M12 14c0 4-1 7-4 7-2 0-3-2-2-4l4-3.500M10 12c-4 0-7-1-7-4 0-2 2-3 4-2l3.500 4"/></g></g><circle r="40" fill="none" stroke="#2a2f36" stroke-width="1.200"/>${dots(5)}${dots(13)}${dots(21)}${dots(29)}${dots(36)}</svg>
                </div>
                <i class="r-kens" style="left:96.4%;top:78.2%"></i>
                <i class="r-foot" style="left:5.4%"></i><i class="r-foot" style="left:87.2%"></i>
            </div>`;
    }

    const BRAND_DOTS_SVG =
        '<svg class="r-brand dots" viewBox="0 0 560 130" aria-label="UGREEN"><defs><pattern id="unc-dots" width="8" height="8" patternUnits="userSpaceOnUse"><circle cx="4" cy="4" r="2.7"/></pattern></defs><text x="280" y="115" text-anchor="middle" font-family="Helvetica Neue, Arial, sans-serif" font-size="140" font-weight="500" textLength="540" lengthAdjust="spacingAndGlyphs" fill="url(#unc-dots)">UGREEN</text></svg>';

    // After a photo of the DXP4800 Plus.
    function rear4800Html(hw: Hardware, lanLabels: string[] = ['LAN 10G', 'LAN 2.5G']): string {
        // LAN 1 is the 10G port, LAN 2 the 2.5G one; the kernel's eth0 / eth1 can be either way round, so the
        // port that supports more is LAN 1 (when ethtool didn't tell, they stay in the kernel's order).
        const ordered = hw.net.slice().sort((a, b) => (b.max_mbit || 0) - (a.max_mbit || 0));
        const lan = (n: number, x: number, y: number, label: string): string => lanHtml(ordered[n], x, y, label);
        const port = portHtml;
        const screw = screwHtml;

        return `
            <div class="unc-rear6800 unc-rear4800">
                <div class="r-body"></div>
                ${screw(5.1, 5.3)}${screw(94.8, 5.3)}${screw(5.1, 93.3)}${screw(94.8, 93.3)}
                <div class="r-blende">
                    <div class="r-fans">${rearFansHtml(1)}</div>
                    <div class="r-mesh"></div>
                    ${BRAND_DOTS_SVG}
                </div>
                ${port(16, 87.5, 'hdmi', 'HDMI')}
                ${port(29.1, 87.5, 'usba', 'USB3.2')}
                <div class="r-abs r-usb2" style="left:42.9%;top:83.9%">${SOCKET.usb2}<b>USB2.0</b>${SOCKET.usb2}<b>USB2.0</b></div>
                ${lan(1, 56.8, 84.6, lanLabels[1] ?? '')}${lan(0, 70.7, 84.6, lanLabels[0] ?? '')}
                ${port(79, 86.5, 'reset', 'RESET')}
                ${port(86.5, 87.1, 'dc', t('case.dc'))}
            </div>`;
    }

    const rearKit = (hw: Hardware, ordered: NetPort[] = hw.net) => ({
        lan: (n: number, x: number, y: number, label: string): string => lanHtml(ordered[n], x, y, label),
        port: portHtml,
        screw: screwHtml
    });

    const BRAND_SVG =
        '<svg class="r-brand" viewBox="0 0 190 40" aria-label="UGREEN"><text x="95" y="31" text-anchor="middle" font-family="Arial Black, Helvetica, Arial, sans-serif" font-size="37" font-weight="900" fill="rgba(255,255,255,.1)" stroke="#e9edf1" stroke-width="1.2" stroke-dasharray="1 2.2">UGREEN</text></svg>';

    // After a photo of the DXP2800.
    function rear2800Html(hw: Hardware, lan = 'LAN 2.5G', dc = t('case.dc'), gt = false): string {
        const k = rearKit(hw);

        return `
            <div class="unc-rear6800 unc-rear2800 ${gt ? 'unc-rear4800' : ''}">
                <div class="r-body"></div>
                ${k.screw(8, 4.6)}${k.screw(92, 4.6)}${k.screw(8, 94)}${k.screw(92, 94)}
                <div class="r-blende">
                    <div class="r-fans">${gt ? rearFansHtml(1) : `<div class="r-fan"><span class="unc-fan-icon">${REAR_FAN_SVG}</span></div>`}</div>
                    <div class="r-mesh"></div>
                    ${gt ? BRAND_DOTS_SVG : BRAND_SVG}
                </div>
                ${k.port(13, 87.5, 'hdmi', 'HDMI')}
                ${k.port(28.8, 87.5, 'usba', 'USB3.2')}
                <div class="r-abs r-usb2" style="left:42.4%;top:86%">${SOCKET.usb2}<b>USB2.0</b>${SOCKET.usb2}<b>USB2.0</b></div>
                ${k.lan(0, 61, 86.5, lan)}
                ${k.port(76, 87, 'reset', 'RESET')}
                ${k.port(88, 87.5, 'dc', dc)}
            </div>`;
    }

    // After a photo of the iDX6011 (Pro).
    function rear6011Html(hw: Hardware): string {
        const k = rearKit(hw);
        const pro = /Pro$/.test(hw.model || '');

        return `
            <div class="unc-rear6800 unc-rear6011">
                <div class="r-body"></div>
                ${k.screw(1.8, 5)}${k.screw(50, 5)}${k.screw(98.2, 5)}
                <div class="r-blende">
                    <div class="r-fans">${['', ''].map(() => `<div class="r-fan"><span class="unc-fan-icon">${REAR_FAN_SVG}</span></div>`).join('')}</div>
                    <div class="r-mesh"></div>
                    ${BRAND_SVG}
                </div>
                <div class="r-pcie"><b>PCIE</b><i></i></div>
                <div class="r-vent"></div>
                <div class="r-abs r-ac" style="left:94.5%;top:63.5%">${SOCKET.ac}<b>POWER</b></div>
                ${k.port(16.2, 88, 'usba', 'USB3.2')}${k.port(20.6, 88, 'usba', '', 'usba2')}${k.port(24.8, 88, 'usba', 'USB3.2', 'usba3')}
                ${k.lan(0, 29, 87, 'LAN 10G')}${k.lan(1, 36, 87, 'LAN 10G')}
                ${k.port(43.6, 88, 'hdmi', 'HDMI')}
                ${pro ? k.port(51.2, 88, 'usbc', 'OCuLink') : ''}
                ${k.port(57, 88, 'reset', 'RESET')}
            </div>`;
    }

    // After a photo of the DXP8800 Plus.
    function rear8800Html(hw: Hardware): string {
        const k = rearKit(hw);

        return `
            <div class="unc-rear6800 unc-rear8800">
                <div class="r-body"></div>
                ${k.screw(3.1, 5.3)}${k.screw(49.7, 5.3)}${k.screw(96.5, 5.3)}${k.screw(5.1, 89)}${k.screw(49.7, 89)}${k.screw(94.6, 89)}
                <div class="r-blende">
                    <div class="r-fans">${['', ''].map(() => `<div class="r-fan"><span class="unc-fan-icon">${REAR_FAN_SVG}</span></div>`).join('')}</div>
                    <div class="r-mesh"></div>
                    ${BRAND_SVG}
                </div>
                <div class="r-pcie"><b>PCIE</b><i></i></div>
                ${k.port(12.4, 80, 'hdmi', 'HDMI')}
                ${k.port(19.5, 80, 'usba', 'USB3.2')}
                <div class="r-abs r-usb2" style="left:26%;top:80%">${SOCKET.usb2}<b>USB2.0</b>${SOCKET.usb2}<b>USB2.0</b></div>
                ${k.lan(0, 33.1, 80, 'LAN 1')}${k.lan(1, 40.4, 80, 'LAN 2')}
                ${k.port(46.2, 80, 'reset', 'RESET')}
                <div class="r-abs r-ac" style="left:68.5%;top:80%">${SOCKET.ac}<b>POWER</b></div>
            </div>`;
    }

    function rearFanHtml(count = 1): string {
        const fans = caseFans(state.data?.fans);
        const one = (i: number): string => {
            const fan = fans[i];
            const svg = fan ? fanIcon(fan, REAR_FAN_SVG) : `<span class="unc-fan-icon">${REAR_FAN_SVG}</span>`;

            return `<div class="unc-rear-fan ${fanStalled(fan) ? 'stalled' : ''}">${svg}${fan ? `<small>${escapeHtml(rearFanText(fan, i + 1))}</small>` : ''}</div>`;
        };

        return `<div class="unc-rear-fans">${Array.from({ length: count }, (_, i) => one(i)).join('')}</div>`;
    }

    function renderRear(hw: Hardware, target = '#unc-rear'): void {
        const lan = (n: number): string => {
            const p = hw.net[n];
            const up = Boolean(p && p.link);
            const label = `${t('case.lan', { n: n + 1 })}${p ? ` · ${p.name}` : ''}`;
            return `
                <div class="unc-socket rj45 ${up ? 'up' : ''}">
                    ${rj45(up, p ? p.speed_mbit : 0, p ? p.name : '')}
                    <small>${escapeHtml(label)}</small>
                    <em>${escapeHtml(up && p ? speedText(p.speed_mbit) : t('case.no_link'))}</em>
                </div>`;
        };

        const model = caseModel(hw);

        if (model.rear6800) {
            q(target).innerHTML = rear6800Html(hw);
            return;
        }

        if (model.rear4800) {
            q(target).innerHTML = rear4800Html(hw, model.lanLabels);
            return;
        }

        if (model.rear2800) {
            q(target).innerHTML = rear2800Html(hw, model.lan, model.dc, model.gt);
            return;
        }

        if (model.rear6011) {
            q(target).innerHTML = rear6011Html(hw);
            return;
        }

        if (model.rear8800) {
            q(target).innerHTML = rear8800Html(hw);
            return;
        }

        q(target).innerHTML = `
            <div class="unc-nas-rear">
                ${model.fan ? rearFanHtml(Number(model.fan) || 1) : ''}
                <div class="unc-rear-ports">
                    ${model.rear.map(([kind, label]) => (kind === 'lan' ? lan(label) : socket(kind, t(label)))).join('')}
                </div>
            </div>`;
    }

    const UNRAID_MARK =
        '<svg viewBox="0 0 24 24" aria-hidden="true" class="unc-mark"><rect width="24" height="24" rx="6" fill="#f15a2c"/><path d="M7.5 6.5v6.2a4.5 4.5 0 0 0 9 0V6.5" fill="none" stroke="#fff" stroke-width="2.4" stroke-linecap="round"/></svg>';

    function ssdUseText(n: Nvme): string {
        if (n.boot) return t('nvme.boot');
        return ssdRoleText(n.role);
    }

    // After a photo of the DXP6800 Pro board; labels, sizes, temperatures and roles come from the system.
    const goldFingers = (x: number, y: number, w: number, h: number, n: number, notchAt: number | null): string => {
        const step = w / n;
        let out = '';

        for (let i = 0; i < n; i++) {
            if (notchAt != null && i >= notchAt && i < notchAt + 2) continue;
            out += `<rect x="${(x + i * step).toFixed(2)}" y="${y}" width="${(step * 0.62).toFixed(2)}" height="${h}" fill="#d9b44a"/>`;
        }
        return out;
    };

    const TEMP_COLOURS: Record<string, string> = { ok: '#22c55e', warm: '#f59e0b', hot: '#ef4444' };
    const tempColour = (temp: number | null): string => TEMP_COLOURS[tempClass(temp, 'nvme')] || '#8b96a3';

    function nvmeModule(L: number, H: number, ssd: Nvme, mirror = false): string {
        const name = String(ssd.model || ssd.id)
            .replace(/ SSD/, '')
            .replace(/\s+/g, ' ')
            .trim();
        const size = fmtSize(ssd.size_bytes);
        const fs = Math.min(H * 0.2, (L * (mirror ? 0.34 : 0.4)) / (Math.max(name.length, 1) * 0.58)).toFixed(1);
        const mark = ssd.boot
            ? `<rect x="${(L * 0.9 - 14).toFixed(1)}" y="${(H * 0.14).toFixed(1)}" width="14" height="14" rx="4" fill="#f15a2c"/><path d="M${(L * 0.9 - 10.5).toFixed(1)} ${(H * 0.14 + 3.5).toFixed(1)}v4.500a3.500 3.500 0 0 0 7 0V${(H * 0.14 + 3.5).toFixed(1)}" fill="none" stroke="#fff" stroke-width="1.800" stroke-linecap="round"/>`
            : '';

        // mirror: the connector sits on the right (the DXP4800 GT's slots); the drawing is flipped, the text is not.
        const tx = mirror ? L * 0.16 + 2 : L * 0.42;
        const flip = (inner: string): string => (mirror ? `<g transform="translate(${L} 0) scale(-1 1)">${inner}</g>` : inner);

        return `${flip(`<rect width="${L}" height="${H}" rx="3" fill="#17394f" stroke="#2c6a8f" stroke-width="1.200"/>
            ${goldFingers(1, H * 0.12, L * 0.085, H * 0.76, 9, 4)}
            <rect x="${L * 0.095}" y="${H * 0.12}" width="2" height="${H * 0.76}" fill="#0f2535"/>
            <rect x="${L * 0.14}" y="${H * 0.26}" width="${L * 0.12}" height="${H * 0.48}" rx="1.500" fill="#2a2f36" stroke="#4a5460" stroke-width=".8"/>
            <rect x="${L * 0.28}" y="${H * 0.12}" width="${L * 0.11}" height="${H * 0.76}" rx="1.500" fill="#20252b" stroke="#3a424c" stroke-width=".8"/>
            <rect x="${L * 0.4}" y="${H * 0.16}" width="${L * 0.44}" height="${H * 0.68}" rx="2" fill="#f1f3f5"/>
            <circle cx="${L - 5}" cy="${H / 2}" r="${H * 0.17}" fill="#0b0d0f" stroke="#c9ced4" stroke-width="1.600"/>${mark}`)}
            <text x="${tx}" y="${H * 0.42}" fill="#14181d" font-size="${fs}" font-weight="800">${escapeHtml(name)}</text>
            <text x="${tx}" y="${H * 0.68}" fill="#4a5460" font-size="${(H * 0.15).toFixed(1)}">${escapeHtml(size)}</text>`;
    }

    function ramModule(L: number, H: number, mod: Dimm): string {
        const speed = mod.configured_mt || mod.speed_mt;

        return `<rect width="${L}" height="${H}" rx="3" fill="#1f6a43" stroke="#2f9a63" stroke-width="1.200"/>
            ${goldFingers(1, H * 0.86, L - 2, H * 0.12, 64, 26)}
            <circle cx="0" cy="${H * 0.42}" r="${H * 0.1}" fill="#0b0d0f"/><circle cx="${L}" cy="${H * 0.42}" r="${H * 0.1}" fill="#0b0d0f"/>
            ${[0, 1, 2, 3].map(i => `<rect x="${L * 0.06 + i * L * 0.225}" y="${H * 0.12}" width="${L * 0.17}" height="${H * 0.46}" rx="1.500" fill="#14171a" stroke="#2b3036" stroke-width=".8"/>`).join('')}
            <rect x="${L * 0.2}" y="${H * 0.3}" width="${L * 0.6}" height="${H * 0.3}" rx="2" fill="#f1f3f5"/>
            <text x="${L * 0.22}" y="${H * 0.43}" fill="#14181d" font-size="${Math.max(8, H * 0.15).toFixed(1)}" font-weight="800">${escapeHtml(`${fmtRam(mod.size_mb)} ${mod.type}${speed ? ` · ${speed}` : ''}`)}</text>
            <text x="${L * 0.22}" y="${H * 0.55}" fill="#4a5460" font-size="${Math.max(6, H * 0.11).toFixed(1)}">${escapeHtml(mod.manufacturer || '')}</text>`;
    }

    const place = (x: number, y: number, L: number, upright: boolean, inner: string): string =>
        upright ? `<g transform="translate(${x} ${y + L}) rotate(-90)">${inner}</g>` : `<g transform="translate(${x} ${y})">${inner}</g>`;

    const tempBadge6800 = (x: number, y: number, temp: number | null): string =>
        temp == null
            ? ''
            : `<rect x="${x}" y="${y}" width="46" height="16" rx="8" fill="${tempColour(temp)}"/><text x="${x + 23}" y="${y + 11.5}" fill="#0b0e12" font-size="10.5" font-weight="800" text-anchor="middle">${temp} °C</text>`;
    const boardText = (x: number, y: number, text: string): string =>
        `<text x="${x}" y="${y}" style="fill:var(--bd-text)" font-size="11.5">${escapeHtml(text)}</text>`;

    function inside6800Html(hw: Hardware): string {
        const ssd = (slot: string): Nvme | undefined => hw.nvme.find(d => d.slot === slot);
        const ramSocket = (x: number, mod: Dimm | undefined, n: number): string => {
            const socket = `<rect x="${x}" y="150" width="104" height="236" rx="5" style="fill:var(--bd-sock);stroke:var(--bd-sockedge)" stroke-width="2"/><rect x="${x + 49}" y="360" width="6" height="18" rx="2" style="fill:var(--bd-sock2)"/><rect x="${x - 9}" y="178" width="9" height="34" rx="2" fill="#aeb6bf"/><rect x="${x + 104}" y="178" width="9" height="34" rx="2" fill="#aeb6bf"/>`;
            return (
                socket +
                (mod && mod.size_mb
                    ? place(x + 4, 154, 228, true, ramModule(228, 96, mod))
                    : `<g transform="rotate(-90 ${x + 52} 268)"><text x="${x + 52}" y="272" style="fill:var(--bd-text2)" font-size="12" text-anchor="middle">${escapeHtml(t('in.ram_empty', { n }))}</text></g>`)
            );
        };
        const m2 = (x: number, y: number, upright: boolean, drive: Nvme | undefined, label: string, bx: number, by: number): string => {
            if (!drive) {
                return `${place(x, y, 262, upright, '<rect width="262" height="74" rx="3" fill="none" style="stroke:var(--bd-sockedge)" stroke-width="2" stroke-dasharray="6 5"/>')}${boardText(bx, by + 11, t('in.slot_empty', { label }))}`;
            }
            const role = drive.boot ? t('nvme.boot') : ssdRoleText(drive.role);

            return `${place(x, y, 262, upright, nvmeModule(262, 74, drive))}${tempBadge6800(bx, by, drive.temp)}${boardText(bx + 54, by + 11, label)}${role ? boardText(bx + 54, by + 24, role) : ''}`;
        };

        const card = ssd('pcie');
        const memory = hw.memory;

        return `
            <svg class="unc-board-svg" viewBox="0 0 1000 640" role="img" aria-label="${escapeHtml(t('case.inside'))}">
                <defs>
                    <pattern id="unc-ribs" width="8" height="8" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="8" height="8" style="fill:var(--bd-ribA)"/><rect width="8" height="3.5" style="fill:var(--bd-ribB)"/></pattern>
                    <linearGradient id="unc-pcb" x1="0" y1="0" x2="1" y2="1"><stop offset="0" style="stop-color:var(--bd-g1)"/><stop offset="1" style="stop-color:var(--bd-g2)"/></linearGradient>
                </defs>
                <rect x="10" y="10" width="980" height="620" rx="16" fill="url(#unc-pcb)" style="stroke:var(--bd-edge)" stroke-width="3"/>
                <rect x="940" y="30" width="38" height="580" rx="8" fill="#a8afb8"/><rect x="948" y="60" width="22" height="34" rx="4" fill="#5d646d"/><rect x="948" y="520" width="22" height="34" rx="4" fill="#5d646d"/>
                <path d="M50 70h620l120 90v320H50z" fill="url(#unc-ribs)" style="stroke:var(--bd-plate)" stroke-width="2"/>
                <g transform="rotate(-90 300 290)"><text x="300" y="304" style="fill:var(--bd-ink)" font-size="64" font-weight="900" text-anchor="middle" letter-spacing="6">UGREEN</text></g>
                <circle cx="400" cy="140" r="9" fill="#b87333"/><circle cx="720" cy="430" r="9" fill="#b87333"/><circle cx="240" cy="470" r="9" fill="#b87333"/>
                ${m2(86, 96, true, ssd('m2-1'), t('in.m2', { n: 1 }), 172, 104)}
                ${m2(340, 22, false, ssd('m2-2'), t('in.m2', { n: 2 }), 612, 40)}
                ${boardText(452, 140, t('in.dimm', { type: memory.find(m => m.type)?.type || 'DDR5' }))}
                ${ramSocket(452, memory[1], 2)}
                ${ramSocket(578, memory[0], 1)}
                ${m2(560, 522, false, ssd('system'), t('in.system'), 560, 484)}
                <rect x="60" y="604" width="390" height="24" rx="6" style="fill:var(--bd-sock);stroke:var(--bd-sockedge)" stroke-width="2"/>
                <text x="255" y="620" style="fill:var(--bd-text2)" font-size="12" text-anchor="middle">${escapeHtml(t('case.pcie_slot'))}${card ? '' : ` · ${escapeHtml(t('case.free'))}`}</text>
                ${
                    card
                        ? `${tempBadge6800(76, 478, card.temp)}${boardText(130, 489, t('in.pcie_card'))}${boardText(130, 502, card.boot ? t('nvme.boot') : ssdRoleText(card.role))}
                    <rect x="76" y="512" width="330" height="86" rx="4" fill="#142a2a" stroke="#2f9a63" stroke-width="1.500"/>${goldFingers(100, 590, 260, 8, 38, null)}<rect x="62" y="522" width="14" height="66" rx="3" fill="#aeb6bf"/>${place(98, 520, 252, false, nvmeModule(252, 64, card))}`
                        : ''
                }
            </svg>`;
    }

    // DXP4800 GT: the service panel on the underside (front at the left, as in the photos of the box): two SO-DIMM
    // sockets side by side (RAM 1 nearer the front), two M.2 slots stacked (M.2-1 above M.2-2), connectors on the right.
    function inside4800gtHtml(hw: Hardware): string {
        const ssd = (slot: string): Nvme | undefined => hw.nvme.find(d => d.slot === slot);
        const memory = hw.memory;
        const ox = 270;
        const oy = 40;
        const ram = (x: number, mod: Dimm | undefined, n: number): string => {
            const filled = Boolean(mod?.size_mb);
            const socket = `<rect x="${x}" y="30" width="86" height="292" rx="5" style="fill:var(--bd-sock);stroke:var(--bd-sockedge)" stroke-width="2"/><rect x="${x + 40}" y="298" width="6" height="16" rx="2" style="fill:var(--bd-sock2)"/><rect x="${x - 8}" y="60" width="8" height="30" rx="2" fill="#aeb6bf"/><rect x="${x + 86}" y="60" width="8" height="30" rx="2" fill="#aeb6bf"/>`;
            const body = mod && filled ? place(x + 2, 34, 286, true, ramModule(286, 82, mod)) : '';
            const sub = mod && filled ? `${fmtRam(mod.size_mb)} ${mod.type || ''}`.trim() : t('case.empty');

            return `${socket}${body}${boardText(x, 342, t('case.ram_slot', { n }))}<text x="${x}" y="356" style="fill:var(--bd-text2)" font-size="11">${escapeHtml(sub)}</text>`;
        };
        const m2 = (y: number, drive: Nvme | undefined, n: number): string => {
            const label = t('in.m2', { n });
            const silk = `<text x="160" y="${y - 6}" style="fill:var(--bd-text2)" font-size="10">M.2 NVMe · PCIe Gen3 ×2</text>`;
            const connector = `<rect x="432" y="${y + 14}" width="8" height="38" rx="2" fill="#aeb6bf"/>`;
            if (!drive) {
                return `${silk}${connector}<rect x="160" y="${y}" width="270" height="66" rx="3" fill="none" style="stroke:var(--bd-sockedge)" stroke-width="2" stroke-dasharray="6 5"/>${boardText(24, y + 38, t('in.slot_empty', { label }))}`;
            }
            const role = drive.boot ? t('nvme.boot') : ssdRoleText(drive.role);
            const roleText = role.length > 21 ? `${role.slice(0, 20)}…` : role;

            return `${silk}${connector}<g transform="translate(160 ${y})">${nvmeModule(270, 66, drive, true)}</g>${tempBadge6800(24, y + 4, drive.temp)}${boardText(24, y + 38, label)}${roleText ? boardText(24, y + 52, roleText) : ''}`;
        };

        return `
            <svg class="unc-board-svg" viewBox="0 0 1000 640" role="img" aria-label="${escapeHtml(t('case.inside'))}">
                <defs>
                    <pattern id="unc-ribs" width="8" height="8" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="8" height="8" style="fill:var(--bd-ribA)"/><rect width="8" height="3.5" style="fill:var(--bd-ribB)"/></pattern>
                </defs>
                <rect x="10" y="10" width="980" height="620" rx="26" style="fill:var(--bd-g2);stroke:var(--bd-edge)" stroke-width="3"/>
                <rect x="60" y="64" width="64" height="20" rx="10" style="fill:var(--bd-sock)"/><rect x="60" y="556" width="64" height="20" rx="10" style="fill:var(--bd-sock)"/>
                <rect x="876" y="64" width="64" height="20" rx="10" style="fill:var(--bd-sock)"/><rect x="876" y="556" width="64" height="20" rx="10" style="fill:var(--bd-sock)"/>
                <text x="30" y="324" style="fill:var(--bd-text2)" font-size="15" font-weight="700" transform="rotate(-90 30 324)" text-anchor="middle">${escapeHtml(t('in.front'))}</text>
                <text x="972" y="324" style="fill:var(--bd-text2)" font-size="15" font-weight="700" transform="rotate(90 972 324)" text-anchor="middle">${escapeHtml(t('case.rear'))}</text>
                <rect x="${ox - 6}" y="${oy - 6}" width="452" height="572" rx="8" style="fill:var(--bd-plate);stroke:var(--bd-edge)" stroke-width="2"/>
                <rect x="${ox}" y="${oy}" width="440" height="560" rx="4" fill="url(#unc-ribs)"/>
                <g transform="translate(${ox} ${oy})">
                    <circle cx="418" cy="22" r="8" fill="#b87333"/><circle cx="418" cy="330" r="8" fill="#b87333"/><circle cx="22" cy="14" r="7" style="fill:var(--bd-sock2)"/>
                    ${ram(40, memory[0], 1)}
                    ${ram(150, memory[1], 2)}
                    ${m2(384, ssd('m2-1'), 1)}
                    ${m2(476, ssd('m2-2'), 2)}
                </g>
                <rect x="760" y="190" width="170" height="260" rx="8" style="fill:var(--bd-sock);stroke:var(--bd-sockedge)" stroke-width="2"/>
                <text x="845" y="232" style="fill:var(--bd-ink)" font-size="22" font-weight="900" text-anchor="middle" letter-spacing="2">UGREEN</text>
                <g style="fill:var(--bd-sockedge)"><rect x="780" y="260" width="130" height="6" rx="3"/><rect x="780" y="280" width="110" height="6" rx="3"/><rect x="780" y="300" width="130" height="6" rx="3"/><rect x="800" y="340" width="90" height="90" rx="6"/></g>
            </svg>`;
    }

    function ssdSlotRow(name: string, ssd: Nvme | undefined): string {
        if (!ssd) return sensorRow({ ico: icon('m2'), name, vals: [{ label: '', text: t('case.empty'), level: 'na' }] });

        return sensorRow({
            ico: ssd.boot ? UNRAID_MARK : icon('m2'),
            name,
            src: [friendly(ssd.model || ssd.id), fmtSize(ssd.size_bytes)].filter(Boolean).join(' · '),
            title: ssd.id,
            badges: ssdUseText(ssd) ? [pill({ text: ssdUseText(ssd) })] : [],
            vals: [tempVal(ssd.temp, 'nvme')]
        });
    }

    function insideListHtml(hw: Hardware): string {
        const model = caseModel(hw);
        const ssd = (slot: string): Nvme | undefined => hw.nvme.find(d => d.slot === slot);
        const card = hw.nvme.filter(d => d.slot === 'pcie');
        const ssds = [
            ssdSlotRow(t('case.m2_slot', { n: 1 }), ssd('m2-1')),
            ssdSlotRow(t('case.m2_slot', { n: 2 }), ssd('m2-2')),
            model.sys ? ssdSlotRow(t('case.sys_slot'), ssd('system')) : '',
            ...(model.pcie
                ? card.length
                    ? card.map(c => ssdSlotRow(t('case.pcie_slot'), c))
                    : [sensorRow({ ico: icon('pcie'), name: t('case.pcie_slot'), vals: [{ label: '', text: t('case.free'), level: 'na' }] })]
                : [])
        ].filter(Boolean);

        return `
            ${sectionHead(t('sensors.group_ssd'))}
            <div class="unc-sn-list">${ssds.join('')}</div>
            ${sectionHead(t('sensors.group_mem'))}
            <div class="unc-sn-list">${memoryRows(hw.memory).join('')}</div>`;
    }

    function setHint(element: HTMLElement, key: Key | undefined): void {
        element.hidden = !key;
        if (key) element.dataset.i18n = key;
        else element.removeAttribute('data-i18n');
        element.textContent = key ? t(key) : '';
    }

    function renderInside(hw: Hardware, target = '#unc-inside'): void {
        const article = q(target).closest('article');
        if (article) article.hidden = Boolean(caseModel(hw).noInside);
        if (caseModel(hw).noInside) return;

        if (caseModel(hw).inside4800gt) {
            q(target).classList.add('board-svg');
            setHint(q('#unc-inside-hint'), caseModel(hw).hint);
            q(target).innerHTML = inside4800gtHtml(hw);
            return;
        }

        if (caseModel(hw).inside6800) {
            const hint = q('#unc-inside-hint');

            q(target).classList.add('board-svg');
            setHint(hint, caseModel(hw).hint);
            q(target).innerHTML = inside6800Html(hw);
            return;
        }

        q(target).classList.remove('board-svg');
        setHint(q('#unc-inside-hint'), caseModel(hw).hint);
        q(target).innerHTML = insideListHtml(hw);
    }

    function renderCase() {
        if (!state.hw) {
            const text = t(state.hwError ? 'case.error' : 'case.loading');
            ['#unc-drive-list', '#unc-rear', '#unc-inside'].forEach(id => {
                q(id).innerHTML = emptyText(text);
            });
            return;
        }
        renderDriveList(state.hw);
        renderRear(state.hw);
        renderInside(state.hw);
    }

    const LED_POLL_MS = 15000;
    const STATUS_POLL_MS = 60000;

    const ledGet = <A extends keyof LedGet & string>(action: A, maxAge = 0): Promise<LedGet[A]> => ledApi.get(action, {}, maxAge);

    const ledsOn = (): boolean => Boolean(window.UNC_LED && state.data?.led?.enabled);

    function ledFacts(): [Key, string][] {
        const status = state.ledStatus;
        if (!ledsOn() || !status) return [];

        const found = Object.keys(state.leds || {});
        return [
            ['ledfacts.mapping', status.mapping ? tEnum(`ledfacts.mapping_${status.mapping}`, status.mapping) : ''],
            ['ledfacts.leds', found.length ? found.join(', ') : t('ledfacts.none')],
            ['ledfacts.monitor', status.foreign_monitor ? t('ledfacts.monitor_on') : t('ledfacts.monitor_off')],
            ['ledfacts.saved', status.settings_saved ? t('ledfacts.saved_yes') : t('ledfacts.saved_no')]
        ];
    }

    async function loadLeds(maxAge = LED_POLL_MS * 0.8): Promise<void> {
        if (!ledsOn()) {
            state.leds = null;
            if (state.ledStatus) {
                state.ledStatus = null;
                renderSystem();
                renderBanners();
                renderNightBadge();
            }
            return;
        }

        try {
            const [all, status] = await Promise.all([
                ledGet('all', maxAge).then(data => ({ ...data, leds: window.UNCLedFx.stamp(data.leds) })),
                ledGet('status', STATUS_POLL_MS * 0.9)
            ]);
            state.leds = all.leds || {};
            state.ledMode = { mode: status.mode, fx: status.fx };
            state.ledBays = Number(status.bays) || 0;
            state.ledStatus = status;
            state.ledMisses = status.i2c_address ? 0 : (state.ledMisses || 0) + 1;
            if (state.data) renderSystem();
            renderBanners();
            renderNightBadge();
        } catch (error) {
            state.leds = null;
            return;
        }

        // Only the night hint depends on it: a failed read must not hide the LED data.
        state.ledSchedule = await ledGet('schedule', STATUS_POLL_MS * 0.9).catch(() => state.ledSchedule);
    }

    function paintLed(element: HTMLElement | null, led: LedState | undefined, name: string | undefined): void {
        window.UNCLedFx.paint(element, led, name, state.ledStatus);
    }

    function applyLeds(container: HTMLElement | null): void {
        const leds = state.leds;
        if (!container || !leds) return;

        container.querySelectorAll('.unc-nbay').forEach((bay, i) => paintLed(bay.querySelector('.bar'), leds[`disk${i + 1}`], `disk${i + 1}`));
        paintLed(container.querySelector('.unc-pled.power'), leds.power, 'power');
        paintLed(container.querySelector('.unc-pled.lan'), leds.netdev, 'netdev');
        container.querySelectorAll<HTMLElement>('.unc-led .unc-pled[data-led]').forEach(el => paintLed(el, leds[el.dataset.led ?? ''], el.dataset.led));
    }

    function ledModeText(): string {
        const m = state.ledMode;
        if (!m) return '';
        if (m.fx) return tRaw(`led.fx_${m.fx}`) !== `led.fx_${m.fx}` ? tRaw(`led.fx_${m.fx}`) : m.fx;
        return t(m.mode === 'status' ? 'led.mode_status' : 'led.mode_manual');
    }

    async function loadHardware() {
        try {
            // The tool prints a list it found nothing for (no NVMe, no dmidecode, no NIC) as null.
            const hw = await api.get('hardware');
            state.hw = hw && { ...hw, memory: hw.memory || [], nvme: hw.nvme || [], net: hw.net || [] };
            state.hwError = false;
        } catch (error) {
            // A failed read keeps the last picture: dropping it would shrink the page under the reader and jump to the top.
            if (!state.hw) state.hwError = true;
        }
        keepScroll(() => {
            renderCase();
            if (state.data?.ok) {
                renderFans();
                renderHero();
                renderOverview();
                renderSensors();
            }
        });
    }

    // Redrawing big parts of the page can make it shorter for a moment; the browser then scrolls up and stays there.
    function keepScroll(draw: () => void): void {
        const y = window.scrollY;

        draw();
        if (window.scrollY !== y) window.scrollTo(0, y);
    }

    function renderHero() {
        const hw = state.hw;
        const info: Partial<Info> = state.data?.info || {};
        const stage = q('#unc-hero');

        if (!hw) {
            stage.className = 'unc-hero';
            stage.innerHTML = emptyText(t(state.hwError ? 'case.error' : 'case.loading'));
            return;
        }

        if (!caseKnown()) {
            stage.className = 'unc-hero';
            stage.innerHTML = emptyText(t('case.unknown', { model: hw.model || info.model || '?' }), 'center');
            return;
        }

        stage.className = 'unc-hero front';
        renderFront(hw, '#unc-hero');
    }

    function renderOverview() {
        renderCaseFoot();
        renderBoard();
    }

    function renderCaseFoot() {
        const data: Partial<StatusResponse> = state.data || {};
        const extras = data.sensors?.nvme_extra || [];
        const [warm] = LIMITS.nvme;
        const slots: { id: string; label: string; model: string; temp: number | null; boot?: boolean }[] = state.hw?.nvme?.length
            ? state.hw.nvme.map(n => ({ id: n.id, label: ssdSlotLabel(n), model: n.model || n.id, temp: n.temp, boot: n.boot }))
            : (data.sensors?.nvme || []).map(n => ({ id: n.name, label: n.name, model: n.label || n.name, temp: n.temp }));
        const ssds = slots
            .filter(n => n.temp != null)
            .map(n => {
                const hot = extras.filter(e => e.name === n.id).filter(hasTemp);
                const warn = hot.find(e => e.temp >= warm);
                const hint = [n.model, ...hot.map(e => `${e.label || t('temps.sensor')} ${e.temp} °C`), warn ? t('temps.hint_nvme_extra') : '']
                    .filter(Boolean)
                    .join('\n');

                return statChip({
                    ico: n.boot ? UNRAID_MARK : icon('m2'),
                    label: n.label,
                    value: tempOf(n.temp, 'nvme'),
                    after: warn ? glyph(icon('alert'), 'warn') : '',
                    hint
                });
            });
        let led = '';

        if (ledsOn() && state.ledMode) {
            const sched = state.ledSchedule;
            const brightness = state.leds ? `${Math.round((Number(state.leds.power?.brightness) || 0) / 2.55)} %` : '';
            const night = sched ? `${t('head.night')} ${sched.enabled ? `${sched.start}–${sched.end}` : t('leds.off').toLowerCase()}` : '';

            led = pill({
                tag: 'button',
                ico: 'bulb',
                text: [`${t('led.title')} ${ledModeText()}`, brightness, night].filter(Boolean).join(' · '),
                attrs: 'data-nav="led:overview"'
            });
        }

        q('#unc-ov-foot').hidden = !ssds.length && !led && !caseKnown();
        q('#unc-ov-foot').innerHTML = `
            ${ssds.length ? `<div class="unc-m2">${ssds.join('')}</div>` : ''}
            ${linkRow(caseKnown() && state.hw ? navLink('case', 'ov.drive_details') : '<span></span>', led)}`;
    }

    const lanPorts = () => (state.hw?.net || []).slice().sort((a, b) => (b.max_mbit || 0) - (a.max_mbit || 0));

    function renderBoard() {
        const data: Partial<StatusResponse> = state.data || {};
        const info: Partial<Info> = data.info || {};
        const hw = state.hw;
        const [cpu, igpu] = shortCpu(info.cpu).split(/\s+with\s+/i);
        const modules = hw?.memory || [];
        const filled = modules.filter(m => m.size_mb > 0);
        const ram = ramMB(info);
        const mt = filled[0] && (filled[0].configured_mt || filled[0].speed_mt);
        const part = filled[0] ? [filled[0].manufacturer, filled[0].part_number].filter(Boolean).join(' · ') : '';
        const link = (nav: string, key: Key): string => (caseKnown() && hw ? linkRow(navLink(nav, key)) : '');

        q('#unc-ov-compute').innerHTML = `
            <p class="unc-ov-main">${glyph(icon('chip'))}<strong>${escapeHtml(cpu || '–')}</strong>${info.threads ? ` <span class="unc-muted">${escapeHtml(t('cc.threads', { n: info.threads }))}</span>` : ''}</p>
            ${igpu ? `<p class="unc-muted">${glyph(icon('pcie'))}${escapeHtml(igpu)}</p>` : ''}
            ${
                ram
                    ? `<p class="unc-ov-ram" title="${escapeHtml(part)}">${glyph(icon('ram'))}<strong>${Math.round(ram / 1024)} GB ${escapeHtml(filled[0]?.type || '')}</strong>
                ${modules.length ? `<span class="unc-dimms">${modules.map(m => `<span class="${m.size_mb ? '' : 'empty'}">${m.size_mb ? fmtRam(m.size_mb) : '–'}</span>`).join('')}</span>` : ''}
                ${mt ? `<span class="unc-muted">${mt} MT/s</span>` : ''}</p>`
                    : emptyText(t('cc.none'))
            }
            ${link('case', 'ov.inside')}`;

        const ports = lanPorts();
        const nics = data.sensors?.nic || [];

        q('#unc-ov-net').innerHTML = `${
            ports.length
                ? `<div class="unc-ports">${ports
                      .map((n, i) => {
                          const chips = nics.filter(c => c.iface === n.name).filter(hasTemp);
                          const temp = chips.length ? Math.max(...chips.map(c => c.temp)) : null;
                          const hint = [`${t('temps.nic_chip')} · ${n.name}`, chips.map(c => `${c.label} ${c.temp} °C`).join(' / ')].filter(Boolean).join('\n');

                          return `<div>${statChip({ ico: icon('net'), label: ports.length > 1 ? t('case.lan', { n: i + 1 }) : t('case.lan_one'), sub: n.name, text: n.link ? speedText(n.speed_mbit) : t('cc.no_link'), level: n.link ? '' : 'na', hint, cls: 'is-row' })}${n.link && temp != null ? tempOf(temp, 'other') : '<span></span>'}</div>`;
                      })
                      .join('')}</div>`
                : emptyText(t('cc.none'))
        }${link('case', 'ov.rear')}`;
    }

    // A sensor that isn't wired reports nonsense (an NVMe sensor 2 that isn't there, -128 from an empty ITE input).
    const hasTemp = <T extends { temp: number | null }>(item: T): item is T & { temp: number } => item.temp != null;

    const plausible = (value: number | null | undefined): number | null => (value != null && value > -40 && value < 125 ? value : null);

    interface SensorVal {
        label: string;
        text: string;
        level: string;
    }

    const tempVal = (value: number | null | undefined, kind: TempKind, label = ''): SensorVal => {
        const temp = plausible(value);

        return { label, text: deg(temp), level: temp == null ? 'na' : tempClass(temp, kind) };
    };

    function sensorRow({
        ico,
        name,
        src = '',
        badges = [],
        vals,
        title = ''
    }: {
        ico: string;
        name: string;
        src?: string;
        badges?: string[];
        vals: SensorVal[];
        title?: string;
    }): string {
        return `
            <div class="unc-sn-row ${vals.length > 1 ? 'multi' : ''}"${title ? ` title="${escapeHtml(title)}"` : ''}>
                ${glyph(ico, 'lg')}
                <span class="unc-sn-name"><strong>${escapeHtml(name)}</strong>${src ? `<small class="unc-muted">${escapeHtml(src)}</small>` : ''}</span>
                <span class="unc-sn-badges">${badges.join('')}</span>
                <span class="unc-sn-vals">${vals.map(v => statChip({ label: v.label, text: v.text, level: v.level, cls: 'is-stack is-end' })).join('')}</span>
            </div>`;
    }

    function sensorDevice(iconName: IconName, title: string, main: string, lines: string[], state: string, good: string | undefined): string {
        return `
            <div class="unc-sn-dev">
                <span class="unc-sn-devico">${icon(iconName)}</span>
                <div>
                    <small class="unc-muted">${escapeHtml(title)}</small>
                    <strong>${escapeHtml(main)}</strong>
                    ${lines
                        .filter(Boolean)
                        .map(line => `<small class="unc-muted">${escapeHtml(line)}</small>`)
                        .join('')}
                </div>
                ${state ? pill({ text: state, ...(good === undefined ? {} : { tone: good }) }) : ''}
            </div>`;
    }

    const memoryRows = (modules: Dimm[]): string[] =>
        modules.map((m, i) =>
            sensorRow({
                ico: icon('ram'),
                name: t('case.ram_slot', { n: i + 1 }),
                src: m.size_mb ? [m.manufacturer, m.part_number].filter(Boolean).join(' · ') : '',
                title: m.configured_mt && m.configured_mt !== m.speed_mt ? t('case.running_at', { speed: m.configured_mt, max: m.speed_mt }) : '',
                vals: m.size_mb
                    ? [
                          { label: '', text: `${fmtRam(m.size_mb)} ${m.type || ''}`.trim(), level: '' },
                          { label: t('sensors.speed'), text: `${m.configured_mt || m.speed_mt} MT/s`, level: '' }
                      ]
                    : [{ label: '', text: t('case.empty'), level: 'na' }]
            })
        );

    function sensorDevices(): string {
        const data = loaded();
        const info: Partial<Info> = data.info || {};
        const led = ledControllerState();
        const status = state.ledStatus;
        const ports = /IT5571/.test(data.chip || '') ? '0x62 / 0x66' : '';
        const fanState: [Key, string] =
            state.data?.fan_caps === undefined && modelName()
                ? ['sensors.no_fanctl', '']
                : !data.chip
                  ? ['sensors.not_found', 'bad']
                  : fanReadOnly()
                    ? ['sensors.read_only', '']
                    : fanOff()
                      ? ['sensors.ctl_off', 'warn']
                      : ['sensors.ctl_on', 'ok'];
        const ledState: [Key, string] | null = led === 'found' ? ['sensors.found', 'ok'] : led === 'missing' ? ['sensors.not_found', 'bad'] : null;

        return [
            sensorDevice(
                'fan',
                t('system.chip'),
                data.chip ? chipName(data.chip) : '–',
                [ports && t('sensors.ports', { ports }), (data.fans || []).length ? t('sensors.fans_n', { n: data.fans?.length }) : ''],
                t(fanState[0]),
                fanState[1]
            ),
            ledsOn()
                ? sensorDevice(
                      'bulb',
                      t('system.ledctl'),
                      status?.controller || t('led.title'),
                      [status?.i2c_address ? t('sensors.led_bus', { bus: status.i2c_bus, address: status.i2c_address }) : ledController()],
                      ledState ? t(ledState[0]) : '',
                      ledState?.[1]
                  )
                : sensorDevice('bulb', t('system.ledctl'), t('led.title'), [ledController()], t('sensors.ctl_off'), 'warn'),
            sensorDevice(
                'bios',
                t('system.model'),
                info.model || '–',
                [
                    [info.bios ? `${t('system.bios')} ${info.bios}` : '', biosDate(info.bios_date)].filter(Boolean).join(' · '),
                    [shortCpu(info.cpu).split(/\s+with\s+/i)[0], info.threads ? t('cc.threads', { n: info.threads }) : ''].filter(Boolean).join(' · ')
                ],
                '',
                ''
            )
        ].join('');
    }

    function renderSensors() {
        const data = loaded();
        const s: Partial<Sensors> = data.sensors || {};
        const info: Partial<Info> = data.info || {};
        const settings: Partial<Settings> = data.settings || {};
        const steering = fanKnown() && !fanOff();
        const intel = /intel/i.test(info.cpu || '');
        const caseSrc = caseSource();
        const ignored = new Set(settings.ignore_drives || []);
        const limit = (value: number): string => pill({ text: t('sensors.limit', { temp: value }) });
        const driver = (name: string, from: string): string =>
            steering && caseSrc.from === from && caseSrc.drive === name ? pill({ tone: 'accent', ico: 'fan', text: t('sensors.drives_case') }) : '';
        const skipped = (name: string): string => (steering && ignored.has(name) ? pill({ text: t('sensors.ignored') }) : '');
        const diskCrit = settings.disk_crit ?? 55;
        const nvmeCrit = diskCrit + (settings.nvme_offset ?? 15);
        const groups: string[] = [];
        const group = (titleKey: Key, iconName: IconName, rows: string[], { one = false, hint = '' }: { one?: boolean; hint?: string } = {}): void => {
            if (!rows.length && !hint) return;
            groups.push(`
                <article class="unc-card unc-sn-group">
                    ${cardHead(t(titleKey), { ico: iconName })}
                    ${rows.length ? `<div class="unc-sn-list ${one ? 'one' : ''}">${rows.join('')}</div>` : ''}
                    ${hint ? note(escapeHtml(hint)) : ''}
                </article>`);
        };

        const board: string[] = [];
        const cores = (s.cpu_cores || []).flatMap(c => (c.temp == null ? [] : [c.temp]));

        board.push(
            sensorRow({
                ico: icon('chip'),
                name: t('temps.cpu'),
                src: intel ? 'coretemp' : 'k10temp',
                vals: [tempVal(s.cpu, 'cpu')],
                badges: [steering ? pill({ tone: 'accent', ico: 'fan', text: t('sensors.drives_cpu') }) : '', steering ? limit(settings.cpu_crit ?? 90) : '']
            })
        );
        if (cores.length)
            board.push(
                sensorRow({ ico: icon('gauge'), name: t('sensors.cores'), src: intel ? 'coretemp' : 'k10temp', vals: [tempVal(Math.max(...cores), 'cpu')] })
            );
        board.push(sensorRow({ ico: icon('pcie'), name: t('temps.gpu'), src: intel ? 'i915' : 'amdgpu', vals: [tempVal(s.gpu, 'cpu')] }));
        board.push(
            sensorRow({ ico: icon('board'), name: t('temps.board'), src: data.chip ? chipName(data.chip) : 'acpitz', vals: [tempVal(s.board, 'other')] })
        );
        (data.chip_temps || []).forEach((temp, i) => {
            if (plausible(temp) != null)
                board.push(
                    sensorRow({ ico: icon('fan'), name: t('sensors.chip_sensor', { n: i + 1 }), src: chipName(data.chip), vals: [tempVal(temp, 'other')] })
                );
        });
        group('sensors.group_board', 'chip', board);

        const fans = data.fans || [];
        group(
            'sensors.group_fans',
            'fan',
            fans.map(fan =>
                sensorRow({
                    ico: fanIcon(fan, icon('fan')),
                    name: fanName(fan, fans),
                    src: [data.chip, fan.id].filter(Boolean).join(' · '),
                    badges: steering ? [pill({ text: fanSourceShort(fan) })] : [],
                    vals: [
                        {
                            label: '',
                            text: fan.rpm > 0 ? `${fan.rpm} ${t('fan.rpm')}` : t(fan.error ? 'fan.error' : 'fan.stopped'),
                            level: fan.rpm > 0 && !fan.error ? '' : 'hot'
                        },
                        { label: t('sensors.power'), text: fan.error ? '–' : fan.pct == null ? t('sensors.auto') : `${fan.pct} %`, level: '' }
                    ]
                })
            ),
            { hint: fanOff() ? t('fanctl.off_note') : '' }
        );

        // NVMe SSDs: the composite value steers the fans, the extra sensors are shown only.
        const extras = s.nvme_extra || [];
        const ssds: { id: string; label: string; model: string; temp: number | null; boot?: boolean }[] = state.hw?.nvme?.length
            ? state.hw.nvme.map(n => ({ id: n.id, label: ssdSlotLabel(n), model: n.model || n.id, temp: n.temp, boot: n.boot }))
            : (s.nvme || []).map(n => ({ id: n.name, label: n.name, model: n.label || n.name, temp: n.temp }));

        group(
            'sensors.group_ssd',
            'm2',
            ssds.map(n =>
                sensorRow({
                    ico: n.boot ? UNRAID_MARK : icon('m2'),
                    name: n.label,
                    src: n.model,
                    badges: [driver(n.id, 'nvme'), skipped(n.id), steering ? limit(nvmeCrit) : ''],
                    vals: [
                        tempVal(n.temp, 'nvme', t('sensors.composite')),
                        ...extras.filter(e => e.name === n.id && plausible(e.temp) != null).map(e => tempVal(e.temp, 'nvme', e.label || t('temps.sensor')))
                    ]
                })
            ),
            { one: true }
        );

        // Drives: a sleeping drive shows "Standby"; the temperature is Unraid's last reading, never a new one.
        const bays = state.hw?.bays || [];
        group(
            'sensors.group_drives',
            'drives',
            (s.disks || []).map(d => {
                const bay = bays.find(b => b.device === d.device);
                const kind = d.hdd ? 'disk' : 'nvme';

                return sensorRow({
                    ico: icon('drives'),
                    name: driveLabel(d.name),
                    src: [bay ? t('sensors.bay', { n: bay.bay }) : '', friendly(bay?.model)].filter(Boolean).join(' · '),
                    title: d.device,
                    badges: [driver(d.name, 'disk'), skipped(d.name), steering && !d.standby && plausible(d.temp) != null ? limit(diskCrit) : ''],
                    vals: [d.standby ? standbyVal() : tempVal(d.temp, kind)]
                });
            }),
            { hint: t('sensors.drives_note') }
        );

        const nics = s.nic || [];
        const ports = lanPorts();
        const nicRow = (name: string, iface: string, port: NetPort | null): string => {
            const chips = nics.filter(c => c.iface === iface);
            const speed = port ? (port.link ? speedText(port.speed_mbit) : t('cc.no_link')) : '';
            const max = port?.link && port.max_mbit > port.speed_mbit ? t('sensors.up_to', { speed: speedText(port.max_mbit) }) : '';

            return sensorRow({
                ico: icon('net'),
                name,
                src: [iface, speed, max].filter(Boolean).join(' · '),
                vals: chips.length ? chips.map(c => tempVal(c.temp, 'other', c.label || t('temps.nic_chip'))) : [tempVal(null, 'other')]
            });
        };

        group(
            'sensors.group_net',
            'net',
            [
                ...ports.map((p, i) => nicRow(ports.length > 1 ? t('case.lan', { n: i + 1 }) : t('case.lan_one'), p.name, p)),
                ...[...new Set(nics.map(c => c.iface))].filter(iface => !ports.some(p => p.name === iface)).map(iface => nicRow(iface, iface, null))
            ],
            { one: true }
        );

        const rams = memoryRows(state.hw?.memory || []);

        if (!rams.length && info.memory_kb)
            rams.push(
                sensorRow({ ico: icon('ram'), name: t('system.memory'), vals: [{ label: '', text: `${Math.round(info.memory_kb / 1048576)} GB`, level: '' }] })
            );
        group('sensors.group_mem', 'ram', rams, { hint: t('sensors.mem_note') });

        q('#unc-sensors').innerHTML = `<div class="unc-sn-devs">${sensorDevices()}</div>${groups.join('')}`;
    }

    // Only the newest request may write the state: a poll that started before a save would bring back the old settings.
    let loadSeq = 0;

    async function load(): Promise<void> {
        const seq = ++loadSeq;
        let data: StatusResponse | null;

        try {
            data = await api.get('status');
            // A stopped daemon leaves its last state file behind: not current.
            if (data && !data.running) data.daemon = null;
        } catch (error) {
            data = null;
        }
        if (seq !== loadSeq) return;
        state.data = data;
        updateNetAct(data?.net_io);
        render();
        paintNetAct();
    }

    const polls: Partial<Record<'status' | 'hardware' | 'leds', { poke(): void }>> = {};

    function showView(nav: string): void {
        // "led:colours": the LED pages are one view of ours, each of their pages an entry of the sidebar.
        const [view, sub] = nav.split(':');

        state.view = view ?? '';
        state.nav = nav;
        renderTitle();
        if (view === 'led') {
            window.ULC?.show(sub ?? '');
            q('#unc-led-head').hidden = sub !== 'overview';
        }
        if (view === 'case' || view === 'overview' || view === 'sensors') polls.hardware?.poke();
        if (view === 'case' || view === 'overview' || view === 'sensors') polls.leds?.poke();
        polls.status?.poke();
        qa('[data-nav]').forEach(button => button.classList.toggle('active', button.dataset.nav === nav));
        const activeTab = q('.unc-nav button.active');
        const tabRow = activeTab.parentElement;
        if (tabRow && tabRow.scrollWidth > tabRow.clientWidth) {
            tabRow.scrollLeft = activeTab.offsetLeft - (tabRow.clientWidth - activeTab.offsetWidth) / 2;
        }
        qa('.unc-view').forEach(section => section.classList.toggle('active', section.dataset.view === view));
        queueDock();
    }

    window.UNC_NAV = nav => showView(nav);

    function bindEvents(): void {
        qa('[data-unc-icon]').forEach(element => {
            element.innerHTML = iconOf(element.dataset.uncIcon);
        });

        qa('[data-nav]').forEach(button => {
            button.addEventListener('click', () => showView(button.dataset.nav ?? ''));
        });
        q('.unc-ov').addEventListener('click', event => {
            const link = hit(event)?.closest<HTMLElement>('[data-nav]');
            if (link) showView(link.dataset.nav ?? '');
        });

        ['#unc-modes', '#unc-modes-fans'].forEach(id =>
            q(id).addEventListener('click', event => {
                const button = hit(event)?.closest<HTMLButtonElement>('[data-mode]');
                if (!button || button.disabled) return;

                const mode = button.dataset.mode;
                if (!isMode(mode)) return;
                if (state.curvesDirty) {
                    if (!window.confirm(t('mode.discard_confirm'))) return;
                    discardCurves();
                }
                run(() => api.post('mode', { value: mode }), t('save.saved'));
            })
        );

        qa('[data-preset]').forEach(button =>
            button.addEventListener('click', () => {
                const mode = button.dataset.preset;
                const preset = mode === undefined ? undefined : state.data?.presets?.[mode];
                if (!mode || !preset || !curvesEditable()) return;

                state.curves = { cpu: parseCurve(preset[0]), case: parseCurve(preset[1]) };
                markDirty('curves', 'cpu');
                markDirty('curves', 'case');
                buildPoints('cpu');
                buildPoints('case');
                renderCurves();
                renderSaveBars();
                toast(t('curves.loaded', { mode: tRaw(`mode.${mode}`) }));
            })
        );

        q('#unc-flow').addEventListener('click', event => {
            const tab = hit(event)?.closest<HTMLElement>('[role="tab"]');
            const start = hit(event)?.closest('[data-measure]');

            if (tab) selectStep(tab.dataset.step ?? '');
            if (start) startMeasure(start.hasAttribute('data-confirm'));
        });

        q('#unc-stepper').addEventListener('keydown', event => {
            const tabs = qa('#unc-stepper [role="tab"]');
            const at = tabs.findIndex(tab => tab === document.activeElement);
            const to = { ArrowRight: at + 1, ArrowLeft: at - 1, Home: 0, End: tabs.length - 1 }[event.key];

            if (to === undefined || at < 0) return;
            event.preventDefault();
            const tab = tabs[(to + tabs.length) % tabs.length];
            if (!tab) return;
            selectStep(tab.dataset.step ?? '');
            tab.focus();
        });

        q('[data-discard="curves"]').addEventListener('click', discardCurves);

        q('#unc-customize').addEventListener('click', () => {
            state.arm = true;
            state.curves = activeCurves();
            markDirty('curves');
            state.saveError.curves = '';
            buildPoints('cpu');
            buildPoints('case');
            renderCurves();
            renderSaveBars();
        });

        // The minimum speed lives only here: saved with the curves in Custom, via the min action in a preset mode.
        const curveMin = q<HTMLInputElement>('#unc-curve-min');
        curveMin.addEventListener('input', () => {
            const v = parseInt(curveMin.value, 10);
            if (Number.isNaN(v)) return;
            const lo = Number(curveMin.min) || 10,
                hi = Number(curveMin.max) || 60;
            state.minEdit = Math.max(lo, Math.min(hi, v));
            markDirty('curves', 'min');
            state.saveError.curves = '';
            showCurveMin();
            renderSaveBars();
            renderCurves();
        });

        qa('[data-chart]').forEach(svg =>
            new ResizeObserver(() => {
                const kind = svg.closest<HTMLElement>('[data-curve]')?.dataset.curve;
                if (Math.round(svg.clientWidth) && svg.dataset.w !== String(Math.round(svg.clientWidth)) && (kind === 'cpu' || kind === 'case'))
                    drawChart(kind);
            }).observe(svg)
        );

        const caseCpu = q<HTMLInputElement>('#unc-case-cpu');
        caseCpu.addEventListener('change', () => {
            state.caseFollows = caseCpu.checked;
            markDirty('curves', 'mix');
            state.saveError.curves = '';
            renderSaveBars();
            renderCurvesMode();
            drawChart('case');
            paintMix();
        });

        q('[data-discard="safety"]').addEventListener('click', discardSafety);

        window.addEventListener('scroll', queueDock, true);
        window.addEventListener('resize', queueDock);

        window.addEventListener('beforeunload', event => {
            if (anyDirty()) event.preventDefault();
        });

        q('#unc-curves-save').addEventListener('click', () => {
            const { cpu, case: cs } = state.curves;

            if (!curvesEditable()) {
                saveFanPage('curves', () => api.post('min', { min_pct: minPct(), case_follows_cpu: state.caseFollows ? '1' : '0' }), t('save.applied'));
                return;
            }

            if (!validCurve(cpu) || !validCurve(cs)) {
                toast(t('curves.invalid'), 'error');
                return;
            }

            saveFanPage(
                'curves',
                () =>
                    api.post('curves', {
                        cpu_curve: curveText(cpu),
                        case_curve: curveText(cs),
                        case_follows_cpu: state.caseFollows ? '1' : '0',
                        min_pct: minPct()
                    }),
                t('curves.saved')
            ).then(() => {
                if (!state.curvesDirty) {
                    state.arm = false;
                    renderCurvesMode();
                }
            });
        });

        SAFETY.forEach(field => {
            q<HTMLInputElement>(`#unc-${field.id}`).addEventListener('input', () => {
                markDirty('safety', field.id);
                state.saveError.safety = '';
                showSafetyValue(field);
                renderSafetyLive();
                renderSaveBars();
            });
        });
        q('#unc-ignore-list').addEventListener('change', () => {
            markDirty('safety', 'ignore');
            state.saveError.safety = '';
            renderSaveBars();
        });
        q<HTMLInputElement>('#unc-stall-notify').addEventListener('change', () => {
            markDirty('safety', 'stall');
            state.saveError.safety = '';
            renderSaveBars();
        });

        q('#unc-safety-save').addEventListener('click', () => {
            const values = { cpu_crit: '', disk_crit: '', nvme_offset: '' };
            SAFETY.forEach(field => {
                values[field.key] = q<HTMLInputElement>(`#unc-${field.id}`).value;
            });

            saveFanPage(
                'safety',
                () =>
                    api.post('safety', {
                        ...values,
                        stall_notify: q<HTMLInputElement>('#unc-stall-notify').checked ? '1' : '0',
                        ignore_drives: ignoredDrives()
                    }),
                t('safety.saved')
            );
        });

        bindModelsFold();
        q('#unc-confirm-send').addEventListener('click', openConfirmIssue);

        q('#unc-conflicts').addEventListener('click', event => {
            const what = hit(event)?.closest<HTMLElement>('[data-conflict-off]')?.dataset.conflictOff;

            if (what === 'fan') {
                run(async () => {
                    await api.post('fan_control', { value: 'off' });
                    await load();
                }, t('save.saved'));
            } else if (what === 'led') {
                run(async () => {
                    await api.post('led', { value: 'off' });
                    reloadSaved();
                });
            }
        });

        const fanEnabled = q<HTMLInputElement>('#unc-fan-enabled');
        fanEnabled.addEventListener('change', () => {
            const on = fanEnabled.checked;

            run(async () => {
                await api.post('fan_control', { value: on ? 'on' : 'off' });
                await load();
            }, t('save.saved'));
        });

        q('#unc-led-import-btn').addEventListener('click', () => {
            run(async () => {
                await api.post('led_import', {});
                location.reload();
            }, t('leds.imported'));
        });

        const ledEnabled = q<HTMLInputElement>('#unc-led-enabled');
        ledEnabled.addEventListener('change', () => {
            const on = ledEnabled.checked;

            run(async () => {
                await api.post('led', { value: on ? 'on' : 'off' });
                // The LED pages are part of the page, so it loads again with or without them; back on the overview.
                try {
                    sessionStorage.setItem('unc-nav', 'led:overview');
                } catch (error) {
                    /* then it opens on the cockpit */
                }
                reloadSaved();
            });
        });

        const theme = q<HTMLSelectElement>('#unc-theme');
        theme.value = window.UGREEN_NAS_THEME || 'dark';

        theme.addEventListener('change', async () => {
            root.dataset.theme = resolveTheme(theme.value);
            const ledRoot = document.getElementById('ulc');
            if (ledRoot) ledRoot.dataset.theme = root.dataset.theme;
            window.UGREEN_NAS_THEME = theme.value;

            try {
                await api.post('theme', { value: theme.value });
                saved();
            } catch (error) {
                toast(errorMessage(error), 'error');
            }
        });

        const language = q<HTMLSelectElement>('#unc-language');
        language.addEventListener('change', async () => {
            try {
                await api.post('lang', { value: language.value });
                reloadSaved();
            } catch (error) {
                toast(errorMessage(error), 'error');
            }
        });
    }

    function translatePage(): void {
        qa('[data-i18n]').forEach(element => {
            element.textContent = tRaw(element.dataset.i18n ?? '');
        });
        qa('[data-i18n-html]').forEach(element => {
            element.innerHTML = tRaw(element.dataset.i18nHtml ?? '');
        });
        // Narrow layout shows icons only: the label stays as tooltip and accessible name.
        qa('.unc-nav button[data-nav]').forEach(button => {
            const label = button.querySelector('[data-i18n]')?.textContent ?? '';
            button.title = label;
            button.setAttribute('aria-label', label);
        });
    }

    function paintLedsNow(): void {
        applyLeds(q('#unc-hero'));
        renderCaseFoot();
    }

    async function init() {
        translatePage();
        mountStatic();
        mountFanModules();
        bindEvents();

        q('#unc-version').textContent = window.UGREEN_NAS_VERSION || '';
        q<HTMLSelectElement>('#unc-language').value = window.UGREEN_NAS_LANG_SETTING || 'auto';

        try {
            const nav = sessionStorage.getItem('unc-nav');
            sessionStorage.removeItem('unc-nav');
            if (nav) showView(nav);
            if (sessionStorage.getItem('unc-saved')) {
                sessionStorage.removeItem('unc-saved');
                saved();
            }
        } catch (error) {
            /* storage blocked */
        }

        await load();
        loadHardware()
            .then(() => loadLeds())
            .then(paintLedsNow);

        // Polling cost: the NAS runs a process per request, so each resource has its own pace and only
        // runs on the views that show it (hidden/idle tabs: see UNCPoll). The fan daemon itself refreshes every 2 s.
        const LIVE = ['overview', 'case', 'curves', 'history', 'sensors'];
        const decor = () => state.view === 'case' || state.view === 'overview' || state.view === 'sensors';

        polls.status = window.UNCPoll.every(
            () => (state.busy ? null : load()),
            () => (state.data?.calibration?.progress?.state === 'running' ? 3000 : LIVE.includes(state.view) ? 5000 : 15000)
        );
        polls.hardware = window.UNCPoll.every(() => (state.busy ? null : loadHardware()), 60000, decor);
        polls.leds = window.UNCPoll.every(() => (state.busy ? null : loadLeds().then(paintLedsNow)), LED_POLL_MS, decor);
    }

    init();
}

const root = document.getElementById('unc');

if (root) main(root);
