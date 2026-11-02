// "Power on after AC loss" on the Settings page: what the NAS does when mains power returns (power_ac_get, power_ac_set).
// The setting lives in the board. Where the board can be read, the card shows the current value; the DXP4800 GT cannot be
// read, there it shows what this plugin last sent and says so. Every write asks first.
import { errorMessage } from '../../../core/api';
import type { StatusResponse } from '../../../core/contract';
import type { PowerAcMode, PowerAcResponse } from '../../../core/contract-fan-probe';
import { berlin } from '../../../core/format';
import { escapeHtml } from '../../../core/html';
import type { Key } from '../text';
import { hasPowerAc } from '../power-ac-models';
import { registerFanModule, type FanCtx } from './module';

export const powerAcVisible = (status: StatusResponse): boolean => hasPowerAc(status.info?.model ?? '');

/** The registers as the tool read them: `{f2: 244, f4: 230}` becomes "F2 0xf4, F4 0xe6". */
export const rawText = (raw: Record<string, number>): string =>
    Object.entries(raw)
        .map(([key, value]) => `${key.toUpperCase()} 0x${value.toString(16).padStart(2, '0')}`)
        .join(', ');

const MODE_KEYS: Record<PowerAcMode | 'unknown', Key> = {
    on: 'power_ac.on',
    off: 'power_ac.off',
    last: 'power_ac.last',
    unknown: 'power_ac.unknown'
};

const state = { data: null as PowerAcResponse | null, error: '', notice: '', loading: false, shown: '' };

function draw(ctx: FanCtx): void {
    const body = ctx.el.querySelector('[data-body]');
    if (!body) return;
    const { ui, t } = ctx;
    const d = state.data;
    const warn = (text: string, tone = 'warn'): string => ui.note(escapeHtml(text), { tone, box: true, tag: 'div', ico: 'alert' });
    if (!d) {
        const html = state.error ? warn(t('tools.load_failed', { error: state.error })) : ui.emptyText(t('panel.reading'));
        if (html !== state.shown) body.innerHTML = html;
        state.shown = html;
        return;
    }
    const current = d.readable
        ? `${ui.sectionHead(t('power_ac.current'))}<div class="unc-fan-tools-status">${ui.pill({
              tone: d.mode === 'unknown' ? 'warn' : 'ok',
              ico: d.mode === 'unknown' ? 'alert' : 'check',
              text: t(MODE_KEYS[d.mode]),
              lg: true
          })}</div>`
        : `<p class="unc-muted">${escapeHtml(t('power_ac.unreadable'))}</p>`;
    const unknown = d.readable && d.mode === 'unknown' ? warn(t('power_ac.unknown_note', { raw: rawText(d.raw) })) : '';
    const last = d.last_set ? t('power_ac.last_set', { mode: t(MODE_KEYS[d.last_set.mode]), time: berlin(d.last_set.time) }) : t('power_ac.never_set');
    // An undecodable value (older BIOS encoding) is not set from the page: the note shows it and points to the CLI.
    const offerSet = !(d.readable && d.mode === 'unknown');
    const buttons = d.settable
        .map(mode => ui.btn({ label: t(MODE_KEYS[mode]), tone: d.readable && d.mode === mode ? 'primary' : '', attrs: `data-mode="${mode}"` }))
        .join('');
    const notice = state.notice ? ui.note(escapeHtml(state.notice), { tone: 'info', box: true, tag: 'div', ico: 'info' }) : '';
    const setTo = offerSet ? `${ui.sectionHead(t('power_ac.set_to'))}<div class="unc-fan-tools-actions">${buttons}</div>` : '';
    const html = `${current}${unknown}<p class="unc-muted">${escapeHtml(last)}</p>${notice}${setTo}`;
    if (html === state.shown) return;
    state.shown = html;
    body.innerHTML = html;
}

async function load(ctx: FanCtx): Promise<void> {
    if (state.loading || state.data) return;
    state.loading = true;
    try {
        state.data = await ctx.api.get('power_ac_get');
        state.error = '';
    } catch (error) {
        state.error = errorMessage(error);
    }
    state.loading = false;
    draw(ctx);
}

async function set(ctx: FanCtx, mode: PowerAcMode): Promise<void> {
    state.notice = '';
    await ctx.run(
        async () => {
            const answer = await ctx.api.post('power_ac_set', { mode });
            state.data = answer;
            if (!answer.readable) state.notice = ctx.t('power_ac.no_readback');
            else if (answer.verified === false) state.notice = ctx.t('power_ac.not_verified');
        },
        ctx.t('power_ac.saved', { mode: ctx.t(MODE_KEYS[mode]) })
    );
    draw(ctx);
}

registerFanModule({
    id: 'power-ac',
    host: 'settings',
    order: 40,
    visible: powerAcVisible,
    render: ctx => `<article class="unc-card">
        ${ctx.ui.cardHead(ctx.t('power_ac.title'), { ico: 'power' })}
        <p class="unc-muted">${escapeHtml(ctx.t('power_ac.hint'))}</p>
        ${ctx.ui.note(escapeHtml(ctx.t('panel.unconfirmed')), { tone: 'info', box: true, tag: 'div', ico: 'info' })}
        <div class="unc-fan-tools-body" data-body></div>
    </article>`,
    bind: ctx => {
        ctx.el.addEventListener('click', event => {
            const mode = (event.target as Element | null)?.closest<HTMLElement>('[data-mode]')?.dataset.mode;
            if (mode !== 'on' && mode !== 'off' && mode !== 'last') return;
            if (!window.confirm(ctx.t('power_ac.confirm', { mode: ctx.t(MODE_KEYS[mode]) }))) return;
            void set(ctx, mode);
        });
    },
    // Only the open Settings page asks the tool: it touches the board's registers.
    update: (_status, ctx) => {
        if (ctx.el.closest('.unc-view')?.classList.contains('active')) void load(ctx);
    }
});
