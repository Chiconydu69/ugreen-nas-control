// The Front panel page: the state of the power and reset button (one read per click, never a watch) and a test beep.
// The tool and its model tables are backend/internal/panel; which model has what is panel-models.ts. Every reading is
// UGREEN driver source; the page says per model whether users confirmed it (the registry's panel check, model-info.ts).
import { errorMessage } from '../../../core/api';
import type { StatusResponse } from '../../../core/contract';
import type { PanelButtonsResponse, PanelPowerResponse } from '../../../core/contract-fan-probe';
import { berlin } from '../../../core/format';
import { escapeHtml } from '../../../core/html';
import { panelConfirmed, panelEvidence } from '../model-info';
import { hasBeeper, hasButtons, powerDoubtful } from '../panel-models';
import { registerFanModule, type FanCtx } from './module';

const modelOf = (status: StatusResponse): string => status.info?.model ?? '';

export const buttonsVisible = (status: StatusResponse): boolean => hasButtons(modelOf(status));
export const beeperVisible = (status: StatusResponse): boolean => hasBeeper(modelOf(status));

const state = { read: null as PanelButtonsResponse | null, error: '', loading: false, asked: false, shown: '' };

const onScreen = (ctx: FanCtx): boolean => ctx.el.closest('.unc-view')?.classList.contains('active') === true;

/** The note on top of a card: confirmed by users on this model, or vendor source only. */
function drawEvidence(status: StatusResponse, ctx: FanCtx): void {
    const slot = ctx.el.querySelector('[data-evidence]');
    if (!slot) return;
    const found = panelEvidence(modelOf(status));
    const confirmed = found?.state === 'users' || found?.state === 'confirmed';
    const partial = found?.state === 'partial';
    const html = partial
        ? ctx.ui.note(escapeHtml(`${ctx.t('panel.partial', { by: found.by })} ${found.note}`.trim()), { tone: 'warn', box: true, tag: 'div', ico: 'info' })
        : confirmed
          ? ctx.ui.note(escapeHtml(ctx.t(found.by ? 'panel.confirmed_by' : 'panel.confirmed', { by: found.by })), {
                tone: 'info',
                box: true,
                tag: 'div',
                ico: 'check'
            })
          : ctx.ui.note(escapeHtml(ctx.t('panel.unconfirmed')), { tone: 'info', box: true, tag: 'div', ico: 'info' });
    if (slot.innerHTML !== html) slot.innerHTML = html;
}

function drawButtons(ctx: FanCtx): void {
    const body = ctx.el.querySelector('[data-body]');
    if (!body) return;
    const { ui, t } = ctx;
    const model = ctx.status()?.info?.model ?? '';
    const word = (held: boolean): string => t(held ? 'panel.held' : 'panel.released');
    const pill = (label: string, held: boolean | null, hidden = false): string =>
        ui.pill({
            tone: held === null ? 'plain' : held ? 'warn' : 'ok',
            ico: held ? 'power' : '',
            text: `${label}: ${hidden ? t('panel.unreadable_box') : held === null ? t('panel.unreadable') : word(held)}`,
            lg: true
        });
    const read = state.read;
    const states = read
        ? `<div class="unc-fan-tools-status">${pill(t('panel.power'), read.power_readable && read.power !== 'unreadable' ? read.power === 'held' : null, read.power === 'unreadable')}${pill(t('panel.reset'), read.reset === 'unreadable' ? null : read.reset === 'held', read.reset === 'unreadable')}</div>
           <p class="unc-muted">${escapeHtml(t('panel.read_at', { time: berlin(read.time) }))}</p>`
        : `<p class="unc-muted">${escapeHtml(t(state.loading ? 'panel.reading' : 'panel.not_read'))}</p>`;
    const error = state.error ? ui.note(escapeHtml(t('panel.read_failed', { error: state.error })), { tone: 'bad', box: true, tag: 'div', ico: 'alert' }) : '';
    const doubt = powerDoubtful(model) ? ui.note(escapeHtml(t('panel.power_doubtful')), { tone: 'warn', box: true, tag: 'div', ico: 'alert' }) : '';
    const html = `${states}${error}${doubt}<div class="unc-fan-tools-actions">${ui.btn({
        label: t('panel.read'),
        ico: 'reset',
        attrs: `data-act="read"${state.loading ? ' disabled' : ''}`
    })}</div>`;
    if (html === state.shown) return;
    state.shown = html;
    body.innerHTML = html;
}

async function load(ctx: FanCtx): Promise<void> {
    if (state.loading) return;
    state.loading = true;
    state.asked = true;
    drawButtons(ctx);
    try {
        state.read = await ctx.api.get('panel_buttons');
        state.error = '';
    } catch (error) {
        state.error = errorMessage(error);
    }
    state.loading = false;
    drawButtons(ctx);
}

registerFanModule({
    id: 'panel-buttons',
    host: 'panel',
    order: 0,
    visible: buttonsVisible,
    render: ctx => `<article class="unc-card">
        ${ctx.ui.cardHead(ctx.t('panel.buttons_title'), { ico: 'power' })}
        <p class="unc-muted">${escapeHtml(ctx.t('panel.buttons_hint'))}</p>
        <div data-evidence></div>
        <div class="unc-fan-tools-body" data-body></div>
    </article>`,
    bind: ctx => {
        ctx.el.addEventListener('click', event => {
            if ((event.target as Element | null)?.closest('[data-act="read"]')) void load(ctx);
        });
    },
    update: (status, ctx) => {
        drawEvidence(status, ctx);
        // The first look at the page reads once; after that only the button reads.
        if (!state.asked && onScreen(ctx)) void load(ctx);
        else drawButtons(ctx);
    }
});

registerFanModule({
    id: 'panel-beeper',
    host: 'panel',
    order: 10,
    visible: beeperVisible,
    render: ctx => `<article class="unc-card">
        ${ctx.ui.cardHead(ctx.t('panel.beeper_title'), { ico: 'bell' })}
        <p class="unc-muted">${escapeHtml(ctx.t('panel.beeper_hint'))}</p>
        <div data-evidence></div>
        <div class="unc-fan-tools-actions">${ctx.ui.btn({ label: ctx.t('panel.beep'), ico: 'bell', attrs: 'data-act="beep"' })}</div>
    </article>`,
    update: drawEvidence,
    bind: ctx => {
        ctx.el.addEventListener('click', event => {
            const button = (event.target as Element | null)?.closest<HTMLButtonElement>('[data-act="beep"]');
            if (!button || button.disabled) return;
            button.disabled = true;
            void ctx
                .run(() => ctx.api.post('panel_beep', {}), ctx.t('panel.beeped'))
                .finally(() => {
                    button.disabled = false;
                });
        });
    }
});

// The opt-in shutdown on a held power button (panel_power). Shown only where users confirmed the buttons: the watcher
// (`ugreen-nas panel power-action`) refuses any other model as well.
export const powerVisible = (status: StatusResponse): boolean => hasButtons(modelOf(status)) && panelConfirmed(modelOf(status));

export const POWER_HOLDS = ['1', '1.5', '2', '2.5', '3'] as const;
const holdText = (hold: number): string => String(hold);

const power = { data: null as PanelPowerResponse | null, error: '', loading: false, shown: '' };

function drawPower(ctx: FanCtx): void {
    const body = ctx.el.querySelector<HTMLElement>('[data-power]');
    if (!body) return;
    const { ui, t } = ctx;
    const d = power.data;
    if (!d) {
        const html = power.error
            ? ui.note(escapeHtml(t('tools.load_failed', { error: power.error })), { tone: 'bad', box: true, tag: 'div', ico: 'alert' })
            : ui.emptyText(t('panel.reading'));
        if (html !== power.shown) body.innerHTML = html;
        power.shown = html;
        return;
    }
    const hold = holdText(d.hold);
    const options = POWER_HOLDS.map(h => `<option value="${h}"${h === hold ? ' selected' : ''}>${escapeHtml(t('panel.power_secs', { s: h }))}</option>`).join(
        ''
    );
    const html = `${ui.note(escapeHtml(t('panel.power_warning')), { tone: 'warn', box: true, tag: 'div', ico: 'alert' })}
        ${ui.switchRow({ id: 'panel-power-on', title: t('panel.power_toggle', { s: hold }), hint: t('panel.power_toggle_hint') })}
        <label class="unc-field"><span>${escapeHtml(t('panel.power_hold'))}</span><select data-power-hold aria-label="${escapeHtml(t('panel.power_hold'))}">${options}</select></label>
        <p class="unc-muted">${escapeHtml(t('panel.power_dry'))} <code>ugreen-nas panel power-action --dry-run</code></p>`;
    if (html !== power.shown) {
        power.shown = html;
        body.innerHTML = html;
    }
    const toggle = body.querySelector<HTMLInputElement>('#panel-power-on');
    if (toggle) toggle.checked = d.enabled;
}

async function loadPower(ctx: FanCtx): Promise<void> {
    if (power.loading || power.data) return;
    power.loading = true;
    try {
        power.data = await ctx.api.get('panel_power');
        power.error = '';
    } catch (error) {
        power.error = errorMessage(error);
    }
    power.loading = false;
    drawPower(ctx);
}

/** Saves the toggle and the hold time; on a refusal (409: the watcher did not start) the page shows the old state again. */
async function savePower(ctx: FanCtx, enabled: boolean, hold: string): Promise<void> {
    await ctx.run(
        async () => {
            power.data = await ctx.api.post('panel_power', { enabled: enabled ? '1' : '0', hold });
        },
        ctx.t(enabled ? 'panel.power_saved_on' : 'panel.power_saved_off')
    );
    power.shown = '';
    drawPower(ctx);
}

registerFanModule({
    id: 'panel-power',
    host: 'panel',
    order: 5,
    visible: powerVisible,
    render: ctx => `<article class="unc-card">
        ${ctx.ui.cardHead(ctx.t('panel.power_title'), { ico: 'power' })}
        <p class="unc-muted">${escapeHtml(ctx.t('panel.power_hint'))}</p>
        <div class="unc-fan-tools-body" data-power></div>
    </article>`,
    bind: ctx => {
        ctx.el.addEventListener('change', event => {
            const target = event.target as HTMLInputElement | HTMLSelectElement | null;
            const d = power.data;
            if (!target || !d) return;
            const select = ctx.el.querySelector<HTMLSelectElement>('[data-power-hold]');
            const toggle = ctx.el.querySelector<HTMLInputElement>('#panel-power-on');
            if (!select || !toggle) return;
            if (target === toggle) {
                // Switching on asks first; "no" puts the switch back.
                if (toggle.checked && !window.confirm(ctx.t('panel.power_confirm', { s: select.value }))) {
                    toggle.checked = false;
                    return;
                }
                void savePower(ctx, toggle.checked, select.value);
            } else if (target === select && d.enabled) {
                // A new hold time while it is on is applied at once, with the same question.
                if (!window.confirm(ctx.t('panel.power_confirm', { s: select.value }))) {
                    select.value = holdText(d.hold);
                    return;
                }
                void savePower(ctx, true, select.value);
            } else if (target === select) {
                void savePower(ctx, false, select.value);
            }
        });
    },
    update: (_status, ctx) => {
        if (ctx.el.closest('.unc-view')?.classList.contains('active')) void loadPower(ctx);
        else drawPower(ctx);
    }
});
