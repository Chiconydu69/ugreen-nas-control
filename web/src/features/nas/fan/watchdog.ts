// The EC watchdog status card on the Overview: the state word is computed by the backend (fan/ec_watchdog.go), this only shows it.
import type { StatusResponse } from '../../../core/contract';
import { escapeHtml } from '../../../core/html';
import { registerFanModule } from './module';

export const watchdogVisible = (status: StatusResponse): boolean => status.fan_caps?.watchdog === true;

const TONES = { armed: 'ok', off: 'plain', action_needed: 'warn', yielded: 'plain', not_counting: 'plain', not_effective: 'warn' } as const;

registerFanModule({
    id: 'watchdog',
    host: 'overview',
    order: 0,
    visible: watchdogVisible,
    render: ctx =>
        `<article class="unc-card">${ctx.ui.cardHead(ctx.t('watchdog.title'), { ico: 'shield' })}<div class="unc-fan-tools-body" data-body></div></article>`,
    update: (status, ctx) => {
        const body = ctx.el.querySelector('[data-body]');
        if (!body) return;
        const state = status.watchdog?.state;
        if (!state) {
            body.innerHTML = ctx.ui.emptyText(ctx.t('watchdog.none'));
            return;
        }
        const pill = ctx.ui.pill({
            tone: TONES[state],
            ico: state === 'armed' ? 'check' : state === 'action_needed' ? 'alert' : '',
            text: ctx.tEnum(`watchdog.state.${state}`, state)
        });
        const hint =
            state === 'action_needed' ? ctx.ui.note(escapeHtml(ctx.t('watchdog.hint_action')), { tone: 'warn', box: true, tag: 'div', ico: 'alert' }) : '';
        const kill =
            state === 'armed' || state === 'action_needed'
                ? ctx.ui.note(escapeHtml(ctx.t('watchdog.hint_kill')), { tone: 'plain', box: true, tag: 'div' })
                : '';
        body.innerHTML = `<div class="unc-fan-tools-status">${pill}</div>${hint}${kill}`;
    }
});
