// The LED pages (Overview, Drives, Night mode, System) inside the NAS page. One closure over the page state;
// the pure parts (texts, theme, api client) live in ../shared and ./text.
import { createClient, errorMessage } from '../../core/api';
import type { LedBay, LedNetworkPort, LedGet, LedPost, LedSchedule, LedState, LedStatusResponse, LedWriteAction } from '../../core/contract';
import { hit, setRangeFill } from '../../core/dom';
import { linkSpeed, speedShort } from '../../core/format';
import { hexToRgb, levelFromPercent, normalizeHex, rgbToHex } from '../../core/color';
import { isLedAllResponse, isLedStatusResponse } from '../../core/guard';
import { escapeHtml } from '../../core/html';
import { LED_ICONS } from '../../core/icons';
import { resolveTheme } from '../../core/theme';
import { findText, type Key, lang, t, tRaw } from './text';

function main(root: HTMLElement): void {
    const API = '/plugins/ugreen-nas-control/led/api.php';

    root.dataset.theme = resolveTheme(window.UGREEN_LED_THEME);

    const q = <T extends HTMLElement = HTMLElement>(selector: string, within: ParentNode = root): T => {
        const element = within.querySelector<T>(selector);
        if (!element) throw new Error(`Missing ${selector}`);
        return element;
    };
    const qa = <T extends HTMLElement = HTMLElement>(selector: string): T[] => [...root.querySelectorAll<T>(selector)];
    const sleep = (ms: number): Promise<void> => new Promise(resolve => setTimeout(resolve, ms));

    const ICONS = LED_ICONS;

    type IconName = keyof typeof ICONS;
    const icon = (name: IconName): string => `<svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[name]}</svg>`;
    const iconOf = (name: string | undefined): string => `<svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[name as IconName] || ''}</svg>`;

    const DEFAULTS = {
        power: '#ffffff',
        netdev: '#00c853',
        disk: '#ffa000',
        error: '#ff3b30',
        brightness: 80
    };

    // on/off times in ms that the driver accepts for breath and blink.
    const SPEEDS: { label: Key; breath: [number, number]; blink: [number, number] }[] = [
        { label: 'speed.0', breath: [3000, 2000], blink: [1000, 1000] },
        { label: 'speed.1', breath: [2000, 1500], blink: [700, 700] },
        { label: 'speed.2', breath: [1200, 900], blink: [450, 450] },
        { label: 'speed.3', breath: [700, 500], blink: [250, 250] },
        { label: 'speed.4', breath: [400, 300], blink: [167, 167] } // 3 Hz at most (WCAG 2.3.1)
    ];

    type EffectId = 'static' | 'breath' | 'blink' | 'rainbow' | 'temperature' | 'off';
    const EFFECTS: { id: EffectId; icon: IconName; software?: boolean }[] = [
        { id: 'static', icon: 'bulb' },
        { id: 'breath', icon: 'wave' },
        { id: 'blink', icon: 'activity' },
        { id: 'rainbow', icon: 'rainbow', software: true },
        { id: 'temperature', icon: 'thermo', software: true },
        { id: 'off', icon: 'power' }
    ];

    interface UiLed {
        color: string;
        brightness: number;
        effect: string;
        on_ms: number;
        off_ms: number;
        sent_ms: LedState['sent_ms'];
        now_ms: LedState['now_ms'];
        rx: LedState['rx'];
    }

    interface State {
        lastLevel: Record<string, number>;
        model: string;
        bays: number;
        order: string[];
        leds: Record<string, UiLed>;
        status: Partial<LedStatusResponse>;
        bayInfo: LedBay[];
        schedule: LedSchedule | null;
        busy: number;
        writeSeq: number;
        speed: number;
        sysTarget: string;
        dragging: boolean;
        identifyTimer?: ReturnType<typeof setTimeout>;
    }

    const state: State = {
        lastLevel: {},
        model: 'UGREEN NAS',
        bays: 0,
        order: [],
        leds: {},
        status: {},
        bayInfo: [],
        schedule: null,
        busy: 0,
        writeSeq: 0,
        speed: 2,
        sysTarget: 'both',
        dragging: false
    };

    const disks = () => state.order.filter(led => led.startsWith('disk'));

    // The two LED groups of the overview, each with its effect tiles. In status mode the daemon owns
    // the drive and network LEDs, so only the power LED (and a manual network LED) take an effect.
    const GROUPS = ['system', 'drives'] as const;
    type Group = (typeof GROUPS)[number];
    const ANIMATED: EffectId[] = ['breath', 'blink', 'rainbow', 'temperature'];

    // Power and the network LEDs. netdev2 is the second LAN LED of the iDX6011 Pro; it only shows where the LED list has it.
    const SYSTEM_LEDS = ['power', 'netdev', 'netdev2'];
    const systemLeds = (): string[] => SYSTEM_LEDS.filter(led => state.order.includes(led));
    const systemTargets = (): string[] => systemLeds().filter(led => !managed(led));

    function groupLeds(group: Group): string[] {
        if (group === 'drives') return statusMode() ? [] : disks();

        const targets = systemTargets();

        return targets.length > 1 && targets.includes(state.sysTarget) ? [state.sysTarget] : targets;
    }

    const rowLeds = (key: string): string[] => (key === 'drives' ? disks() : [key]);
    const rowDefault = (key: string): string => (key === 'power' ? DEFAULTS.power : key === 'netdev' || key === 'netdev2' ? DEFAULTS.netdev : DEFAULTS.disk);

    const statusMode = () => state.status.mode === 'status';

    const fxLeds = () => (state.status.fx_leds || '').split(',').filter(Boolean);
    const fxOn = (led: string): boolean => Boolean(state.status.fx) && fxLeds().includes(led);

    // In status mode the daemon drives disk and network LEDs; only colour and brightness stay manual.
    // The network LED can be left manual (netdev_status).
    const netdevManual = () => state.status.netdev_status === 'manual';
    const managed = (led: string): boolean => statusMode() && (led.startsWith('disk') || (led === 'netdev' && !netdevManual()));

    const ledLabel = (led: string): string =>
        led === 'drives'
            ? t('drives.all')
            : led === 'power'
              ? t('led.power')
              : led === 'netdev'
                ? t('led.netdev')
                : led === 'netdev2'
                  ? t('led.netdev2')
                  : t('led.bay', { n: led.slice(4) });

    const effectName = (id: EffectId): string => t(`effect.${id}`);

    let toastTimer: ReturnType<typeof setTimeout> | undefined;
    function toast(message: string, type = 'success'): void {
        const element = q('#ulc-toast');

        element.textContent = message;
        element.className = `ulc-toast show ${type}`;

        if (type === 'error') console.error('[UGREEN LED]', message);

        clearTimeout(toastTimer);
        toastTimer = setTimeout(
            () => {
                element.className = 'ulc-toast';
            },
            type === 'error' ? 6000 : 2500
        );
    }

    // Every write counts twice (start, end), so a read that was in flight while a
    // write ran can be told apart and dropped: its values are older than the write.
    const client = createClient<LedGet, LedPost>({
        base: API,
        csrf: () => window.UGREEN_LED_CSRF_TOKEN || '',
        reads: 'shared',
        checks: { status: isLedStatusResponse, all: isLedAllResponse },
        badResponse: status => t('error.response', { status }),
        // Known api.php messages are translated by their English text.
        failed: (data, status) => (data.error && findText(`api.${data.error}`)) || data.error || t('error.api', { status })
    });

    const get = <A extends keyof LedGet & string>(action: A, maxAge = 0): Promise<LedGet[A]> => client.get(action, {}, maxAge);

    async function post<A extends keyof LedPost & string>(action: A, params: LedPost[A]): Promise<void> {
        state.writeSeq++;

        try {
            await client.post(action, params);
        } finally {
            state.writeSeq++;
        }
    }

    async function write<A extends LedWriteAction>(leds: string[], action: A, extra: Omit<LedPost[A], 'led'> = {} as Omit<LedPost[A], 'led'>): Promise<void> {
        if (!leds.length) return;

        state.busy++;

        try {
            await post(action, { led: leds.join(','), ...extra } as LedPost[A]);
        } finally {
            state.busy--;
        }
    }

    async function run(action: () => Promise<unknown>, success?: string): Promise<void> {
        try {
            await action();
            if (success) toast(success);
        } catch (error) {
            toast(errorMessage(error), 'error');
        }

        // An action may have started or ended a rainbow/temperature effect. The cheap read (the LEDs) does
        // not wait for the costly one (status).
        await Promise.all([loadStatus(), refresh()]);
    }

    // led/status and bays are the costly reads (hundreds of processes on the NAS): once a minute, at load,
    // on a page switch and after the user changed something (those pass maxAge 0).
    const STATUS_POLL_MS = 60000;
    const ALL_POLL_MS = 10000;

    async function loadStatus(maxAge = 0): Promise<void> {
        try {
            const data = await get('status', maxAge);

            state.status = data;
            state.model = data.model || 'UGREEN NAS';

            const order = Array.isArray(data.leds) ? data.leds : [];
            const baysChanged = data.bays !== state.bays || order.join() !== state.order.join();

            state.bays = data.bays;
            state.order = order;

            if (baysChanged) buildLayout();

            renderStatus();
            renderMode();
            renderNight();
            renderAlerts();
            renderDisplay();
            renderNetwork();
            renderShutdown();
            await loadBays(maxAge);
        } catch (error) {
            renderStatus();
        }
    }

    async function loadBays(maxAge = 0): Promise<void> {
        try {
            const data = await get('bays', maxAge);
            state.bayInfo = data.bays || [];
        } catch (error) {
            state.bayInfo = [];
        }

        renderBays();
    }

    async function refresh(maxAge = 0): Promise<void> {
        if (!state.order.length) return;

        const seq = state.writeSeq;

        try {
            const data = await get('all', maxAge);
            window.UNCLedFx.stamp(data.leds);

            // A write ran while this read was in flight; its own refresh follows.
            if (state.busy || seq !== state.writeSeq) return;

            const leds: Record<string, UiLed> = {};

            for (const [name, led] of Object.entries(data.leds || {})) {
                leds[name] = {
                    color: rgbToHex(led.color),
                    brightness: Number(led.brightness) || 0,
                    effect: led.effect || 'none',
                    on_ms: Number(led.on_ms) || 0,
                    off_ms: Number(led.off_ms) || 0,
                    sent_ms: led.sent_ms,
                    now_ms: led.now_ms,
                    rx: led.rx
                };
            }

            state.leds = leds;
            render();
        } catch (error) {
            renderStatus();
        }
    }

    function renderStatus() {
        q('#ulc-mapping-hint').hidden = !state.status.mapping || state.status.mapping === 'tested';
        q('#ulc-foreign').hidden = !state.status.foreign_monitor;
    }

    function buildLayout() {
        qa('[data-nas]').forEach(element => {
            element.style.setProperty('--bays', String(disks().length || 1));
            element.innerHTML =
                '<div class="ulc-nas-bays">' +
                disks()
                    .map(
                        (led, i) => `
                    <div class="ulc-bay">
                        <span class="ulc-bay-no">${String(i + 1).padStart(2, '0')}</span>
                        <span class="ulc-bay-info" data-bay-info="${led}"></span>
                        <span class="ulc-bay-knob"></span>
                        <span class="ulc-led" data-led="${led}"></span>
                    </div>`
                    )
                    .join('') +
                '</div>' +
                '<div class="ulc-nas-front">' +
                '<span class="ulc-led" data-led="power"></span><span class="ulc-led-label">Power</span>' +
                '<span class="ulc-led" data-led="netdev"></span><span class="ulc-led-label">LAN</span><span class="ulc-led-label ulc-link-speed" data-link-speed hidden></span>' +
                (state.order.includes('netdev2') ? '<span class="ulc-led" data-led="netdev2"></span><span class="ulc-led-label">LAN 2</span>' : '') +
                '<span class="ulc-nas-brand">UGREEN</span>' +
                '</div>';
        });

        buildRows(q('#ulc-system-table'), systemLeds());
        buildRows(q('#ulc-drives-table'), disks().length ? ['drives'] : []);
        buildRows(q('#ulc-bay-table'), disks(), true);
    }

    function render() {
        qa('.ulc-led[data-led]').forEach(element => paintLed(element, element.dataset.led ?? ''));
        renderGeneral();
        renderEffects();
        renderTables();
        renderFxSettings();
    }

    function paintLed(element: HTMLElement, led: string): void {
        const s = state.leds[led];

        if (!s) return;

        const { r, g, b } = hexToRgb(s.color);

        window.UNCLedFx.paint(
            element,
            {
                color: `${r} ${g} ${b}`,
                brightness: s.brightness,
                effect: s.effect,
                on_ms: s.on_ms,
                off_ms: s.off_ms,
                sent_ms: s.sent_ms,
                now_ms: s.now_ms,
                rx: s.rx
            },
            led,
            state.status
        );
        element.title = `${ledLabel(led)}: ${s.color.toUpperCase()}, ${Math.round(s.brightness / 2.55)} %`;
    }

    // Brightness 0-255 to show for an LED: the saved value whenever night mode or
    // the status daemon (spun-down disks are dimmed) changes what the LED itself
    // shows; the setting is what the user chose, not what the daemon derived.
    function dayLevel(led: string): number {
        const saved = state.status.saved_brightness?.[led];
        return (state.status.night || statusMode()) && saved != null ? Number(saved) : state.leds[led]?.brightness || 0;
    }

    function portColor(port: { link: boolean; speed: number }): string {
        if (!port.link || !state.status.netdev_speed_enabled) return '';

        const step = LINK_SPEEDS.filter(s => port.speed >= s).pop() || LINK_SPEEDS[0] || 10;
        const rgb = (state.status.netdev_speed_colors || {})[step];

        return rgb ? rgbToHex(rgb) : (SPEED_DEFAULTS[step] ?? '');
    }

    function renderLinkSpeed(): void {
        const ports = state.status.network_ports || [];
        const speed = Number(state.status.netdev_speed) || 0;
        const items =
            ports.length > 1
                ? ports.map(port => ({
                      text: speedShort(port.link ? port.speed : port.max),
                      down: !port.link,
                      color: portColor(port),
                      title: portTitle(port)
                  }))
                : speed > 0
                  ? [
                        {
                            text: speedShort(speed),
                            down: false,
                            color: portColor({ link: true, speed }),
                            title: t('display.speed_current', { speed: speedLabel(speed) })
                        }
                    ]
                  : [];

        qa('[data-link-speed]').forEach(element => {
            element.replaceChildren(
                ...items.map(item => {
                    const span = document.createElement('span');
                    span.innerHTML = item.down ? PORT_ICON_DOWN : PORT_ICON;
                    span.append(document.createTextNode(item.text));
                    span.title = item.title;
                    if (item.down) span.className = 'down';
                    if (item.color) span.style.setProperty('--port', item.color);
                    return span;
                })
            );
            element.hidden = !items.length;
        });
    }

    function portTitle(port: LedNetworkPort): string {
        const max = port.max ? ` (${t('fact.port_max', { speed: speedLabel(port.max) })})` : '';
        return `${port.name}: ${port.link ? (port.speed > 0 ? speedLabel(port.speed) : t('fact.port_up')) : t('fact.port_down')}${max}`;
    }

    function renderGeneral(): void {
        renderLinkSpeed();

        const note = q('#ulc-night-note');

        note.hidden = !state.status.night;
        if (state.status.night) {
            note.textContent = t(state.schedule?.action === 'off' ? 'night.note_off' : 'night.note_dim', { time: state.schedule?.end || '' });
        }
    }

    const speedLabel = (mbps: number): string => linkSpeed(mbps, lang() === 'en');

    // Network LED by link speed (Mbit/s); the backend picks the largest step at or below the speed.
    const LINK_SPEEDS = [10, 100, 1000, 2500, 5000, 10000];
    const SPEED_DEFAULTS: Record<number, string> = { 10: '#ff3b30', 100: '#ff8c00', 1000: '#ffd60a', 2500: '#00c853', 5000: '#00c8c8', 10000: '#0078ff' };

    const PORT_ICON_DOWN =
        '<svg class="ulc-port-ico" viewBox="0 0 20 16" aria-hidden="true"><rect x="1.5" y="2.5" width="17" height="12" rx="1.8"/><path d="M7.5 2.5V1.2h5v1.3"/><path d="M7.4 6.4l5.2 5.2M12.6 6.4l-5.2 5.2"/></svg>';
    const PORT_ICON =
        '<svg class="ulc-port-ico" viewBox="0 0 20 16" aria-hidden="true"><rect x="1.5" y="2.5" width="17" height="12" rx="1.8"/><path d="M7.5 2.5V1.2h5v1.3"/><path d="M5 8v4M6.6 8v4M8.2 8v4M9.8 8v4M11.4 8v4M13 8v4M14.6 8v4"/></svg>';

    // "name" tells a screen reader which colour the two fields (picker and hex value) belong to.
    const colorInput = (value: string, disabled = false, name = ''): string => `
        <div class="ulc-color-input">
            <label class="ulc-swatch" style="--c:${value}">
                <input type="color" value="${value}" ${disabled ? 'disabled' : ''} aria-label="${escapeHtml([t('colors.pick'), name].filter(Boolean).join(': '))}">
            </label>
            <input type="text" value="${value.toUpperCase()}" maxlength="7" spellcheck="false" ${disabled ? 'disabled' : ''} aria-label="${escapeHtml([t('colors.hex'), name].filter(Boolean).join(': '))}">
        </div>`;

    function ensureColor(selector: string, fallback: string): HTMLElement {
        const container = q(selector);

        if (!container.firstElementChild) {
            container.innerHTML = colorInput(fallback, false, tRaw(container.dataset.labelKey ?? ''));
        }

        return container;
    }

    function setColorInput(container: HTMLElement, value: string): void {
        q('.ulc-swatch', container).style.setProperty('--c', value);
        q<HTMLInputElement>('input[type="color"]', container).value = value;
        q<HTMLInputElement>('input[type="text"]', container).value = value.toUpperCase();
    }

    function currentEffect(leds: string[]): string | null {
        const effects = leds.map(led => {
            const s = state.leds[led];

            if (!s) return null;
            if (s.brightness === 0) return 'off';
            if (fxOn(led)) return state.status.fx;

            return s.effect === 'none' ? 'static' : s.effect;
        });

        return effects.length && effects.every(effect => effect && effect === effects[0]) ? (effects[0] ?? null) : null;
    }

    function buildEffects() {
        const tiles = EFFECTS.map(
            effect => `
            <button type="button" class="ulc-effect" data-effect="${effect.id}">
                ${icon(effect.icon)}
                <strong>${escapeHtml(effectName(effect.id))}</strong>
                <small>${escapeHtml(t(`effect.${effect.id}_text`))}</small>
            </button>`
        ).join('');

        GROUPS.forEach(group => {
            q(`[data-effects="${group}"]`).innerHTML = tiles;
        });
    }

    function renderEffects() {
        for (const group of GROUPS) {
            const leds = groupLeds(group);
            const active = leds.length ? currentEffect(leds) : null;

            qa<HTMLButtonElement>(`[data-effects="${group}"] [data-effect]`).forEach(button => {
                button.classList.toggle('active', button.dataset.effect === active);
                button.disabled = !leds.length;
            });
        }

        // The power and the network LED can take different effects: say which one a tile applies to.
        const targets = systemTargets();

        q('#ulc-system-target-row').hidden = targets.length < 2;
        qa<HTMLOptionElement>('#ulc-system-target option[data-led]').forEach(option => {
            option.hidden = !targets.includes(option.dataset.led ?? '');
        });
        q<HTMLSelectElement>('#ulc-system-target').value = targets.includes(state.sysTarget) ? state.sysTarget : 'both';
        q('#ulc-system-effect-title').textContent = targets.length === 1 ? t('group.effect_for', { led: ledLabel(targets[0] ?? '') }) : t('group.effect');
    }

    // Speed, direction and idle-only are one setting for all LEDs (a single effect process, one speed
    // for breathe and pulse), so they show once, and only the ones the effects in use understand.
    function renderFxSettings() {
        const used = new Set(state.order.filter(led => !managed(led)).map(led => currentEffect([led])));
        const uses = (effects: string[]): boolean => effects.some(effect => used.has(effect));
        const rows = {
            speed: uses(['breath', 'blink', 'rainbow']),
            direction: uses(['rainbow']),
            palette: uses(['rainbow']),
            idle: uses(['rainbow', 'temperature'])
        };

        q('#ulc-settings-card').hidden = !uses(ANIMATED);
        qa('#ulc-settings-card [data-fx-row]').forEach(element => {
            element.hidden = !rows[element.dataset.fxRow as keyof typeof rows];
        });
        q('#ulc-settings-card .ulc-form').hidden = !(rows.speed || rows.direction || rows.palette);
    }

    function buildRows(container: HTMLElement, keys: string[], withEffect = false): void {
        container.innerHTML = keys
            .map(key => {
                const label = ledLabel(key);

                return `
            <div class="ulc-led-row" data-led-row="${key}">
                ${key === 'drives' ? icon('drives') : `<span class="ulc-led" data-led="${key}"></span>`}
                <span>${escapeHtml(label)}<small data-led-sub="${key}"></small></span>
                <div class="ulc-color-reset">
                    ${colorInput('#ffffff', false, label)}
                    <button type="button" class="ulc-icon-btn" data-reset title="${escapeHtml(t('colors.reset'))}" aria-label="${escapeHtml(t('colors.reset_for', { led: label }))}">${icon('reset')}</button>
                </div>
                <div class="ulc-led-field">
                    <label for="ulc-br-${key}">${escapeHtml(t('row.brightness'))}</label>
                    <div class="ulc-inline">
                        <input type="range" id="ulc-br-${key}" min="0" max="100" step="1" value="0" data-brightness aria-label="${escapeHtml(t('brightness.label_for', { led: label }))}">
                        <output class="ulc-value" data-brightness-value for="ulc-br-${key}">0%</output>
                    </div>
                </div>
                ${
                    withEffect
                        ? `
                <div class="ulc-led-field" data-effect-field>
                    <label for="ulc-fx-${key}">${escapeHtml(t('row.effect'))}</label>
                    <select id="ulc-fx-${key}" data-led-effect aria-label="${escapeHtml(t('effect.label_for', { led: label }))}">
                        <option value="static">${t('effect.static')}</option>
                        <option value="breath">${t('effect.breath')}</option>
                        <option value="blink">${t('effect.blink')}</option>
                        <option value="off">${t('effect.off_short')}</option>
                        <option value="rainbow" disabled>${t('effect.rainbow')}</option>
                        <option value="temperature" disabled>${t('effect.temperature')}</option>
                    </select>
                </div>`
                        : ''
                }
            </div>`;
            })
            .join('');
    }

    function renderTables() {
        qa('[data-led-row]').forEach(row => {
            const leds = rowLeds(row.dataset.ledRow ?? '');
            const first = state.leds[leds[0] ?? ''];
            const effectField = row.querySelector<HTMLElement>('[data-effect-field]');

            // In status mode the daemon owns a bay's effect; the card says so.
            if (effectField) effectField.hidden = managed(leds[0] ?? '');

            if (!first || row.contains(document.activeElement)) return;

            setColorInput(row, first.color);

            const range = q<HTMLInputElement>('[data-brightness]', row);
            range.value = String(Math.round(Math.max(...leds.map(dayLevel)) / 2.55));
            setRangeFill(range);
            q('[data-brightness-value]', row).textContent = `${range.value}%`;

            const effect = row.querySelector<HTMLSelectElement>('[data-led-effect]');
            if (effect) effect.value = currentEffect(leds) ?? '';
        });
    }

    async function loadSchedule() {
        try {
            state.schedule = await get('schedule');
        } catch (error) {
            return;
        }

        const card = q('#ulc-night-card');
        if (!card.contains(document.activeElement)) {
            q<HTMLInputElement>('#ulc-night-enabled').checked = state.schedule.enabled;
            q<HTMLInputElement>('#ulc-night-start').value = state.schedule.start;
            q<HTMLInputElement>('#ulc-night-end').value = state.schedule.end;
            q<HTMLSelectElement>('#ulc-night-action').value = state.schedule.action;

            const slider = q<HTMLInputElement>('#ulc-night-brightness');
            slider.value = String(Math.max(1, Math.round(state.schedule.brightness / 2.55)));
            setRangeFill(slider);
            q('#ulc-night-brightness-value').textContent = `${slider.value}%`;
        }

        renderNight();
        renderGeneral(); // its night note names the end time
    }

    function renderNight() {
        const schedule = state.schedule;
        const night = Boolean(state.status.night);

        q<HTMLInputElement>('#ulc-night-brightness')
            .closest('.ulc-inline')
            ?.classList.toggle('disabled', q<HTMLSelectElement>('#ulc-night-action').value === 'off');

        if (!schedule) return;

        q('#ulc-night-state').textContent = !schedule.enabled
            ? t('night.state_off')
            : night
              ? t('night.state_night', { time: schedule.end })
              : t('night.state_day', { time: schedule.start });
    }

    function saveSchedule() {
        const start = q<HTMLInputElement>('#ulc-night-start').value;
        const end = q<HTMLInputElement>('#ulc-night-end').value;

        if (!start || !end) {
            toast(t('night.need_times'), 'error');
            return;
        }

        return run(async () => {
            await post('schedule', {
                enabled: q<HTMLInputElement>('#ulc-night-enabled').checked ? '1' : '0',
                start,
                end,
                night_action: q<HTMLSelectElement>('#ulc-night-action').value,
                brightness: levelFromPercent(Number(q<HTMLInputElement>('#ulc-night-brightness').value))
            });

            await loadStatus();
            await loadSchedule();
        }, t('night.saved'));
    }

    function renderMode() {
        const status = statusMode();
        const modeButton = q('#ulc-drive-mode-btn');

        q<HTMLSelectElement>('#ulc-mode').value = status ? 'status' : 'manual';
        q('#ulc-mode-hint').textContent = t(status ? 'mode.hint_status' : 'mode.hint_manual');

        if (document.activeElement !== q<HTMLSelectElement>('#ulc-direction')) {
            q<HTMLSelectElement>('#ulc-direction').value = state.status.fx_reverse ? 'backward' : 'forward';
        }
        q<HTMLInputElement>('#ulc-idle').checked = Boolean(state.status.fx_idle);

        q('#ulc-netdev-row').hidden = !status;
        if (document.activeElement !== q<HTMLSelectElement>('#ulc-netdev-status')) {
            q<HTMLSelectElement>('#ulc-netdev-status').value = state.status.netdev_status || 'activity';
        }

        // Drive LEDs: a line saying who drives them, instead of tiles that do nothing.
        // In status mode it also names the standby look, which lives on the Drive status page.
        q('#ulc-drive-note-text').textContent = status
            ? `${t('drives.note_status')} ${t('drives.note_standby', { mode: tRaw(`display.standby_${state.status.standby_mode || 'dim'}`) })}`
            : t('drives.note_manual');
        modeButton.textContent = t(status ? 'drives.set_manual' : 'drives.show_status');
        modeButton.dataset.act = status ? 'mode-manual' : 'mode-status';
        q('#ulc-drive-status-link').hidden = !status;
        q('#ulc-drive-effect').hidden = status;
        q('#ulc-display-note').hidden = status;

        renderEffects();
        renderTables();
        renderFxSettings();
    }

    function renderBays() {
        const container = q('#ulc-bays');

        if (!state.bayInfo.length) {
            container.innerHTML = `<p class="ulc-muted">${escapeHtml(t('bays.none'))}</p>`;
            return;
        }

        container.innerHTML = state.bayInfo
            .map(bay => {
                const kind = !bay.device ? 'empty' : bay.error ? 'error' : '';
                const text = !bay.device ? t('bays.empty') : bay.status ? tRaw(`disk.${bay.status}`) : t('bays.outside');

                return `
                <div class="ulc-bay-card ${kind}">
                    <strong><span class="ulc-led" data-led="${escapeHtml(bay.led)}"></span>${escapeHtml(t('led.bay', { n: bay.bay }))}</strong>
                    <small>${bay.device ? `/dev/${escapeHtml(bay.device)}${bay.slot ? ` · ${escapeHtml(bay.slot)}` : ''}` : escapeHtml(t('bays.no_disk'))}</small>
                    <small>${bay.serial ? escapeHtml(bay.serial) : ''}</small>
                    <span class="ulc-state ${kind}">${escapeHtml(text)}</span>
                    <small>${bay.device ? escapeHtml(bayCondition(bay)) : ''}</small>
                    ${bay.smart_warnings ? `<span class="ulc-state warning">${escapeHtml(t('bays.smart', { list: smartText(bay.smart_warnings) }))}</span>` : ''}
                    <div class="ulc-bay-actions">
                        ${bay.smart_warnings ? `<button type="button" class="ulc-btn small" data-smart-ack="${escapeHtml(bay.led)}" title="${escapeHtml(t('smart.ack_title'))}">${icon('check')}${escapeHtml(t('smart.ack'))}</button>` : ''}
                        ${identifyButton(bay.led)}
                    </div>
                </div>`;
            })
            .join('');

        container.querySelectorAll<HTMLElement>('.ulc-led[data-led]').forEach(element => paintLed(element, element.dataset.led ?? ''));

        renderCaseInfo();
        renderLedSubtitles();
    }

    // Under a bay's name in the LED tables: the Unraid disk in it, not the
    // controller's internal LED name (disk1..disk8), which reads like a slot.
    function renderLedSubtitles() {
        qa('[data-led-sub]').forEach(element => {
            const bay = state.bayInfo.find(b => b.led === element.dataset.ledSub);

            // In status mode a spun-down disk's LED differs from its setting.
            const standby =
                bay && bay.device && bay.standby && statusMode()
                    ? tRaw(`led.standby_${state.status.standby_mode || 'dim'}`, { pct: state.status.standby_level ?? 30 })
                    : '';

            // In status mode an error or SMART warning wins over the colour set here.
            const override =
                bay && bay.device && statusMode()
                    ? bay.error && state.status.error_enabled !== false
                        ? t('led.error_override')
                        : bay.smart_warnings && state.status.smart_enabled !== false
                          ? t('led.smart_override')
                          : ''
                    : '';

            element.textContent = !bay ? '' : !bay.device ? t('bays.no_disk') : [bay.slot, `/dev/${bay.device}`, standby, override].filter(Boolean).join(' · ');
        });
    }

    function renderCaseInfo() {
        qa('[data-bay-info]').forEach(element => {
            const bay = state.bayInfo.find(b => b.led === element.dataset.bayInfo);
            let text = '';
            let kind = '';

            if (bay && bay.device) {
                if (bay.error) {
                    text = t('case.error');
                    kind = 'error';
                } else if (bay.smart_warnings) {
                    text = 'SMART';
                    kind = 'warning';
                } else if (bay.standby) {
                    text = t('bays.standby');
                    kind = 'standby';
                } else {
                    text = bay.temp != null ? `${bay.temp}°` : 'OK';
                }
            }

            element.textContent = text;
            element.className = `ulc-bay-info ${kind}`;
            element.title =
                bay && bay.device
                    ? [t('led.bay', { n: bay.bay }), bay.status ? tRaw(`disk.${bay.status}`) : '', bayCondition(bay)].filter(Boolean).join(' · ')
                    : '';
        });
    }

    function bayCondition(bay: LedBay): string {
        const parts = [bay.standby ? t('bays.standby') : bay.temp != null ? `${bay.temp} °C` : ''];
        if (bay.errors > 0) parts.push(t('bays.errors', { n: bay.errors }));
        return parts.filter(Boolean).join(' · ');
    }

    // "5=8 errors=2" -> "8 umgelagerte Sektoren, 2 Lese-/Schreibfehler"
    function smartText(warnings: string): string {
        return warnings
            .split(' ')
            .map(pair => {
                const [name, value] = pair.split('=');
                return tRaw(`smart.${name}`, { n: value });
            })
            .join(', ');
    }

    function ackSmart(led: string): Promise<void> {
        return run(() => post('smart_ack', { led }), t('smart.acked', { n: led.slice(4) }));
    }

    function renderDisplay() {
        const status = state.status;
        const sync = q('#ulc-sync-state');

        sync.hidden = !status.sync_action;
        if (status.sync_action) {
            sync.textContent = tRaw(`sync.${status.sync_action}`, { percent: status.sync_percent });
        }

        const card = q('#ulc-display-card');
        if (card.contains(document.activeElement)) return;

        q<HTMLInputElement>('#ulc-sync-enabled').checked = status.sync_enabled !== false;

        setColorInput(ensureColor('#ulc-sync-color', '#0078ff'), rgbToHex(status.sync_color || '0 120 255'));
        setColorInput(ensureColor('#ulc-error-color', '#ff3b30'), rgbToHex(status.disk_error_color || '255 59 48'));

        q<HTMLSelectElement>('#ulc-standby-mode').value = status.standby_mode || 'dim';
        q<HTMLSelectElement>('#ulc-disk-color-mode').value = status.disk_color_mode || 'own';
        q<HTMLInputElement>('#ulc-smart-enabled').checked = status.smart_enabled !== false;
        q<HTMLInputElement>('#ulc-error-enabled').checked = status.error_enabled !== false;
        q<HTMLInputElement>('#ulc-activity-enabled').checked = status.activity_enabled !== false;

        setColorInput(ensureColor('#ulc-smart-color', '#ff8c00'), rgbToHex(status.smart_color || '255 140 0'));

        const level = q<HTMLInputElement>('#ulc-standby-level');
        level.value = String(status.standby_level || 30);
        setRangeFill(level);
        q('#ulc-standby-level-value').textContent = `${level.value}%`;
        level.closest('.ulc-inline')?.classList.toggle('disabled', q<HTMLSelectElement>('#ulc-standby-mode').value !== 'dim');
    }

    function renderShutdown() {
        const status = state.status;

        if (q('#ulc-shutdown-card').contains(document.activeElement)) return;

        q<HTMLInputElement>('#ulc-shutdown-enabled').checked = Boolean(status.shutdown_blink);
        q<HTMLSelectElement>('#ulc-shutdown-effect').value = status.shutdown_effect || 'blink';

        setColorInput(ensureColor('#ulc-shutdown-color', '#ff8c00'), rgbToHex(status.shutdown_color || '255 140 0'));
    }

    function saveShutdown() {
        const hex = normalizeHex(q<HTMLInputElement>('#ulc-shutdown-color input[type="text"]').value);

        if (!hex) {
            toast(t('colors.invalid'), 'error');
            return;
        }

        return run(
            () =>
                post('shutdown', {
                    enabled: q<HTMLInputElement>('#ulc-shutdown-enabled').checked ? '1' : '0',
                    effect: q<HTMLSelectElement>('#ulc-shutdown-effect').value,
                    ...hexToRgb(hex)
                }),
            t('shutdown.saved')
        );
    }

    function renderNetwork() {
        const status = state.status;
        const current = q('#ulc-speed-current');
        const speedOn = Boolean(status.netdev_speed_enabled);

        const linkNow = status.netdev_speed ?? 0;
        current.textContent = linkNow > 0 ? t('display.speed_current', { speed: speedLabel(linkNow) }) : t('display.speed_unknown');

        if (q('#ulc-network-box').contains(document.activeElement)) return;

        q<HTMLInputElement>('#ulc-speed-enabled').checked = speedOn;
        q('#ulc-speed-colors').hidden = !speedOn;
        current.hidden = !speedOn;

        LINK_SPEEDS.forEach(speed => {
            const saved = (status.netdev_speed_colors || {})[speed];
            setColorInput(q(`[data-speed="${speed}"]`), saved ? rgbToHex(saved) : (SPEED_DEFAULTS[speed] ?? ''));
        });
    }

    function saveNetwork() {
        const speeds: Record<`speed_${number}`, string> = {};

        for (const speed of LINK_SPEEDS) {
            const speedHex = normalizeHex(q<HTMLInputElement>(`[data-speed="${speed}"] input[type="text"]`).value);

            if (!speedHex) {
                toast(t('colors.invalid'), 'error');
                return;
            }

            const c = hexToRgb(speedHex);
            speeds[`speed_${speed}`] = `${c.r} ${c.g} ${c.b}`;
        }

        return run(
            () => post('netdev_speed', { speed_enabled: q<HTMLInputElement>('#ulc-speed-enabled').checked ? '1' : '0', ...speeds }),
            t('display.netdev_saved')
        );
    }

    function saveDisplay() {
        const hex = normalizeHex(q<HTMLInputElement>('#ulc-sync-color input[type="text"]').value);
        const smartHex = normalizeHex(q<HTMLInputElement>('#ulc-smart-color input[type="text"]').value);

        if (!hex || !smartHex) {
            toast(t('colors.invalid'), 'error');
            return;
        }

        const smart = hexToRgb(smartHex);

        return run(
            () =>
                post('status_display', {
                    disk_color_mode: q<HTMLSelectElement>('#ulc-disk-color-mode').value,
                    smart_enabled: q<HTMLInputElement>('#ulc-smart-enabled').checked ? '1' : '0',
                    error_enabled: q<HTMLInputElement>('#ulc-error-enabled').checked ? '1' : '0',
                    activity_enabled: q<HTMLInputElement>('#ulc-activity-enabled').checked ? '1' : '0',
                    smart_r: smart.r,
                    smart_g: smart.g,
                    smart_b: smart.b,
                    sync_enabled: q<HTMLInputElement>('#ulc-sync-enabled').checked ? '1' : '0',
                    standby_mode: q<HTMLSelectElement>('#ulc-standby-mode').value,
                    standby_level: q<HTMLInputElement>('#ulc-standby-level').value,
                    ...hexToRgb(hex)
                }),
            t('display.saved')
        );
    }

    const identifying = (led: string): boolean => (state.status.identify || []).includes(led);

    function identifyButton(led: string): string {
        const active = identifying(led);

        return `<button type="button" class="ulc-btn small ${active ? 'primary' : ''}" data-identify="${escapeHtml(led)}" title="${escapeHtml(t('identify.title'))}">
            ${icon(active ? 'reset' : 'bulb')}${escapeHtml(t(active ? 'identify.stop' : 'identify.button'))}
        </button>`;
    }

    // The LED blinks white for 30 s; afterwards the backend restores it on its own.
    async function identify(led: string): Promise<void> {
        const stop = identifying(led);

        await run(() => post('identify', { led, value: stop ? 'stop' : 'start' }), stop ? t('identify.stopped') : t('identify.started', { n: led.slice(4) }));

        clearTimeout(state.identifyTimer);
        if (!stop) state.identifyTimer = setTimeout(() => window.UNCPoll.awake() && loadStatus(), 31000);
    }

    function renderAlerts() {
        const status = state.status;
        const card = q('#ulc-alert-card');

        q('#ulc-alert-state').textContent = !status.alert_enabled
            ? t('alerts.state_off')
            : (status.alert_count ?? 0) > 0
              ? t('alerts.state_active', { n: status.alert_count })
              : t('alerts.state_none');

        if (card.contains(document.activeElement)) return;

        q<HTMLInputElement>('#ulc-alert-enabled').checked = Boolean(status.alert_enabled);
        q<HTMLSelectElement>('#ulc-alert-level').value = status.alert_level || 'warning';

        setColorInput(ensureColor('#ulc-alert-color', '#ff3b30'), rgbToHex(status.alert_color || '255 59 48'));
    }

    function saveAlerts() {
        const hex = normalizeHex(q<HTMLInputElement>('#ulc-alert-color input[type="text"]').value);

        if (!hex) {
            toast(t('colors.invalid'), 'error');
            return;
        }

        return run(
            () =>
                post('alerts', {
                    enabled: q<HTMLInputElement>('#ulc-alert-enabled').checked ? '1' : '0',
                    level: q<HTMLSelectElement>('#ulc-alert-level').value,
                    ...hexToRgb(hex)
                }),
            t('alerts.saved')
        );
    }

    async function setMode(mode: 'status' | 'manual'): Promise<void> {
        // Show it at once; the re-read after the write confirms it (or puts the old value back on a failure).
        state.status.mode = mode;
        renderMode();

        await run(() => post('mode', { value: mode }), mode === 'status' ? t('mode.status_on') : t('mode.manual_on'));
    }

    function applyErrorColor(hex: string): Promise<void> | undefined {
        const value = normalizeHex(hex);

        if (!value) {
            toast(t('colors.invalid'), 'error');
            render();
            return;
        }

        state.status.disk_error_color = Object.values(hexToRgb(value)).join(' ');

        return run(() => post('error_color', { ...hexToRgb(value) }), t('colors.error_saved'));
    }

    function preview(leds: string[], change: Partial<UiLed>): void {
        for (const led of leds) {
            if (!state.leds[led]) continue;
            Object.assign(state.leds[led], change);
            // A new effect starts now: the old command's phase no longer applies.
            if ('effect' in change) Object.assign(state.leds[led], { sent_ms: undefined, now_ms: undefined, rx: undefined });
        }

        qa('.ulc-led[data-led]').forEach(element => paintLed(element, element.dataset.led ?? ''));
    }

    function applyColor(leds: string[], hex: string): Promise<void> | undefined {
        const value = normalizeHex(hex);

        if (!value) {
            toast(t('colors.invalid'), 'error');
            render();
            return;
        }

        preview(leds, { color: value });

        return run(() => write(leds, 'color', hexToRgb(value)), t('colors.applied'));
    }

    const timing = (effect: EffectId): [number, number] | undefined => {
        const speed = SPEEDS[state.speed];
        return effect === 'breath' ? speed?.breath : effect === 'blink' ? speed?.blink : undefined;
    };

    async function setEffect(leds: string[], effect: EffectId): Promise<void> {
        const dark = leds.filter(led => (state.leds[led]?.brightness || 0) === 0);

        if (effect === 'off') {
            // Remember each LED's own level (the saved one in status/night mode), so a later effect lights it as it was.
            leds.forEach(led => {
                const level = dayLevel(led);
                if (level) state.lastLevel[led] = level;
            });
            await write(leds, 'off');
            return;
        }

        // breath/blink and "static" need a lit LED to be visible: back to the level before "Off", else
        // that of a lit LED, else the default. Every LED gets its own level, LEDs with the same level one write.
        const fallback = Math.max(0, ...state.order.map(led => state.leds[led]?.brightness || 0)) || levelFromPercent(DEFAULTS.brightness);
        const byLevel = new Map<number, string[]>();
        dark.forEach(led => {
            const value = state.lastLevel[led] || fallback;
            byLevel.set(value, [...(byLevel.get(value) || []), led]);
        });
        for (const [value, group] of byLevel) await write(group, 'brightness', { value });

        if (effect === 'static') {
            await write(leds, 'stop');
        } else {
            const pair = timing(effect);
            if (effect !== 'breath' && effect !== 'blink') return;
            const [on, off] = pair ?? [0, 0];
            await write(leds, effect, { on, off });
        }
    }

    function applySoftwareEffect(leds: string[], effect: EffectId, name: string): Promise<void> {
        return run(
            () =>
                post('fx', {
                    value: effect,
                    led: leds.join(','),
                    speed: state.speed,
                    reverse: q<HTMLSelectElement>('#ulc-direction').value === 'backward' ? '1' : '0',
                    idle: q<HTMLInputElement>('#ulc-idle').checked ? '1' : '0'
                }),
            t('effect.applied', { name })
        );
    }

    function applyEffect(leds: string[], effect: EffectId): Promise<void> {
        const name = effectName(effect);

        if (EFFECTS.find(e => e.id === effect)?.software) {
            return applySoftwareEffect(leds, effect, name);
        }

        const [on, off] = timing(effect) || [0, 0];

        preview(leds, effect === 'off' ? { brightness: 0 } : { effect: effect === 'static' ? 'none' : effect, on_ms: on, off_ms: off });

        return run(() => setEffect(leds, effect), t('effect.applied', { name }));
    }

    function applyBrightness(leds: string[], percent: number): Promise<void> {
        const value = levelFromPercent(percent);

        preview(leds, { brightness: value });

        return run(() => write(leds, 'brightness', { value }), t('brightness.applied', { percent }));
    }

    async function testLeds() {
        const button = q<HTMLButtonElement>('#ulc-test');
        const before: Record<string, UiLed> = JSON.parse(JSON.stringify(state.leds));
        const leds = state.order.filter(led => (before[led]?.brightness ?? 0) > 0);

        if (!leds.length) {
            toast(t('test.all_off'), 'error');
            return;
        }

        button.disabled = true;

        await run(async () => {
            await write(leds, 'blink', { on: 150, off: 150 });
            preview(leds, { effect: 'blink', on_ms: 150, off_ms: 150 });
            await sleep(3000);

            // Put every LED back to the effect it had before.
            const groups: Record<string, string[]> = { none: [], breath: [], blink: [] };
            leds.forEach(led => (groups[before[led]?.effect ?? ''] || groups.none)?.push(led));

            await write(groups.none ?? [], 'stop');

            for (const effect of ['breath', 'blink'] as const) {
                const [on, off] = timing(effect) ?? [0, 0];
                await write(groups[effect] ?? [], effect, { on, off });
            }
        }, t('test.done'));

        button.disabled = false;
    }

    function resetDefaults() {
        if (!window.confirm(t('reset.confirm'))) {
            return;
        }

        return run(async () => {
            const colors: [string[], string][] = [
                [['power'], DEFAULTS.power],
                [['netdev', 'netdev2'], DEFAULTS.netdev],
                [disks(), DEFAULTS.disk]
            ];

            for (const [leds, hex] of colors) {
                await write(
                    leds.filter(led => state.order.includes(led)),
                    'color',
                    hexToRgb(hex)
                );
            }

            await write(state.order, 'stop');
            await write(state.order, 'brightness', { value: levelFromPercent(DEFAULTS.brightness) });
        }, t('reset.done'));
    }

    function showView(view: string): void {
        qa('.ulc-view').forEach(section => section.classList.toggle('active', section.dataset.view === view));
    }

    const onScreen = () => root.getClientRects().length > 0;

    const goto = (nav: string): void => (window.UNC_NAV ? window.UNC_NAV(nav) : showView(nav.split(':')[1] ?? ''));

    // Saves a settings card on every committed change; changes made while a save
    // is running are saved right after it.
    function autoSave(card: HTMLElement, save: () => unknown): void {
        let running: unknown = null;
        let again = false;

        card.addEventListener('change', async () => {
            if (running) {
                again = true;
                return;
            }
            do {
                again = false;
                running = save();
                await running;
            } while (again);
            running = null;
        });
    }

    function bindColorInputs(container: HTMLElement, applyFor: (input: HTMLInputElement, value: string) => unknown): void {
        container.addEventListener('input', event => {
            const input = event.target;
            if (!(input instanceof HTMLInputElement)) return;
            const box = input.closest<HTMLElement>('.ulc-color-input');
            if (!box) return;

            if (input.type === 'color') {
                q('.ulc-swatch', box).style.setProperty('--c', input.value);
                q<HTMLInputElement>('input[type="text"]', box).value = input.value.toUpperCase();
                return;
            }

            // A typed hex value previews as soon as it is complete (GitHub issue #30).
            const hex = input.type === 'text' ? normalizeHex(input.value) : null;
            if (hex) {
                q('.ulc-swatch', box).style.setProperty('--c', hex);
                q<HTMLInputElement>('input[type="color"]', box).value = hex;
            }
        });

        container.addEventListener('change', event => {
            const input = event.target;
            if (!(input instanceof HTMLInputElement)) return;

            if (input.type !== 'color' && input.type !== 'text') return;
            if (!input.closest('.ulc-color-input')) return;

            applyFor(input, input.value);
        });
    }

    function bindEvents(): void {
        qa('[data-ulc-icon]').forEach(element => {
            element.innerHTML = iconOf(element.dataset.ulcIcon);
        });

        const modeSelect = q<HTMLSelectElement>('#ulc-mode');
        modeSelect.addEventListener('change', () => setMode(modeSelect.value === 'status' ? 'status' : 'manual'));

        root.addEventListener('click', event => {
            const act = hit(event)?.closest<HTMLElement>('[data-act]')?.dataset.act;

            if (act === 'mode-manual') setMode('manual');
            if (act === 'mode-status') setMode('status');
        });

        q('#ulc-drive-status-link').addEventListener('click', () => goto('led:drives'));

        bindColorInputs(q('#ulc-alert-color'), () => {});
        bindColorInputs(q('#ulc-sync-color'), () => {});
        bindColorInputs(q('#ulc-smart-color'), () => {});
        bindColorInputs(q('#ulc-error-color'), (_input, value) => applyErrorColor(value));
        const speedColors = q('#ulc-speed-colors');
        speedColors.innerHTML = LINK_SPEEDS.map(
            speed => `
            <div class="ulc-speed-item">
                <label>${escapeHtml(t('display.speed_color', { speed: speedLabel(speed) }))}</label>
                <div data-speed="${speed}">${colorInput(SPEED_DEFAULTS[speed] ?? '', false, t('display.speed_color', { speed: speedLabel(speed) }))}</div>
            </div>`
        ).join('');
        bindColorInputs(speedColors, () => {});
        const speedEnabled = q<HTMLInputElement>('#ulc-speed-enabled');
        speedEnabled.addEventListener('change', () => {
            speedColors.hidden = !speedEnabled.checked;
            q('#ulc-speed-current').hidden = !speedEnabled.checked;
        });

        autoSave(q('#ulc-display-card'), saveDisplay);
        autoSave(q('#ulc-network-box'), saveNetwork);

        bindColorInputs(q('#ulc-shutdown-color'), () => {});
        autoSave(q('#ulc-shutdown-card'), saveShutdown);

        q('#ulc-shutdown-test').addEventListener('click', async () => {
            await saveShutdown();
            run(() => post('shutdown_test', {}), t('shutdown.test_started'));
        });
        autoSave(q('#ulc-alert-card'), saveAlerts);
        autoSave(q('#ulc-night-card'), saveSchedule);

        const standbyLevel = q<HTMLInputElement>('#ulc-standby-level');
        standbyLevel.addEventListener('input', () => {
            setRangeFill(standbyLevel);
            q('#ulc-standby-level-value').textContent = `${standbyLevel.value}%`;
        });
        const netdevStatus = q<HTMLSelectElement>('#ulc-netdev-status');
        netdevStatus.addEventListener('change', () => run(() => post('netdev_status', { value: netdevStatus.value }), t('display.netdev_saved')));

        const standbyMode = q<HTMLSelectElement>('#ulc-standby-mode');
        standbyMode.addEventListener('change', () => {
            standbyLevel.closest('.ulc-inline')?.classList.toggle('disabled', standbyMode.value !== 'dim');
        });

        q('#ulc-bays').addEventListener('click', event => {
            const button = hit(event)?.closest<HTMLElement>('[data-identify]');
            if (button) identify(button.dataset.identify ?? '');

            const ack = hit(event)?.closest<HTMLElement>('[data-smart-ack]');
            if (ack) ackSmart(ack.dataset.smartAck ?? '');
        });

        const nightBrightness = q<HTMLInputElement>('#ulc-night-brightness');

        nightBrightness.addEventListener('input', () => {
            setRangeFill(nightBrightness);
            q('#ulc-night-brightness-value').textContent = `${nightBrightness.value}%`;
        });

        q<HTMLSelectElement>('#ulc-night-action').addEventListener('change', renderNight);

        for (const group of GROUPS) {
            q(`[data-effects="${group}"]`).addEventListener('click', event => {
                const button = hit(event)?.closest<HTMLButtonElement>('[data-effect]');
                if (!button || button.disabled) return;

                applyEffect(groupLeds(group), button.dataset.effect as EffectId);
            });
        }

        const systemTarget = q<HTMLSelectElement>('#ulc-system-target');
        systemTarget.addEventListener('change', () => {
            state.sysTarget = systemTarget.value;
            renderEffects();
        });

        // Speed, direction and idle-only are shared by all LEDs. The running effect process
        // (rainbow/temperature) keeps its LEDs; breathe and pulse are set per LED.
        const reapplyEffects = (kinds: EffectId[]): void => {
            if (state.status.fx && kinds.includes(state.status.fx as EffectId)) {
                applyEffect(fxLeds(), state.status.fx as EffectId);
            }

            for (const effect of ['breath', 'blink'] as const) {
                const leds = kinds.includes(effect) ? state.order.filter(led => !managed(led) && currentEffect([led]) === effect) : [];

                if (leds.length) applyEffect(leds, effect);
            }
        };

        const speed = q<HTMLInputElement>('#ulc-speed');

        speed.addEventListener('input', () => {
            setRangeFill(speed);
            q('#ulc-speed-value').textContent = t(SPEEDS[Number(speed.value)]?.label ?? 'speed.0');
        });

        speed.addEventListener('change', () => {
            state.speed = Number(speed.value);

            try {
                localStorage.setItem('ulc-speed', speed.value);
            } catch (error) {
                // Storage may be blocked; the speed then just isn't remembered.
            }

            reapplyEffects(['breath', 'blink', 'rainbow']);
        });

        const reapplySoftwareEffect = () => reapplyEffects(['rainbow', 'temperature']);

        q<HTMLSelectElement>('#ulc-direction').addEventListener('change', reapplySoftwareEffect);
        q<HTMLInputElement>('#ulc-idle').addEventListener('change', reapplySoftwareEffect);

        q('#ulc-test').addEventListener('click', testLeds);
        q('#ulc-reset').addEventListener('click', resetDefaults);

        const rowKey = (element: HTMLElement): string => element.closest<HTMLElement>('[data-led-row]')?.dataset.ledRow ?? '';

        for (const table of [q('#ulc-system-table'), q('#ulc-drives-table'), q('#ulc-bay-table')]) {
            bindColorInputs(table, (input, value) => applyColor(rowLeds(rowKey(input)), value));

            table.addEventListener('click', event => {
                const button = hit(event)?.closest<HTMLElement>('[data-reset]');
                if (button) applyColor(rowLeds(rowKey(button)), rowDefault(rowKey(button)));
            });

            table.addEventListener('input', event => {
                const target = event.target;
                if (!(target instanceof HTMLInputElement) || !target.matches('[data-brightness]')) return;

                setRangeFill(target);
                const inline = target.closest('.ulc-inline');
                if (inline) q('[data-brightness-value]', inline).textContent = `${target.value}%`;
            });

            table.addEventListener('change', event => {
                const target = event.target;
                if (!(target instanceof HTMLInputElement || target instanceof HTMLSelectElement)) return;
                const row = target.closest<HTMLElement>('[data-led-row]');
                if (!row) return;

                const leds = rowLeds(row.dataset.ledRow ?? '');

                if (target.matches('[data-brightness]')) {
                    applyBrightness(leds, Number(target.value));
                } else if (target.matches('[data-led-effect]')) {
                    applyEffect(leds, target.value as EffectId);
                }
            });
        }
    }

    function translatePage(): void {
        qa('[data-i18n]').forEach(element => {
            element.textContent = tRaw(element.dataset.i18n ?? '');
        });
        qa('[data-i18n-html]').forEach(element => {
            element.innerHTML = tRaw(element.dataset.i18nHtml ?? '');
        });
    }

    async function init() {
        translatePage();

        try {
            const saved = localStorage.getItem('ulc-speed');
            if (saved !== null && SPEEDS[Number(saved)]) state.speed = Number(saved);
        } catch (error) {
            // No storage: keep the default speed.
        }

        const speed = q<HTMLInputElement>('#ulc-speed');
        speed.value = String(state.speed);
        q('#ulc-speed-value').textContent = t(SPEEDS[state.speed]?.label ?? 'speed.0');

        qa<HTMLInputElement>('input[type="range"]').forEach(input => setRangeFill(input));

        buildEffects();
        bindEvents();

        await loadStatus(15000);
        await refresh(5000);
        await loadSchedule();

        // led/all drives the preview (blink and breathe phases come from sent_ms/now_ms), so a slow poll is enough.
        window.UNCPoll.every(() => (state.busy || state.dragging ? null : refresh(ALL_POLL_MS * 0.8)), ALL_POLL_MS, onScreen);
        window.UNCPoll.every(() => (state.busy ? null : loadStatus(STATUS_POLL_MS * 0.9)), STATUS_POLL_MS, onScreen);

        // Switching to an LED page: fresh preview now, status unless it is only seconds old.
        shown = () => {
            if (state.busy || state.dragging) return;
            refresh(3000);
            loadStatus(15000);
        };
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }

    let shown = () => {};

    window.ULC = {
        show: view => {
            showView(view);
            shown();
        }
    };
}

const root = document.getElementById('ulc');

if (root) main(root);
