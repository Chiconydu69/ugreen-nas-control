// The Front panel page: the state of the power and reset button (one read per click, never a watch) and a test beep.
// The tool and its model tables are backend/internal/panel; which model has what is panel-models.ts. Every reading is
// UGREEN driver source, not yet confirmed on hardware, and the page says so.
import { errorMessage } from '../../../core/api';
import type { StatusResponse } from '../../../core/contract';
import type { PanelButtonsResponse } from '../../../core/contract-fan-probe';
import { berlin } from '../../../core/format';
import { escapeHtml } from '../../../core/html';
import { hasBeeper, hasButtons, powerDoubtful } from '../panel-models';
import { registerFanModule, type FanCtx } from './module';

const modelOf = (status: StatusResponse): string => status.info?.model ?? '';

export const buttonsVisible = (status: StatusResponse): boolean => hasButtons(modelOf(status));
export const beeperVisible = (status: StatusResponse): boolean => hasBeeper(modelOf(status));

const state = { read: null as PanelButtonsResponse | null, error: '', loading: false, asked: false, shown: '' };

const onScreen = (ctx: FanCtx): boolean => ctx.el.closest('.unc-view')?.classList.contains('active') === true;

function drawButtons(ctx: FanCtx): void {
    const body = ctx.el.querySelector('[data-body]');
    if (!body) return;
    const { ui, t } = ctx;
    const model = ctx.status()?.info?.model ?? '';
    const word = (held: boolean): string => t(held ? 'panel.held' : 'panel.released');
    const pill = (label: string, held: boolean | null): string =>
        ui.pill({
            tone: held === null ? 'plain' : held ? 'warn' : 'ok',
            ico: held ? 'power' : '',
            text: `${label}: ${held === null ? t('panel.unreadable') : word(held)}`,
            lg: true
        });
    const read = state.read;
    const states = read
        ? `<div class="unc-fan-tools-status">${pill(t('panel.power'), read.power_readable ? read.power === 'held' : null)}${pill(t('panel.reset'), read.reset === 'held')}</div>
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
        ${ctx.ui.note(escapeHtml(ctx.t('panel.unconfirmed')), { tone: 'info', box: true, tag: 'div', ico: 'info' })}
        <div class="unc-fan-tools-body" data-body></div>
    </article>`,
    bind: ctx => {
        ctx.el.addEventListener('click', event => {
            if ((event.target as Element | null)?.closest('[data-act="read"]')) void load(ctx);
        });
    },
    update: (_status, ctx) => {
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
        ${ctx.ui.note(escapeHtml(ctx.t('panel.unconfirmed')), { tone: 'info', box: true, tag: 'div', ico: 'info' })}
        <div class="unc-fan-tools-actions">${ctx.ui.btn({ label: ctx.t('panel.beep'), ico: 'bell', attrs: 'data-act="beep"' })}</div>
    </article>`,
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
