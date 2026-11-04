// The DXP2800 GT has a Chipsea CS201x, not an IT8613E (fan_caps.kind is 'probe', but the IT8613E probe wizard does not apply):
// diagnostics read from the chip, "Confirm fans" (cs201x confirm) and "Forget" (cs201x forget). Without a confirmed
// map fan control stays off and the status says why in chip_error.
import { errorMessage } from '../../../core/api';
import type { StatusResponse } from '../../../core/contract';
import type { Cs201xReading, FanProbeStatusResponse } from '../../../core/contract-fan-probe';
import { escapeHtml } from '../../../core/html';
import { registerFanModule, type FanCtx } from './module';

/** The backend's isDXP2800GT: the DMI name starts with "DXP2800 GT". */
export const isCs201xModel = (model: string | undefined): boolean => (model ?? '').startsWith('DXP2800 GT');

export const cs201xVisible = (status: StatusResponse): boolean => status.fan_caps?.kind === 'probe' && isCs201xModel(status.info?.model);

/** The fan ids the page offers: fan1 = cpu, fan3 = case (backend csFans). */
export const CS201X_FANS = ['fan1', 'fan3'] as const;

/** The `fans` parameter of cs201x_confirm: undefined when both are chosen (the tool's default). */
export const confirmFans = (chosen: readonly string[]): string | undefined => {
    const ids = CS201X_FANS.filter(id => chosen.includes(id));
    return ids.length === CS201X_FANS.length ? undefined : ids.join(',');
};

const state = {
    status: null as FanProbeStatusResponse | null,
    read: null as Cs201xReading | null,
    chosen: new Set<string>(CS201X_FANS),
    refusal: '',
    error: '',
    readError: '',
    loading: false,
    fetchedAt: 0,
    shown: ''
};

const REFRESH_MS = 8000;
const onScreen = (ctx: FanCtx): boolean => ctx.el.closest('.unc-view')?.classList.contains('active') === true;

function diagnostics(ctx: FanCtx): string {
    const { ui, t } = ctx;
    const r = state.read;
    const rows: [string, string][] = [];
    if (r) {
        rows.push([t('cs201x.k_chip'), r.chip_id], [t('cs201x.k_revision'), r.revision], [t('cs201x.k_host'), r.host_ec]);
        if (r.ec_firmware) rows.push([t('cs201x.k_firmware'), r.ec_firmware]);
        for (const f of r.fans ?? []) {
            rows.push([
                t('cs201x.k_fans'),
                t('cs201x.fan_row', { id: f.id, role: t(f.role === 'cpu' ? 'probe.role_cpu' : 'probe.role_case'), rpm: f.rpm, duty: f.duty_pct })
            ]);
        }
        if (r.temps?.cpu !== undefined) rows.push([t('cs201x.k_cpu'), `${r.temps.cpu} °C`]);
        if (r.temps?.board !== undefined) rows.push([t('cs201x.k_board'), `${r.temps.board} °C`]);
        if (r.temp_inputs) rows.push([t('cs201x.k_inputs'), t(r.temp_inputs.configured ? 'cs201x.inputs_on' : 'cs201x.inputs_off')]);
        if (r.watchdog) {
            rows.push([
                t('cs201x.k_watchdog'),
                t('cs201x.wdt', {
                    armed: t(r.watchdog.armed ? 'cs201x.wdt_armed' : 'cs201x.wdt_off'),
                    counting: t(r.watchdog.counting ? 'cs201x.wdt_counting' : 'cs201x.wdt_idle')
                })
            ]);
        }
    }
    const list = rows.length
        ? `<dl class="unc-facts">${rows.map(([k, v]) => `<dt>${escapeHtml(k)}</dt><dd>${escapeHtml(v)}</dd>`).join('')}</dl>`
        : ui.emptyText(state.readError ? t('tools.load_failed', { error: state.readError }) : t('panel.reading'));
    const notes = (r?.notes ?? []).map(n => `<li>${escapeHtml(n)}</li>`).join('');

    return `${ui.sectionHead(t('cs201x.diag'), { actions: ui.btn({ label: t('cs201x.reread'), ico: 'reset', small: true, attrs: 'data-act="reread"' }) })}
        <p class="unc-muted">${escapeHtml(t('cs201x.diag_hint'))}</p>
        ${list}${notes ? `<ul class="unc-fan-tools-list unc-muted">${notes}</ul>` : ''}`;
}

function confirmed(ctx: FanCtx, status: FanProbeStatusResponse): string {
    const { ui, t } = ctx;
    const map = status.map;
    if (!map) return '';
    const rows = map.outputs.map(o => `<li>${escapeHtml(t(o.id === 'fan1' ? 'cs201x.fan1' : 'cs201x.fan3'))}</li>`).join('');

    return `${ui.sectionHead(t('cs201x.map'))}<ul class="unc-fan-tools-list">${rows}</ul>`;
}

function picker(ctx: FanCtx): string {
    const { ui, t } = ctx;
    const boxes = CS201X_FANS.map(id =>
        ui.chipCheck(`data-fan="${id}"`, state.chosen.has(id), escapeHtml(t(id === 'fan1' ? 'cs201x.fan1' : 'cs201x.fan3')))
    ).join('');

    return `${ui.sectionHead(t('cs201x.pick'))}<div class="unc-fan-tools-actions">${boxes}</div>`;
}

function draw(ctx: FanCtx): void {
    const body = ctx.el.querySelector('[data-body]');
    if (!body) return;
    const { ui, t } = ctx;
    const status = state.status;
    const data = ctx.status();
    const on = status?.fan_control_possible === true;
    const pill = status
        ? `<div class="unc-fan-tools-status">${ui.pill({ tone: on ? 'ok' : 'warn', ico: on ? 'check' : 'alert', text: t(on ? 'cs201x.state_on' : 'cs201x.state_off') })}</div>`
        : '';
    const reason =
        !on && data?.chip_error
            ? ui.note(escapeHtml(t('cs201x.reason', { error: data.chip_error })), { tone: 'warn', box: true, tag: 'div', ico: 'alert' })
            : '';
    const mapError = status?.map_error
        ? ui.note(escapeHtml(t('probe.map_error', { error: status.map_error })), { tone: 'bad', box: true, tag: 'div', ico: 'alert' })
        : '';
    const failed = state.error
        ? ui.note(escapeHtml(t('tools.load_failed', { error: state.error })), { tone: 'warn', box: true, tag: 'div', ico: 'alert' })
        : '';
    const refusal = state.refusal
        ? ui.note(escapeHtml(t('cs201x.refused', { error: state.refusal })), { tone: 'bad', box: true, tag: 'div', ico: 'alert' })
        : '';
    const ready = state.chosen.size > 0;
    const actions = status
        ? `${on ? confirmed(ctx, status) : `<p class="unc-muted">${escapeHtml(t('cs201x.intro'))}</p>`}
           ${picker(ctx)}
           <p class="unc-muted" data-need${ready ? ' hidden' : ''}>${escapeHtml(t('cs201x.confirm_need'))}</p>
           <div class="unc-fan-tools-actions">
               ${ui.btn({ label: t('cs201x.confirm'), ico: 'check', tone: on ? '' : 'primary', attrs: `data-act="confirm"${ready ? '' : ' disabled'}` })}
               ${on ? ui.btn({ label: t('cs201x.forget'), ico: 'reset', attrs: 'data-act="forget"' }) : ''}
           </div>`
        : '';
    const html = `${failed}${pill}${reason}${mapError}${refusal}${actions}${diagnostics(ctx)}`;
    if (html === state.shown) return;
    state.shown = html;
    body.innerHTML = html;
}

async function refresh(ctx: FanCtx, force: boolean): Promise<void> {
    if (state.loading || (!force && Date.now() - state.fetchedAt < REFRESH_MS)) return;
    state.loading = true;
    try {
        state.status = await ctx.api.get('fan_probe_status');
        state.error = '';
    } catch (error) {
        state.error = errorMessage(error);
    }
    try {
        state.read = (await ctx.api.get('cs201x_read')).read;
        state.readError = '';
    } catch (error) {
        state.readError = errorMessage(error);
    }
    state.fetchedAt = Date.now();
    state.loading = false;
    draw(ctx);
}

registerFanModule({
    id: 'cs201x',
    host: 'curves',
    order: 20,
    visible: cs201xVisible,
    render: ctx =>
        `<article class="unc-card">${ctx.ui.cardHead(ctx.t('cs201x.title'), { ico: 'fan' })}<div class="unc-fan-tools-body" data-body></div></article>`,
    bind: ctx => {
        ctx.el.addEventListener('change', event => {
            const box = (event.target as Element | null)?.closest<HTMLInputElement>('input[data-fan]');
            if (!box?.dataset.fan) return;
            if (box.checked) state.chosen.add(box.dataset.fan);
            else state.chosen.delete(box.dataset.fan);
            const ready = state.chosen.size > 0;
            const confirm = ctx.el.querySelector<HTMLButtonElement>('[data-act="confirm"]');
            if (confirm) confirm.disabled = !ready;
            ctx.el.querySelector<HTMLElement>('[data-need]')?.toggleAttribute('hidden', ready);
            state.shown = ctx.el.querySelector('[data-body]')?.innerHTML ?? '';
        });
        ctx.el.addEventListener('click', event => {
            const act = (event.target as Element | null)?.closest<HTMLElement>('[data-act]')?.dataset.act;
            if (act === 'reread') {
                void refresh(ctx, true);
            } else if (act === 'confirm') {
                if (state.chosen.size === 0) return;
                const fans = confirmFans([...state.chosen]);
                state.refusal = '';
                void ctx
                    .run(async () => {
                        try {
                            await ctx.api.post('cs201x_confirm', fans === undefined ? {} : { fans });
                        } catch (error) {
                            state.refusal = errorMessage(error);
                            throw error;
                        }
                    }, ctx.t('cs201x.confirmed'))
                    .then(() => refresh(ctx, true));
            } else if (act === 'forget') {
                if (!window.confirm(ctx.t('cs201x.forget_confirm'))) return;
                state.refusal = '';
                void ctx.run(() => ctx.api.post('cs201x_forget', {}), ctx.t('cs201x.forgot')).then(() => refresh(ctx, true));
            }
        });
    },
    update: (_status, ctx) => {
        if (onScreen(ctx)) void refresh(ctx, false);
    }
});
