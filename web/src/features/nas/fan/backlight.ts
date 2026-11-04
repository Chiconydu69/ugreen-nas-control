// The LCD backlight of the iDX Pro (fan_backlight): a slider that saves when released.
import { errorMessage } from '../../../core/api';
import { setRangeFill } from '../../../core/dom';
import type { StatusResponse } from '../../../core/contract';
import { escapeHtml } from '../../../core/html';
import { registerFanModule, type FanCtx } from './module';

export const backlightVisible = (status: StatusResponse): boolean => status.fan_caps?.backlight === true;

/** Where the slider starts while the register holds the vendor default: UGREEN's driver registers the display at 80 %. */
export const DEFAULT_SLIDER_PCT = 80;

/** The answer's `pct` is a number, or the string "default" while the register holds the vendor default (raw 0). */
export function backlightView(pct: number | string): { value: number; isDefault: boolean } {
    const value = typeof pct === 'number' ? pct : Number(pct);

    return pct === 'default' || !Number.isFinite(value)
        ? { value: DEFAULT_SLIDER_PCT, isDefault: true }
        : { value: Math.min(100, Math.max(0, Math.round(value))), isDefault: false };
}

let loading = false;
let loaded = false;
let failed = false;

registerFanModule({
    id: 'backlight',
    host: 'settings',
    order: 50,
    visible: backlightVisible,
    render: ctx => `<article class="unc-card">
        ${ctx.ui.cardHead(ctx.t('backlight.title'), { ico: 'bulb', titleId: 'unc-fan-backlight-title' })}
        <p class="unc-muted">${escapeHtml(ctx.t('backlight.hint'))}</p>
        ${ctx.ui.rangeField({ id: 'unc-fan-backlight', value: 100, unit: '%', labelledby: 'unc-fan-backlight-title' })}
        <p class="unc-muted" id="unc-fan-backlight-default" hidden>${escapeHtml(ctx.t('backlight.default_hint'))}</p>
    </article>`,
    bind: ctx => {
        const slider = ctx.el.querySelector<HTMLInputElement>('#unc-fan-backlight');
        const value = ctx.el.querySelector('#unc-fan-backlight-value');
        if (!slider || !value) return;
        slider.disabled = true;
        slider.addEventListener('input', () => {
            value.textContent = `${slider.value} %`;
        });
        slider.addEventListener('change', async () => {
            try {
                await ctx.api.post('fan_backlight', { pct: Number(slider.value) });
                ctx.el.querySelector('#unc-fan-backlight-default')?.setAttribute('hidden', '');
                ctx.saved();
            } catch (error) {
                ctx.toast(errorMessage(error), 'error');
            }
        });
    },
    update: (_status, ctx) => void load(ctx)
});

async function load(ctx: FanCtx): Promise<void> {
    if (loaded || loading) return;
    const slider = ctx.el.querySelector<HTMLInputElement>('#unc-fan-backlight');
    const value = ctx.el.querySelector('#unc-fan-backlight-value');
    if (!slider || !value) return;
    loading = true;
    try {
        const answer = await ctx.api.get('fan_backlight');
        const view = backlightView(answer.pct);
        slider.value = String(view.value);
        setRangeFill(slider);
        value.textContent = view.isDefault ? ctx.t('backlight.default') : `${view.value} %`;
        ctx.el.querySelector('#unc-fan-backlight-default')?.toggleAttribute('hidden', !view.isDefault);
        slider.disabled = false;
        loaded = true;
    } catch (error) {
        if (!failed) ctx.toast(errorMessage(error), 'error');
        failed = true;
    }
    loading = false;
}
