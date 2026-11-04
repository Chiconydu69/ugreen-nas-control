// Dashboard tile, drawn like the plugin's Overview: the NAS front with its bay LEDs, a card per fan, the
// temperatures, the SSDs and links into the plugin pages. Colours come from Unraid's dashboard (app.css, .unc-dash).
// The tile header carries the essentials (mode, fan speeds, hottest temperature, alerts), so they stay visible
// when Unraid collapses the tile.
import { createClient } from '../core/api';
import type { Fan, HardwareResponse, LedState, LedStatus, NasGet, NasPost, Nvme, StatusResponse } from '../core/contract';
import { fanIcon } from '../core/fanspin';
import { type TempKind, deg, tempClass } from '../core/format';
import { isStatusResponse } from '../core/guard';
import { escapeHtml } from '../core/html';
import { luminance } from '../core/theme';
import { miniFrontHtml } from '../features/nas/components/front';
import { fanName } from '../features/nas/names';
import { t, tRaw } from '../features/nas/text';

const API = '/plugins/ugreen-nas-control/api.php';
const PAGE = '/Settings/UGREENNASControl';
const POLL_MS = 15000;

const api = createClient<NasGet, NasPost>({ base: API, csrf: () => '', reads: 'direct', checks: { status: isStatusResponse } });

const ICONS = {
    fan: '<circle cx="12" cy="12" r="2"/><path d="M12 10c0-4 1-7 4-7 2 0 3 2 2 4l-4 3.5M14 12c4 0 7 1 7 4 0 2-2 3-4 2l-3.5-4M12 14c0 4-1 7-4 7-2 0-3-2-2-4l4-3.5M10 12c-4 0-7-1-7-4 0-2 2-3 4-2l3.5 4"/>',
    chip: '<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>',
    m2: '<rect x="2" y="8" width="20" height="8" rx="1.5"/><path d="M6 10.5h4v3H6zM12 10.5h3v3h-3z"/><circle cx="19" cy="12" r="1.2"/>',
    drives: '<rect x="3" y="4" width="18" height="6" rx="1.5"/><rect x="3" y="14" width="18" height="6" rx="1.5"/><path d="M7 7h.01M7 17h.01"/>',
    home: '<path d="m3 10 9-7 9 7v10a2 2 0 0 1-2 2h-4v-7H9v7H5a2 2 0 0 1-2-2z"/>',
    curve: '<path d="M3 3v18h18"/><path d="M7 16c3 0 4-8 7-8s3 4 6 4"/>',
    box: '<path d="M21 8 12 3 3 8v8l9 5 9-5z"/><path d="m3 8 9 5 9-5M12 13v8"/>',
    bulb: '<path d="M9 18h6M10 22h4"/><path d="M12 2a7 7 0 0 0-4 12.7c.6.5 1 1.3 1 2.3h6c0-1 .4-1.8 1-2.3A7 7 0 0 0 12 2z"/>',
    alert: '<path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4M12 17h.01"/>'
};
const icon = (name: keyof typeof ICONS): string => `<svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[name]}</svg>`;
const glyph = (markup: string): string => `<span class="unc-glyph">${markup}</span>`;
const UNRAID =
    '<svg viewBox="0 0 24 24" aria-hidden="true" class="unc-mark"><rect width="24" height="24" rx="6" fill="#f15a2c" stroke="none"/><path d="M7.5 6.5v6.2a4.5 4.5 0 0 0 9 0V6.5" fill="none" stroke="#fff" stroke-width="2.4" stroke-linecap="round"/></svg>';

type Row = [label: string, value: number | null, kind: TempKind];
interface LedRead {
    leds: Record<string, LedState>;
    status: LedStatus & { bays?: number | string; mode?: string };
}

const hottest = (list: (number | null)[]): number | null => list.reduce<number | null>((max, v) => (v != null && (max == null || v > max) ? v : max), null);

// The tile is hit by a 15 s poll: a part is only replaced when its markup changed.
function set(el: HTMLElement | null, html: string): void {
    if (!el || el.dataset.h === html) return;
    el.dataset.h = html;
    el.innerHTML = html;
}

function temperatures(data: StatusResponse): Row[] {
    const s = data.sensors || {};
    const rows: Row[] = [[t('temps.cpu'), s.cpu ?? null, 'cpu']];
    const disk = hottest((s.disks || []).filter(d => d.hdd).map(d => d.temp));
    const nvme = hottest((s.nvme || []).map(n => n.temp));

    if (disk != null) rows.push([t('dash.disk'), disk, 'disk']);
    if (nvme != null) rows.push([t('dash.nvme'), nvme, 'nvme']);
    return rows;
}

function fanSource(fan: Fan, data: StatusResponse): string {
    const daemon = data.running ? data.daemon : null;
    const mode = data.settings?.mode;

    if (!daemon) return '';
    if (daemon.failsafe) return t('warn.failsafe');
    if (mode === 'bios' || mode === 'full') return t(`mode.${mode}`);
    if (fan.role === 'cpu' || daemon.case_driver === 'cpu') return t('ov.follows_cpu');
    if (daemon.case_from === 'disk') return t('ov.follows_disk');
    if (daemon.case_from === 'nvme') return t('ov.follows_nvme', { name: t('dash.nvme') });
    return t('ov.follows_none');
}

function stalledNames(data: StatusResponse): string[] {
    const fans = data.fans || [];

    return ((data.running ? data.daemon?.stalled : null) || [])
        .map(id => fans.find(f => f.id === id))
        .filter((f): f is Fan => Boolean(f))
        .map(f => fanName(f, fans));
}

function alertTexts(data: StatusResponse): string[] {
    const daemon = data.running ? data.daemon : null;
    const stalled = stalledNames(data);

    return [
        daemon?.failsafe ? `${t('warn.failsafe')} ${tRaw(`failsafe.${daemon.failsafe}`)}` : '',
        stalled.length ? t('stalled.text', { list: stalled.join(', ') }) : ''
    ].filter(Boolean);
}

function fanCard(fan: Fan, fans: Fan[], data: StatusResponse): string {
    const running = fan.rpm > 0 && !fan.error;
    const rpm =
        fan.rpm > 0 ? `${Math.round(Number(fan.rpm))} <small>${escapeHtml(t('fan.rpm'))}</small>` : escapeHtml(t(fan.error ? 'fan.error' : 'fan.stopped'));
    const power = fan.error ? '' : fan.pct == null ? t('fan.auto') : t('fan.speed', { pct: fan.pct });

    return `
        <div class="unc-fantile ${running ? '' : 'stopped'}">
            ${fanIcon(fan, icon('fan'))}
            <div class="unc-fantile-text"><strong>${escapeHtml(fanName(fan, fans))}</strong><span>${escapeHtml(fanSource(fan, data))}</span></div>
            <div class="unc-fantile-val"><b>${rpm}</b><span>${escapeHtml(power)}</span></div>
            <span class="unc-meter"><span style="width:${fan.pct ?? 0}%"></span></span>
        </div>`;
}

function heatStat(label: string, value: number | null, kind: TempKind, ico: keyof typeof ICONS): string {
    return `<span class="unc-stat is-stack is-lg"><small class="unc-muted">${glyph(icon(ico))}${escapeHtml(label)}</small><b class="unc-t ${tempClass(value, kind)}">${value == null ? '–' : deg(value)}</b></span>`;
}

function paintStatus(root: HTMLElement, data: StatusResponse): void {
    const fans = data.fans || [];
    const rows = temperatures(data);
    const alerts = alertTexts(data);
    const rowIcon: Record<TempKind, keyof typeof ICONS> = { cpu: 'chip', disk: 'drives', nvme: 'm2', other: 'chip' };

    const alert = root.querySelector<HTMLElement>('[data-alert]');
    if (alert) {
        alert.hidden = !alerts.length;
        set(
            alert,
            alerts.length
                ? `<span class="unc-ico">${icon('alert')}</span><div>${alerts.map(text => `<div>${escapeHtml(text)}</div>`).join('')}<a href="${PAGE}" data-nav="safety">${escapeHtml(t('dash.settings'))}</a></div>`
                : ''
        );
    }

    const fansBox = root.querySelector<HTMLElement>('[data-fans]');
    if (fansBox) fansBox.hidden = !fans.length;
    set(fansBox, fans.map(fan => fanCard(fan, fans, data)).join(''));
    set(root.querySelector('[data-temps]'), rows.map(([label, value, kind]) => heatStat(label, value, kind, rowIcon[kind])).join(''));
}

const slotName = (ssd: Nvme): string =>
    ssd.slot === 'pcie'
        ? t('in.pcie_card')
        : ssd.slot === 'system'
          ? t('slot.system')
          : ssd.slot === 'm2'
            ? 'M.2'
            : t('case.m2_slot', { n: ssd.slot.replace('m2-', '') });

function ssdChips(ssds: Nvme[]): string {
    return ssds
        .filter(s => s.temp != null)
        .map(
            s =>
                `<span class="unc-stat" title="${escapeHtml([s.model, s.id].filter(Boolean).join(' · '))}"><small class="unc-muted">${glyph(s.boot ? UNRAID : icon('m2'))}${escapeHtml(slotName(s))}</small><b class="unc-t ${tempClass(s.temp, 'nvme')}">${deg(s.temp)}</b></span>`
        )
        .join('');
}

async function fetchLeds(root: HTMLElement): Promise<LedRead | null> {
    const api = root.dataset.ledApi;
    if (!api) return null;

    try {
        // led/status costs the NAS over a second of CPU: once a minute. led/all draws the bars.
        const [all, status] = await Promise.all([
            window.UNCPoll.json<{ ok?: boolean; leds?: Record<string, LedState> }>(`${api}?action=all`, POLL_MS * 0.8).then(data => ({
                ...data,
                leds: window.UNCLedFx.stamp(data.leds)
            })),
            window.UNCPoll.json<LedRead['status']>(`${api}?action=status`, 55000)
        ]);
        return all.ok === false || !all.leds ? null : { leds: all.leds, status };
    } catch {
        return null;
    }
}

function linkPill(nav: string, ico: keyof typeof ICONS, text: string, sub = ''): string {
    return `<a class="unc-pill" href="${PAGE}" data-nav="${nav}"><span class="unc-ico">${icon(ico)}</span><span>${escapeHtml(text)}</span>${sub ? `<small>${escapeHtml(sub)}</small>` : ''}</a>`;
}

function paintLinks(root: HTMLElement, data: StatusResponse | null, hw: HardwareResponse | null, led: LedRead | null): void {
    const model = data?.info?.model || hw?.model || '';
    const fanControl = !model || /^(DXP6800|DXP4800.*GT)/.test(model);
    const mode = led?.status?.fx || (led ? t(led.status?.mode === 'status' ? 'led.mode_status' : 'led.mode_manual') : '');
    const links = [
        linkPill('overview', 'home', t('nav.overview')),
        fanControl ? linkPill('curves', 'curve', t('nav.curves')) : '',
        hw?.bays?.length ? linkPill('case', 'box', t('nav.case')) : '',
        led ? linkPill('led:overview', 'bulb', t('led.title'), mode) : ''
    ];

    set(root.querySelector('[data-links]'), links.join(''));
}

// The front is redrawn when the drives change; the LEDs are painted on top every time (UNCLedFx keeps a running animation).
function paintFront(root: HTMLElement, hw: HardwareResponse | null, led: LedRead | null): void {
    const box = root.querySelector<HTMLElement>('[data-front]');
    if (!box) return;

    const bays = hw?.bays || [];
    box.hidden = !hw || !bays.length;
    if (hw && bays.length) set(box, miniFrontHtml({ ...hw, memory: hw.memory || [], nvme: hw.nvme || [], net: hw.net || [] }));

    if (!led) return;
    box.querySelectorAll<HTMLElement>('[data-led]').forEach(el => window.UNCLedFx.paint(el, led.leds[el.dataset.led ?? ''], el.dataset.led, led.status));
}

function paintHeader(data: StatusResponse | null): void {
    const line = document.getElementById('unc-dash-state');
    if (!line) return;

    line.innerHTML = '';

    if (!data) {
        line.textContent = t('head.offline');
        return;
    }

    const part = (text: string, className = ''): void => {
        const el = document.createElement('span');
        el.className = className;
        el.textContent = text;
        line.append(el);
    };
    const rpm = (data.fans || []).map(f => f.rpm || 0);
    const top = temperatures(data).reduce<Row | null>((max, r) => (r[1] != null && (!max || r[1] > (max[1] as number)) ? r : max), null);

    const failsafe = data.running && data.daemon?.failsafe;

    const model = data.info?.model || '';
    const noFan = Boolean(model) && !/^(DXP6800|DXP4800.*GT)/.test(model);

    if (!noFan) part(t(`mode.${data.settings?.mode || 'standard'}`));
    if (failsafe || (data.chip_error && !noFan)) part(` · ${failsafe ? t('warn.failsafe') : t('warn.chip')}`, 'unc-dash-alert');
    else if (stalledNames(data).length) part(` · ${stalledNames(data).join(', ')}: ${t('fan.stopped')}`, 'unc-dash-alert');
    if (rpm.length) part(` · ${rpm.join(' / ')} ${t('fan.rpm')}`, 'unc-dash-sum');
    if (top) part(` · ${top[0]} ${top[1]} °C`, 'unc-dash-sum');
    if (line.firstChild?.textContent?.startsWith(' · ')) line.firstChild.textContent = line.firstChild.textContent.slice(3);
}

let last: StatusResponse | null = null;

async function load(root: HTMLElement): Promise<void> {
    try {
        const data = await api.get('status');

        last = data;
        paintStatus(root, data);
        paintHeader(data);

        // Drives and LEDs are read together and drawn in one go, so the front never loses its LEDs between two reads.
        const [raw, led] = await Promise.all([
            // Static facts (drives, memory, NICs): once a minute.
            window.UNCPoll.json<HardwareResponse>(`${API}?action=hardware`, 55000).catch(() => null),
            fetchLeds(root)
        ]);
        const hw = raw && raw.ok !== false ? raw : null;

        paintFront(root, hw, led);
        set(root.querySelector('[data-nvme]'), ssdChips(hw?.nvme || []));
        root.querySelector<HTMLElement>('[data-nvme]')?.toggleAttribute('hidden', !hw?.nvme?.some(s => s.temp != null));
        paintLinks(root, last, hw, led);
    } catch {
        paintHeader(null);
    }
}

// "Dark" and "Light" give the tile a background of its own in that scheme; "auto" (same as Unraid) draws none and
// follows whatever is behind it: light when the first opaque background above it is light.
function tileTheme(root: HTMLElement, setting?: string): 'light' | 'dark' {
    if (setting === 'light' || setting === 'dark') return setting;
    for (let el: HTMLElement | null = root.parentElement; el; el = el.parentElement) {
        const match = getComputedStyle(el).backgroundColor.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?/);
        if (match && (match[4] === undefined || Number(match[4]) > 0.5)) {
            return luminance(Number(match[1]), Number(match[2]), Number(match[3])) > 140 ? 'light' : 'dark';
        }
    }
    return 'dark';
}

function init(): void {
    const root = document.getElementById('unc-dash');
    if (!root) return;

    const setting = window.UGREEN_NAS_THEME;
    root.dataset.theme = tileTheme(root, setting);
    root.classList.toggle('unc-dash-solid', setting === 'light' || setting === 'dark');

    const settings = document.getElementById('unc-dash-settings');
    if (settings) settings.title = t('dash.settings');

    root.addEventListener('click', event => {
        const nav = (event.target as HTMLElement).closest<HTMLElement>('[data-nav]')?.dataset.nav;
        try {
            if (nav) sessionStorage.setItem('unc-nav', nav);
        } catch {
            /* storage blocked: the page opens on its first view */
        }
    });
    paintLinks(root, null, null, null);

    load(root);
    // Hidden tab: no polling at all (UNCPoll). The dashboard is often left open on a screen, so no idle pause here.
    window.UNCPoll.setIdle(Number.POSITIVE_INFINITY);
    window.UNCPoll.every(() => load(root), POLL_MS);
}

if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
} else {
    init();
}
