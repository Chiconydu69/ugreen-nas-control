// Profiles: export the saved fan settings, import a profile of this plugin or another fan tool. Always a preview first;
// nothing is written before Apply, and the other tool's files are only read (by the backend).
import { errorMessage } from '../../../core/api';
import type { StatusResponse } from '../../../core/contract';
import type { FanProfilePreviewResponse, FanProfileSource } from '../../../core/contract-fan-inputs';
import { parseCurve, type CurvePoint } from '../../../core/curve';
import { escapeHtml } from '../../../core/html';
import { registerFanModule, type FanCtx } from './module';

const EXPORT_URL = '/plugins/ugreen-nas-control/api.php?action=fan_profile_export';

interface Request {
    source?: string;
    path?: string;
    content?: string;
}

export const profilesVisible = (status: StatusResponse): boolean => status.fan_caps !== undefined;

export function isPreview(value: unknown): value is FanProfilePreviewResponse {
    const v = value as Partial<FanProfilePreviewResponse> | null;
    return (
        typeof v === 'object' &&
        v !== null &&
        typeof v.token === 'string' &&
        typeof v.title === 'string' &&
        Array.isArray(v.diff) &&
        Array.isArray(v.warnings) &&
        Array.isArray(v.not_imported) &&
        typeof v.profile === 'object' &&
        v.profile !== null
    );
}

export function miniCurve(curves: { cls: 'cpu' | 'case'; points: CurvePoint[] }[]): string {
    const W = 240;
    const H = 120;
    const pad = 8;
    const all = curves.flatMap(c => c.points);
    if (!all.length) return '';
    const lo = Math.min(...all.map(p => p.temp));
    const span = Math.max(1, Math.max(...all.map(p => p.temp)) - lo);
    const x = (temp: number): number => pad + ((temp - lo) / span) * (W - 2 * pad);
    const y = (pct: number): number => H - pad - (Math.min(100, Math.max(0, pct)) / 100) * (H - 2 * pad);
    const grid = [0, 50, 100].map(p => `<line class="unc-fan-tools-grid" x1="${pad}" x2="${W - pad}" y1="${y(p)}" y2="${y(p)}"/>`).join('');
    const lines = curves
        .map(c => {
            const pts = c.points.map(p => `${x(p.temp).toFixed(1)},${y(p.pct).toFixed(1)}`).join(' ');
            const dots = c.points
                .map(p => `<circle class="unc-fan-tools-dot is-${c.cls}" cx="${x(p.temp).toFixed(1)}" cy="${y(p.pct).toFixed(1)}" r="2.5"/>`)
                .join('');
            return `<polyline class="unc-fan-tools-line is-${c.cls}" points="${pts}"/>${dots}`;
        })
        .join('');

    return `<svg class="unc-fan-tools-curve" viewBox="0 0 ${W} ${H}" role="img" aria-hidden="true">${grid}${lines}</svg>`;
}

const state = {
    sources: null as FanProfileSource[] | null,
    loading: false,
    error: '',
    paths: {} as Record<string, string>,
    preview: null as FanProfilePreviewResponse | null,
    request: null as Request | null,
    rival: ''
};

export function previewView(ctx: FanCtx, p: FanProfilePreviewResponse): string {
    const { ui, t } = ctx;
    const cpu = parseCurve(p.profile.cpu_curve);
    const kase = parseCurve(p.profile.case_curve);
    const curves = [...(cpu.length ? [{ cls: 'cpu' as const, points: cpu }] : []), ...(kase.length ? [{ cls: 'case' as const, points: kase }] : [])];
    const temps = curves.flatMap(c => c.points.map(q => q.temp));
    const chart = curves.length
        ? `<div class="unc-fan-tools-curves">${miniCurve(curves)}<div>
            <div class="unc-fan-tools-legend">${cpu.length ? `<span><i class="is-cpu"></i>${escapeHtml(t('profiles.cpu_curve'))}</span>` : ''}${kase.length ? `<span><i class="is-case"></i>${escapeHtml(t('profiles.case_curve'))}</span>` : ''}</div>
            <p class="unc-muted">${escapeHtml(t('profiles.curve_range', { from: Math.min(...temps), to: Math.max(...temps) }))}</p></div></div>`
        : ui.emptyText(t('profiles.no_curve'));
    const rows = p.diff
        .map(d => `<tr><td class="unc-id">${escapeHtml(d.key)}</td><td>${escapeHtml(d.from ?? '–')}</td><td>${escapeHtml(d.to)}</td></tr>`)
        .join('');
    const diff = p.diff.length
        ? `<div class="unc-fan-tools-scroll"><table class="unc-table"><thead><tr><th>${escapeHtml(t('profiles.key'))}</th><th>${escapeHtml(t('profiles.now'))}</th><th>${escapeHtml(t('profiles.new'))}</th></tr></thead><tbody>${rows}</tbody></table></div>`
        : ui.emptyText(t('profiles.no_diff'));
    const same = p.unchanged > 0 && p.diff.length ? `<p class="unc-muted">${escapeHtml(t('profiles.unchanged', { n: p.unchanged }))}</p>` : '';
    const rival = state.rival ? ui.note(escapeHtml(t('profiles.rival', { name: state.rival })), { tone: 'bad', box: true, tag: 'div', ico: 'alert' }) : '';
    const warnings = p.warnings.length
        ? `${ui.sectionHead(t('profiles.notes'))}<ul class="unc-fan-tools-list">${p.warnings.map(w => `<li>${escapeHtml(w)}</li>`).join('')}</ul>`
        : '';
    const skipped = p.not_imported.length
        ? `${ui.sectionHead(t('profiles.skipped'))}<ul class="unc-fan-tools-list">${p.not_imported.map(n => `<li><span class="unc-id">${escapeHtml(n.key)}</span>: ${escapeHtml(n.reason)}</li>`).join('')}</ul>`
        : '';
    const backup = p.v2 ? `<p class="unc-muted">${escapeHtml(t('profiles.backup'))}</p>` : '';

    return `<div class="unc-fan-tools-preview">
        ${ui.sectionHead(t('profiles.preview_title', { title: p.title }))}
        ${rival}
        ${ui.sectionHead(t('profiles.curves'))}${chart}
        ${ui.sectionHead(t('profiles.diff'))}${diff}${same}
        ${warnings}${skipped}${backup}
        <div class="unc-fan-tools-actions">
            ${ui.btn({ label: t('profiles.apply'), ico: 'check', tone: 'primary', attrs: `data-act="apply"${p.diff.length ? '' : ' disabled'}` })}
            ${ui.btn({ label: t('profiles.cancel'), attrs: 'data-act="cancel"' })}
        </div>
    </div>`;
}

export function sourceRow(ctx: FanCtx, s: FanProfileSource): string {
    const { ui, t } = ctx;
    const ask = s.id === 'itkuny' && !s.path;
    const input = ask
        ? `<input type="text" data-path="${escapeHtml(s.id)}" value="${escapeHtml(state.paths[s.id] ?? '')}" placeholder="${escapeHtml(t('profiles.path'))}" aria-label="${escapeHtml(t('profiles.path'))}">`
        : '';
    const pill = s.running ? ui.pill({ tone: 'warn', text: t('profiles.running') }) : '';
    const files = s.path ? [s.path] : s.files;

    return `<div class="unc-fan-tools-src"><div><strong>${escapeHtml(s.name)} ${pill}</strong><small>${escapeHtml(files.join(', '))}</small></div>
        <div class="unc-fan-tools-src-act">${input}${ui.btn({ label: t('profiles.preview'), small: true, attrs: `data-act="source" data-id="${escapeHtml(s.id)}"` })}</div></div>`;
}

function draw(ctx: FanCtx): void {
    const body = ctx.el.querySelector('[data-body]');
    if (!body) return;
    const { ui, t } = ctx;
    const failed = state.error
        ? ui.note(escapeHtml(t('tools.load_failed', { error: state.error })), { tone: 'warn', box: true, tag: 'div', ico: 'alert' })
        : '';
    const list = state.sources?.length ? state.sources.map(s => sourceRow(ctx, s)).join('') : ui.emptyText(t('profiles.none'));
    // The path being typed survives a redraw because every key press is stored.
    body.innerHTML = `<p class="unc-muted">${escapeHtml(t('profiles.intro'))}</p>
        <div class="unc-fan-tools-actions">
            <a class="unc-btn" href="${EXPORT_URL}" download><span>${escapeHtml(t('profiles.export'))}</span></a>
            ${ui.btn({ label: t('profiles.import_file'), attrs: 'data-act="file"' })}
        </div>
        ${ui.sectionHead(t('profiles.found'))}<div>${failed || list}</div>
        ${state.preview ? previewView(ctx, state.preview) : ''}`;
}

async function showPreview(ctx: FanCtx, request: Request, rival: string): Promise<void> {
    try {
        const answer = await ctx.api.post('fan_profile_preview', request);
        if (!isPreview(answer)) throw new Error(ctx.t('profiles.read_failed'));
        state.preview = answer;
        state.request = request;
        state.rival = rival;
    } catch (error) {
        ctx.toast(errorMessage(error), 'error');
    }
    draw(ctx);
}

async function loadSources(ctx: FanCtx): Promise<void> {
    if (state.sources || state.loading) return;
    state.loading = true;
    try {
        state.sources = (await ctx.api.get('fan_profile_sources')).sources;
        state.error = '';
    } catch (error) {
        const message = errorMessage(error);
        const same = message === state.error;
        state.error = message;
        state.loading = false;
        if (!same) draw(ctx);
        return;
    }
    state.loading = false;
    draw(ctx);
}

registerFanModule({
    id: 'profiles',
    host: 'curves',
    order: 30,
    visible: profilesVisible,
    render: ctx => `<article class="unc-card">${ctx.ui.cardHead(ctx.t('profiles.title'), { ico: 'curve' })}
        <div class="unc-fan-tools-body" data-body></div><input type="file" class="unc-fan-tools-file" data-file></article>`,
    bind: ctx => {
        const file = ctx.el.querySelector<HTMLInputElement>('[data-file]');
        file?.addEventListener('change', async () => {
            const chosen = file.files?.[0];
            if (!chosen) return;
            let content = '';
            try {
                content = await chosen.text();
            } catch {
                ctx.toast(ctx.t('profiles.read_failed'), 'error');
            }
            file.value = '';
            if (content) await showPreview(ctx, { content }, '');
        });
        ctx.el.addEventListener('input', event => {
            const input = (event.target as Element | null)?.closest<HTMLInputElement>('input[data-path]');
            if (input?.dataset.path) state.paths[input.dataset.path] = input.value;
        });
        ctx.el.addEventListener('click', event => {
            const target = (event.target as Element | null)?.closest<HTMLElement>('[data-act]');
            const act = target?.dataset.act;
            if (act === 'file') {
                file?.click();
            } else if (act === 'source') {
                const source = state.sources?.find(s => s.id === target?.dataset.id);
                if (!source) return;
                const path = source.path || (state.paths[source.id] ?? '').trim();
                if (source.id === 'itkuny' && !path) {
                    ctx.toast(ctx.t('profiles.path_needed'), 'error');
                    return;
                }
                void showPreview(ctx, path ? { source: source.id, path } : { source: source.id }, source.running ? source.name : '');
            } else if (act === 'cancel') {
                state.preview = null;
                state.request = null;
                draw(ctx);
            } else if (act === 'apply') {
                const { preview, request } = state;
                if (!preview || !request) return;
                void ctx
                    .run(async () => {
                        await ctx.api.post('fan_profile_apply', { token: preview.token, ...request });
                        state.preview = null;
                        state.request = null;
                    }, ctx.t('profiles.applied'))
                    .then(() => draw(ctx));
            }
        });
    },
    update: (_status, ctx) => void loadSources(ctx)
});
