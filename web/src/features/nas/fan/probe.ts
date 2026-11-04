// The probe wizard for models whose fan wiring is unknown (fan_caps.kind === 'probe'): run, watch, assign cpu/case, confirm.
// Without a confirmed map the daemon never starts; the card says so.
import { errorMessage } from '../../../core/api';
import type { StatusResponse } from '../../../core/contract';
import type { FanProbeStatusResponse, ProbeOutput } from '../../../core/contract-fan-probe';
import { escapeHtml } from '../../../core/html';
import { isCs201xModel } from './cs201x';
import { registerFanModule, type FanCtx } from './module';

type Role = 'cpu' | 'case';

// The DXP2800 GT is a probe model too, but has its own card (cs201x.ts): the IT8613E probe would refuse there.
export const probeVisible = (status: StatusResponse): boolean => status.fan_caps?.kind === 'probe' && !isCs201xModel(status.info?.model);

export const chosenRoles = (outputs: ProbeOutput[], roles: Record<string, Role | ''>): { pwm: string; role: Role }[] =>
    outputs.flatMap(o => {
        const role = roles[o.pwm];
        return o.status === 'mapped' && role ? [{ pwm: o.pwm, role }] : [];
    });

const POLL_MS = 1500;
const REFRESH_MS = 8000;

const state = {
    status: null as FanProbeStatusResponse | null,
    shown: '',
    tachs: null as { id: string; rpm: number }[] | null,
    roles: {} as Record<string, Role | ''>,
    refusal: '',
    fetching: false,
    fetchedAt: 0,
    error: '',
    timer: undefined as ReturnType<typeof setTimeout> | undefined
};

const STATUS_TONE = { mapped: 'ok', no_fan: 'plain', ambiguous: 'warn' } as const;

function intro(ctx: FanCtx, status: FanProbeStatusResponse): string {
    const { ui, t } = ctx;
    const pill = status.fan_control_possible
        ? ui.pill({ tone: 'ok', ico: 'check', text: t('probe.state_on') })
        : ui.pill({ tone: 'warn', ico: 'alert', text: t('probe.state_off') });
    const off = status.fan_control_possible ? '' : ui.note(escapeHtml(t('probe.off_note')), { tone: 'warn', box: true, tag: 'div', ico: 'alert' });
    const error = status.map_error
        ? ui.note(escapeHtml(t('probe.map_error', { error: status.map_error })), { tone: 'bad', box: true, tag: 'div', ico: 'alert' })
        : '';

    return `<div class="unc-fan-tools-status">${pill}</div>${off}${error}`;
}

function tachList(ctx: FanCtx): string {
    const { ui, t } = ctx;
    const pills = (state.tachs ?? []).map(x => ui.pill({ text: t('probe.tach_row', { id: x.id, rpm: x.rpm }) })).join('');

    const read = ui.btn({ label: t('probe.read_tachs'), small: true, attrs: 'data-act="tachs"' });
    if (!pills) return `<div class="unc-fan-tools-actions">${read}</div>`;

    return `${ui.sectionHead(t('probe.tachs'), { actions: read })}
        <div class="unc-fan-tools-tachs">${pills}</div>`;
}

function runButton(ctx: FanCtx, label: string, tone = ''): string {
    return ctx.ui.btn({ label, tone, attrs: 'data-act="run"' });
}

function running(ctx: FanCtx, status: FanProbeStatusResponse): string {
    const p = status.progress;
    const pct = p && p.steps > 0 ? Math.min(100, Math.round((p.step / p.steps) * 100)) : 0;
    const text = ctx.t('probe.running', { pwm: p?.pwm ?? '…', step: p?.step ?? 0, steps: p?.steps ?? 0 });

    return `<div class="unc-fan-tools-progress" role="status">${ctx.ui.meter(pct)}<span class="unc-muted">${escapeHtml(text)}</span></div>${tachList(ctx)}`;
}

function resultTable(ctx: FanCtx, outputs: ProbeOutput[]): string {
    const { t, tEnum } = ctx;
    const option = (value: Role | '', current: string, label: string): string =>
        `<option value="${value}"${value === current ? ' selected' : ''}>${escapeHtml(label)}</option>`;
    const rows = outputs
        .map(o => {
            const status = ctx.ui.pill({ tone: STATUS_TONE[o.status], text: tEnum(`probe.status.${o.status}`, o.status) });
            const role =
                o.status === 'mapped'
                    ? `<select data-role="${escapeHtml(o.pwm)}" aria-label="${escapeHtml(t('probe.col_role'))}">${option('', state.roles[o.pwm] ?? '', t('probe.role_none'))}${option('cpu', state.roles[o.pwm] ?? '', t('probe.role_cpu'))}${option('case', state.roles[o.pwm] ?? '', t('probe.role_case'))}</select>`
                    : '';

            return `<tr>
                <td class="unc-id">${escapeHtml(o.pwm)}</td>
                <td>${status}</td>
                <td class="unc-id">${escapeHtml(o.tach ?? o.candidates?.join(', ') ?? '')}</td>
                <td>${escapeHtml(t('probe.rpm', { base: o.base_rpm, peak: o.peak_rpm }))}</td>
                <td>${role}</td>
            </tr>`;
        })
        .join('');

    return `<div class="unc-fan-tools-scroll"><table class="unc-table"><thead><tr>
        <th>${escapeHtml(t('probe.col_out'))}</th><th>${escapeHtml(t('probe.col_status'))}</th><th>${escapeHtml(t('probe.col_tach'))}</th><th>${escapeHtml(t('probe.col_rpm'))}</th><th>${escapeHtml(t('probe.col_role'))}</th>
    </tr></thead><tbody>${rows}</tbody></table></div>`;
}

function proposal(ctx: FanCtx, status: FanProbeStatusResponse): string {
    const { ui, t } = ctx;
    const p = status.proposal;
    if (!p) return '';
    const agree =
        p.agrees === undefined
            ? ''
            : ui.note(escapeHtml(t(p.agrees ? 'probe.agrees' : 'probe.differs')), {
                  tone: p.agrees ? 'info' : 'warn',
                  box: !p.agrees,
                  tag: 'div',
                  ico: p.agrees ? 'check' : 'alert'
              });
    const ready = chosenRoles(p.outputs, state.roles).length > 0;

    return `${ui.sectionHead(t('probe.result'))}
        ${resultTable(ctx, p.outputs)}
        ${agree}
        <p class="unc-muted" data-need${ready ? ' hidden' : ''}>${escapeHtml(t('probe.confirm_need'))}</p>
        <div class="unc-fan-tools-actions">
            ${ui.btn({ label: t('probe.confirm'), ico: 'check', tone: 'primary', attrs: `data-act="confirm"${ready ? '' : ' disabled'}` })}
            ${runButton(ctx, t('probe.run_again'))}
        </div>
        ${tachList(ctx)}`;
}

function mapView(ctx: FanCtx, status: FanProbeStatusResponse): string {
    const { ui, t } = ctx;
    const map = status.map;
    if (!map) return '';
    const rows = map.outputs
        .map(
            o =>
                `<tr><td class="unc-id">${escapeHtml(o.id)}</td><td>${escapeHtml(t(o.role === 'cpu' ? 'probe.role_cpu' : 'probe.role_case'))}</td><td class="unc-id">${escapeHtml(o.pwm)}</td><td class="unc-id">${escapeHtml(o.tach)}</td></tr>`
        )
        .join('');

    return `${ui.sectionHead(t('probe.map'))}
        <div class="unc-fan-tools-scroll"><table class="unc-table"><thead><tr><th>#</th><th>${escapeHtml(t('probe.col_role'))}</th><th>${escapeHtml(t('probe.col_out'))}</th><th>${escapeHtml(t('probe.col_tach'))}</th></tr></thead><tbody>${rows}</tbody></table></div>
        <div class="unc-fan-tools-actions">
            ${runButton(ctx, t('probe.run_again'))}
            ${ui.btn({ label: t('probe.forget'), ico: 'reset', attrs: 'data-act="forget"' })}
        </div>`;
}

function idle(ctx: FanCtx, status: FanProbeStatusResponse): string {
    const aborted = status.progress?.state === 'aborted' && !status.proposal;
    const note = aborted
        ? ctx.ui.note(escapeHtml(ctx.t('probe.aborted', { reason: status.progress?.reason ?? '–' })), { tone: 'warn', box: true, tag: 'div', ico: 'alert' })
        : '';

    return `<p class="unc-muted">${escapeHtml(ctx.t('probe.intro'))}</p>${note}<div class="unc-fan-tools-actions">${runButton(ctx, ctx.t('probe.run'), 'primary')}</div>${tachList(ctx)}`;
}

function draw(ctx: FanCtx): void {
    const body = ctx.el.querySelector('[data-body]');
    const status = state.status;
    if (!body) return;
    const failed = state.error
        ? ctx.ui.note(escapeHtml(ctx.t('tools.load_failed', { error: state.error })), { tone: 'warn', box: true, tag: 'div', ico: 'alert' })
        : '';
    if (!status) {
        const html = failed;
        if (html !== state.shown) body.innerHTML = html;
        state.shown = html;
        return;
    }
    const refusal = state.refusal
        ? ctx.ui.note(escapeHtml(ctx.t('probe.refused', { error: state.refusal })), { tone: 'bad', box: true, tag: 'div', ico: 'alert' })
        : '';
    const isRunning = status.progress?.state === 'running';
    const main = isRunning ? running(ctx, status) : status.proposal ? proposal(ctx, status) : status.map ? mapView(ctx, status) : idle(ctx, status);
    const html = `${failed}${intro(ctx, status)}${refusal}${main}`;
    const key = JSON.stringify([html, state.roles]);
    if (key === state.shown) return;
    state.shown = key;
    body.innerHTML = html;
}

async function readTachs(ctx: FanCtx): Promise<void> {
    try {
        state.tachs = (await ctx.api.get('fan_probe_tachs')).tachs;
    } catch (error) {
        ctx.toast(errorMessage(error), 'error');
    }
}

/** Only the open page polls: the status call runs the probe tool on the NAS. */
const onScreen = (ctx: FanCtx): boolean => ctx.el.closest('.unc-view')?.classList.contains('active') === true;

async function refresh(ctx: FanCtx, force: boolean): Promise<void> {
    if (state.fetching || (!force && state.error === '' && Date.now() - state.fetchedAt < REFRESH_MS)) return;
    state.fetching = true;
    clearTimeout(state.timer);
    try {
        state.status = await ctx.api.get('fan_probe_status');
        state.error = '';
        state.fetchedAt = Date.now();
        if (state.status.progress?.state === 'running') {
            try {
                state.tachs = (await ctx.api.get('fan_probe_tachs')).tachs;
            } catch {
                // the next poll tries again
            }
            state.timer = setTimeout(() => void (onScreen(ctx) ? refresh(ctx, true) : undefined), POLL_MS);
        }
    } catch (error) {
        state.error = errorMessage(error);
    }
    state.fetching = false;
    draw(ctx);
}

registerFanModule({
    id: 'probe',
    host: 'curves',
    order: 20,
    visible: probeVisible,
    render: ctx =>
        `<article class="unc-card">${ctx.ui.cardHead(ctx.t('probe.title'), { ico: 'fan' })}<div class="unc-fan-tools-body" data-body></div></article>`,
    bind: ctx => {
        ctx.el.addEventListener('change', event => {
            const select = (event.target as Element | null)?.closest<HTMLSelectElement>('select[data-role]');
            if (!select?.dataset.role) return;
            state.roles[select.dataset.role] = select.value === 'cpu' || select.value === 'case' ? select.value : '';
            const ready = chosenRoles(state.status?.proposal?.outputs ?? [], state.roles).length > 0;
            const confirm = ctx.el.querySelector<HTMLButtonElement>('[data-act="confirm"]');
            if (confirm) confirm.disabled = !ready;
            ctx.el.querySelector<HTMLElement>('[data-need]')?.toggleAttribute('hidden', ready);
            state.shown = JSON.stringify([ctx.el.querySelector('[data-body]')?.innerHTML, state.roles]);
        });
        ctx.el.addEventListener('click', event => {
            const act = (event.target as Element | null)?.closest<HTMLElement>('[data-act]')?.dataset.act;
            if (act === 'tachs') {
                void readTachs(ctx).then(() => {
                    state.shown = '';
                    draw(ctx);
                });
            } else if (act === 'run') {
                state.refusal = '';
                void ctx
                    .run(async () => {
                        try {
                            await ctx.api.post('fan_probe_run', {});
                        } catch (error) {
                            state.refusal = errorMessage(error);
                        }
                    })
                    .then(() => refresh(ctx, true));
            } else if (act === 'confirm') {
                const chosen = chosenRoles(state.status?.proposal?.outputs ?? [], state.roles);
                if (!chosen.length) return;
                const map = Object.fromEntries(chosen.map(c => [`map[${c.pwm}]`, c.role]));
                void ctx
                    .run(async () => {
                        await ctx.api.post('fan_probe_confirm', map);
                        state.roles = {};
                    }, ctx.t('probe.confirmed'))
                    .then(() => refresh(ctx, true));
            } else if (act === 'forget') {
                void ctx.run(() => ctx.api.post('fan_probe_forget', {}), ctx.t('probe.forgot')).then(() => refresh(ctx, true));
            }
        });
    },
    update: (_status, ctx) => {
        if (onScreen(ctx)) void refresh(ctx, false);
        else clearTimeout(state.timer);
    }
});
