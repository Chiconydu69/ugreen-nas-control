// Fan history page: per fan the speed, the rpm and the temperatures over the last hour or 24 hours, and which input drove
// the speed at each moment. The data is the fan daemon's RAM (action fan_history); the card asks only while it is on screen.
// Pure shaping: history-model.ts; the SVG: history-chart.ts.
import { errorMessage } from '../../../core/api';
import { MOD, type FanHistoryRange, type FanHistoryResponse } from '../../../core/contract-fan-history';
import { escapeHtml } from '../../../core/html';
import { fanChart, geometry, type ChartText, type Geometry } from './history-chart';
import {
    clock,
    dayClock,
    domain,
    modNames,
    nearest,
    REFRESH_SECONDS,
    sampleIndexes,
    sourceArg,
    sourceKind,
    tracks,
    type FanTrack,
    type SourceKind
} from './history-model';
import { registerFanModule, type FanCtx, type FanModule } from './module';

const RANGES: FanHistoryRange[] = ['1h', '24h'];
const KNOWN_SOURCES = [
    'cpu',
    'disk',
    'disk_avg',
    'disk_max',
    'nvme',
    'gpu',
    'hba',
    'nic',
    'script',
    'idle',
    'missing',
    'hold',
    'emergency',
    'bios',
    'rival',
    'calibration'
];
const KINDS: SourceKind[] = ['cpu', 'disk', 'nvme', 'gpu', 'hba', 'nic', 'script', 'idle', 'emergency', 'off', 'other'];

/** The text of a source: "Hottest drive disk2", "CPU", "Emergency: The CPU reached the emergency limit." */
export function sourceText(ctx: Pick<FanCtx, 'tRaw'>, source: string): string {
    const kind = source.split(':')[0] ?? '';
    const name = sourceArg(source);
    if (kind === 'emergency') return ctx.tRaw('hist.src.emergency', { reason: ctx.tRaw(`failsafe.${name}`) });
    return ctx.tRaw(`hist.src.${KNOWN_SOURCES.includes(kind) ? kind : 'other'}`, { name });
}

/** The few characters that fit into a band. */
function shortText(ctx: Pick<FanCtx, 'tRaw'>, source: string): string {
    const kind = source.split(':')[0] ?? '';
    switch (kind) {
        case 'disk':
        case 'nic':
        case 'script':
            return sourceArg(source);
        case 'disk_avg':
            return 'Ø';
        case 'cpu':
        case 'gpu':
        case 'hba':
            return kind.toUpperCase();
        case 'nvme':
            return 'NVMe';
        case 'disk_max':
            return ctx.tRaw('hist.kind.disk');
        default:
            return ctx.tRaw(`hist.kind.${sourceKind(source)}`);
    }
}

function fanNames(list: FanTrack[], ctx: Pick<FanCtx, 't'>): Map<string, string> {
    const cases = list.filter(f => f.role !== 'cpu');
    const names = new Map<string, string>();
    for (const f of list) {
        if (f.role === 'cpu') names.set(f.id, ctx.t('fan.cpu'));
        else names.set(f.id, cases.length === 1 ? ctx.t('fan.case_one') : ctx.t('fan.case', { n: cases.indexOf(f) + 1 }));
    }
    return names;
}

const last = <T>(list: (T | null)[]): T | null => {
    for (let i = list.length - 1; i >= 0; i--) if (list[i] != null) return list[i] as T;
    return null;
};

const deg = (v: number | null | undefined): string => (v == null ? '–' : `${Math.round(v * 10) / 10} °C`);

function create(): FanModule {
    let tick: (() => void) | undefined;

    return {
        id: 'history',
        host: 'history',
        order: 0,
        visible: status => Boolean(status.fan_caps && status.fan_caps.channels > 0),
        render: ctx => `<article class="unc-card unc-hist" aria-labelledby="unc-hist-title">
            ${ctx.ui.cardHead(ctx.t('hist.title'), {
                ico: 'activity',
                titleId: 'unc-hist-title',
                actions: `<div class="unc-hist-range" role="group" aria-label="${escapeHtml(ctx.t('hist.range'))}">${RANGES.map(
                    r => `<button type="button" data-range="${r}" aria-pressed="${r === '1h'}">${escapeHtml(ctx.tRaw(`hist.range_${r}`))}</button>`
                ).join('')}</div>`
            })}
            <p class="unc-muted unc-hist-note" data-note>${escapeHtml(ctx.t('hist.note'))}</p>
            <div class="unc-hist-legend" data-legend></div>
            <div class="unc-hist-body" data-body></div>
        </article>`,
        bind(ctx) {
            const root = ctx.el;
            const body = root.querySelector<HTMLElement>('[data-body]');
            const legend = root.querySelector<HTMLElement>('[data-legend]');
            const note = root.querySelector<HTMLElement>('[data-note]');
            if (body && legend && note) mount(ctx, root, body, legend, note);
        },
        update: () => tick?.()
    };

    function mount(ctx: FanCtx, root: HTMLElement, body: HTMLElement, legend: HTMLElement, note: HTMLElement): void {
        let range: FanHistoryRange = '1h';
        let data: FanHistoryResponse | null = null;
        let error = '';
        let loadedAt = 0;
        let loading = false;
        let width = 0;
        let tableOpen = false;

        const text = (): ChartText => ({
            speed: ctx.t('hist.speed'),
            rpm: ctx.t('fan.rpm'),
            temp: ctx.t('hist.temp'),
            short: source => shortText(ctx, source)
        });

        const modText = (mods: number): string[] => modNames(mods).map(m => ctx.tEnum(`hist.mod.${m}`, m));

        function legendHtml(h: FanHistoryResponse): string {
            const list = tracks(h);
            const drivers = h.drivers ?? [];
            const kinds = new Set<SourceKind>();
            let mods = 0;
            for (const f of list) {
                f.series.drv.forEach(d => d >= 0 && kinds.add(sourceKind(drivers[d] ?? 'other')));
                mods = f.series.mods.reduce((a, m) => a | m, mods);
            }
            const swatch = (cls: string, label: string) => `<span class="unc-hist-key"><i class="sw ${cls}" aria-hidden="true"></i>${escapeHtml(label)}</span>`;
            const lane = KINDS.filter(k => kinds.has(k)).map(k => swatch(`band k-${k}`, ctx.tRaw(`hist.kind.${k}`)));
            const limits = [
                mods & MOD.quiet ? swatch('mod m-quiet', ctx.t('hist.mod.quiet')) : '',
                mods & (MOD.mover | MOD.parity) ? swatch('mod m-job', ctx.t('hist.mod.job')) : '',
                mods & MOD.floor ? swatch('mod m-floor', ctx.t('hist.mod.floor')) : ''
            ].filter(Boolean);
            const temps = [
                swatch('line drive', ctx.t('hist.legend_drive')),
                swatch('line ref cpu', ctx.t('hist.legend_cpu')),
                swatch('line ref disk', ctx.t('hist.legend_disk'))
            ];
            const group = (title: string, items: string[]) =>
                items.length ? `<div class="unc-hist-group"><b>${escapeHtml(title)}</b>${items.join('')}</div>` : '';
            return group(ctx.t('hist.lane'), lane) + group(ctx.t('hist.limits'), limits) + group(ctx.t('hist.temp'), temps);
        }

        function fanHtml(f: FanTrack, name: string, h: FanHistoryResponse): string {
            const s = f.series;
            const drivers = h.drivers ?? [];
            const i = s.pct.length - 1;
            const pct = last(s.pct);
            const rpm = last(s.rpm);
            const drv = s.drv[i] ?? -1;
            const source = drv >= 0 ? drivers[drv] : undefined;
            const nowText = pct == null ? '' : ctx.t('hist.now', { pct, rpm: rpm ?? '–' });
            const by = source ? ctx.t('hist.now_by', { source: `${sourceText(ctx, source)}${s.temp[i] != null ? ` ${deg(s.temp[i])}` : ''}` }) : '';
            const known = s.pct.filter((v): v is number => v != null);
            const [from, to] = domain(h, range);
            const summary = ctx.t('hist.summary', {
                name,
                min: known.length ? Math.min(...known) : '–',
                max: known.length ? Math.max(...known) : '–',
                pct: pct ?? '–',
                from: clock(Math.max(from, h.t?.[0] ?? from)),
                to: clock(to)
            });
            return `<section class="unc-hist-fan" data-fan="${escapeHtml(f.id)}">
                    <div class="unc-hist-fanhead"><h3>${escapeHtml(name)}</h3><span class="unc-hist-now">${escapeHtml([nowText, by].filter(Boolean).join(' · '))}</span></div>
                    <div class="unc-hist-plot">
                        <svg class="unc-hist-svg" role="img" aria-label="${escapeHtml(summary)}"></svg>
                        <div class="unc-hist-tip" role="status" hidden></div>
                    </div>
                </section>`;
        }

        function tableHtml(list: FanTrack[], names: Map<string, string>, h: FanHistoryResponse): string {
            const drivers = h.drivers ?? [];
            const t = h.t ?? [];
            const rows = sampleIndexes(t.length, 24).reverse();
            const withSeconds = range === '1h';
            return `<details class="unc-hist-table"${tableOpen ? ' open' : ''}><summary>${escapeHtml(ctx.t('hist.table'))}</summary>${list
                .map(
                    f => `<table><caption>${escapeHtml(names.get(f.id) ?? f.id)}</caption><thead><tr>
                            <th scope="col">${escapeHtml(ctx.t('hist.col_time'))}</th><th scope="col">${escapeHtml(ctx.t('hist.col_speed'))}</th>
                            <th scope="col">${escapeHtml(ctx.t('fan.rpm'))}</th><th scope="col">${escapeHtml(ctx.t('hist.col_temp'))}</th>
                            <th scope="col">${escapeHtml(ctx.t('hist.col_by'))}</th><th scope="col">${escapeHtml(ctx.t('hist.col_limits'))}</th></tr></thead><tbody>${rows
                                .map(i => {
                                    const d = f.series.drv[i] ?? -1;
                                    const pct = f.series.pct[i];
                                    return `<tr><td>${escapeHtml(dayClock(t[i] as number, withSeconds))}</td><td>${pct == null ? '–' : `${pct} %`}</td>
                                        <td>${f.series.rpm[i] ?? '–'}</td><td>${deg(f.series.temp[i])}</td>
                                        <td>${d >= 0 ? escapeHtml(sourceText(ctx, drivers[d] ?? 'other')) : '–'}</td>
                                        <td>${escapeHtml(modText(f.series.mods[i] ?? 0).join(', ')) || '–'}</td></tr>`;
                                })
                                .join('')}</tbody></table>`
                )
                .join('')}</details>`;
        }

        /** The tooltip at one point of one fan. */
        function tipHtml(f: FanTrack, h: FanHistoryResponse, i: number): string {
            const s = f.series;
            const drivers = h.drivers ?? [];
            const d = s.drv[i] ?? -1;
            const source = d >= 0 ? (drivers[d] ?? 'other') : '';
            const kind = source ? sourceKind(source) : 'other';
            const pct = s.pct[i];
            const mods = modText(s.mods[i] ?? 0);
            const step = h.step ?? 10;
            return `<div class="tip-time">${escapeHtml(dayClock(h.t?.[i] ?? 0, range === '1h'))}${
                range === '24h' ? ` <small>${escapeHtml(ctx.t('hist.tip_avg', { s: step }))}</small>` : ''
            }</div>
                    <div class="tip-row"><span>${escapeHtml(ctx.t('hist.tip_speed'))}</span><b>${pct == null ? '–' : `${pct} %`} · ${s.rpm[i] ?? '–'} ${escapeHtml(ctx.t('fan.rpm'))}</b></div>
                    <div class="tip-row"><span>${escapeHtml(ctx.t('hist.tip_temp'))}</span><b>${deg(s.temp[i])}</b></div>
                    <div class="tip-row"><span>CPU</span><b>${deg(h.cpu?.[i])}</b></div>
                    <div class="tip-row"><span>${escapeHtml(ctx.t('hist.legend_disk'))}</span><b>${deg(h.case?.[i])}</b></div>
                    <div class="tip-by"><span>${escapeHtml(ctx.t('hist.tip_by'))}</span><b><i class="sw band k-${kind}" aria-hidden="true"></i>${escapeHtml(source ? sourceText(ctx, source) : '–')}${
                        source && s.temp[i] != null ? ` ${escapeHtml(deg(s.temp[i]))}` : ''
                    }</b></div>
                    ${mods.length ? `<div class="tip-mods">${mods.map(m => `<span>${escapeHtml(m)}</span>`).join('')}</div>` : ''}`;
        }

        function draw(): void {
            const h = data;
            if (error) {
                legend.innerHTML = '';
                body.innerHTML = ctx.ui.note(escapeHtml(ctx.t('hist.failed', { error })), { tone: 'warn', box: true, ico: 'alert', tag: 'div' });
                return;
            }
            if (!h) {
                legend.innerHTML = '';
                body.innerHTML = ctx.ui.emptyText(ctx.t('hist.empty_wait'));
                return;
            }
            if (!h.running) {
                legend.innerHTML = '';
                body.innerHTML = ctx.ui.emptyText(ctx.t('hist.empty_stopped'));
                return;
            }
            const list = tracks(h);
            if (!h.t?.length || !list.length) {
                legend.innerHTML = '';
                body.innerHTML = ctx.ui.emptyText(ctx.t('hist.empty_wait'));
                return;
            }
            note.textContent = `${ctx.t('hist.note')} ${h.since ? ctx.t('hist.since', { time: dayClock(h.since) }) + '.' : ''}`;
            legend.innerHTML = legendHtml(h);
            const names = fanNames(list, ctx);
            const wasOpen = body.querySelector<HTMLDetailsElement>('.unc-hist-table')?.open;
            tableOpen = wasOpen ?? tableOpen;
            body.innerHTML = list.map(f => fanHtml(f, names.get(f.id) ?? f.id, h)).join('') + tableHtml(list, names, h);
            paint();
        }

        let geom: Geometry | null = null;
        // Where the pointer is, so a redraw at a new width keeps the crosshair.
        let hoverAt: { id: string; x: number } | null = null;

        /** Draws the charts at the width of the card; needs the card on screen. */
        function paint(): void {
            const h = data;
            if (!h?.running || !h.t?.length) return;
            const list = tracks(h);
            const first = body.querySelector<HTMLElement>('.unc-hist-plot');
            width = Math.round(first?.clientWidth ?? 0) || 640;
            const [from, to] = domain(h, range);
            geom = geometry(width, from, to);
            const g = geom;
            for (const f of list) {
                const svg = body.querySelector<SVGSVGElement>(`[data-fan="${CSS.escape(f.id)}"] svg`);
                if (!svg) continue;
                svg.setAttribute('viewBox', `0 0 ${g.W} ${g.H}`);
                svg.setAttribute('height', String(g.H));
                svg.innerHTML = fanChart(f, h, range, g, text());
                if (hoverAt?.id === f.id) {
                    const cross = svg.querySelector('[data-cross]');
                    cross?.setAttribute('x1', String(hoverAt.x));
                    cross?.setAttribute('x2', String(hoverAt.x));
                    cross?.removeAttribute('hidden');
                }
            }
        }

        function hover(event: PointerEvent): void {
            const h = data;
            const target = event.target instanceof Element ? event.target.closest<HTMLElement>('.unc-hist-fan') : null;
            const plot = target?.querySelector<HTMLElement>('.unc-hist-plot');
            if (!h || !geom || !target || !plot) return;
            const f = tracks(h).find(x => x.id === target.dataset.fan);
            if (!f) return;
            const box = plot.getBoundingClientRect();
            const x = event.clientX - box.left;
            if (x < geom.L || x > geom.W - geom.R) return leave();
            const i = nearest(h.t ?? [], geom.time(x));
            const tip = plot.querySelector<HTMLElement>('.unc-hist-tip');
            const cross = plot.querySelector<SVGLineElement>('[data-cross]');
            if (i < 0 || !tip || !cross) return;
            // A point only counts where the daemon recorded one: not across a hole.
            const step = h.step ?? 10;
            if (Math.abs((h.t?.[i] ?? 0) + step / 2 - geom.time(x)) > step * 1.5) return leave();
            const cx = geom.x((h.t?.[i] ?? 0) + step / 2);
            cross.setAttribute('x1', String(cx));
            cross.setAttribute('x2', String(cx));
            cross.removeAttribute('hidden');
            hoverAt = { id: f.id, x: cx };
            tip.innerHTML = tipHtml(f, h, i);
            tip.hidden = false;
            const room = geom.W - cx;
            tip.style.left = room > tip.offsetWidth + 24 ? `${cx + 12}px` : `${Math.max(0, cx - tip.offsetWidth - 12)}px`;
            tip.style.top = `${Math.max(0, event.clientY - box.top - tip.offsetHeight / 2)}px`;
            leaveOthers(plot);
        }

        function leaveOthers(keep: Element | null): void {
            body.querySelectorAll<HTMLElement>('.unc-hist-plot').forEach(p => {
                if (p === keep) return;
                p.querySelector<HTMLElement>('.unc-hist-tip')?.setAttribute('hidden', '');
                p.querySelector('[data-cross]')?.setAttribute('hidden', '');
            });
        }
        const leave = (): void => {
            hoverAt = null;
            leaveOthers(null);
        };

        body.addEventListener('pointermove', hover);
        body.addEventListener('pointerdown', hover);
        body.addEventListener('pointerleave', leave);

        async function load(): Promise<void> {
            if (loading) return;
            loading = true;
            try {
                data = await ctx.api.get('fan_history', { range }, REFRESH_SECONDS[range] * 500);
                error = '';
            } catch (e) {
                error = errorMessage(e);
            } finally {
                loading = false;
                loadedAt = Date.now();
            }
            draw();
        }

        root.querySelectorAll<HTMLButtonElement>('[data-range]').forEach(button =>
            button.addEventListener('click', () => {
                range = button.dataset.range === '24h' ? '24h' : '1h';
                root.querySelectorAll<HTMLButtonElement>('[data-range]').forEach(b => b.setAttribute('aria-pressed', String(b === button)));
                data = null;
                draw();
                void load();
            })
        );

        // Redraw when the card changes width.
        if (typeof ResizeObserver !== 'undefined') {
            new ResizeObserver(() => {
                const w = Math.round(body.querySelector<HTMLElement>('.unc-hist-plot')?.clientWidth ?? 0);
                if (w && w !== width) paint();
            }).observe(root);
        }

        // The status poll calls this; the history is asked for only while the card is on screen.
        tick = () => {
            if (root.offsetParent === null || document.hidden) return;
            if (!data || Date.now() - loadedAt >= REFRESH_SECONDS[range] * 1000 - 500) void load();
        };
    }
}

registerFanModule(create());
