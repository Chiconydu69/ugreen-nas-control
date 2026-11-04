// How a fan group follows its curve (cpu_/case_ deadband, start_pct, start_ms, min_run, up_step, down_step, allow_off):
// one fold under the curves, saved through fan_v2 with its own save bar.
import type { FanConfigV2, FanGroupConfig, StatusResponse } from '../../../core/contract';
import { escapeHtml } from '../../../core/html';
import { changes, clamp, RANGES, v2Params, validValue } from './fields';
import { registerFanModule, type FanModule } from './module';

const GROUPS = ['cpu', 'case'] as const;
type Group = (typeof GROUPS)[number];
const NUMBERS = ['deadband', 'start_pct', 'start_ms', 'min_run', 'up_step', 'down_step'] as const;
type NumberField = (typeof NUMBERS)[number];
type Shape = Record<NumberField, number> & { allow_off: boolean };
type Draft = Record<Group, Shape>;

const shapeOf = (g: FanGroupConfig): Shape => ({
    deadband: g.deadband,
    start_pct: g.start_pct,
    start_ms: g.start_ms,
    min_run: g.min_run,
    up_step: g.up_step,
    down_step: g.down_step,
    allow_off: g.allow_off
});

const draftOf = (cfg: FanConfigV2): Draft => ({ cpu: shapeOf(cfg.cpu), case: shapeOf(cfg.case) });

export const valuesOf = (draft: Draft): Record<string, string> =>
    Object.fromEntries(
        GROUPS.flatMap(group => [
            ...NUMBERS.map(field => [`${group}_${field}`, String(draft[group][field])]),
            [`${group}_allow_off`, draft[group].allow_off ? '1' : '0']
        ])
    );

/** The lowest speed at which every fan of the group was measured to turn; null without a measurement. */
export function measuredStart(status: StatusResponse, group: Group): number | null {
    const table = status.calibration?.table?.fans;
    if (!table) return null;
    const starts = (status.fans ?? [])
        .filter(fan => fan.role === group)
        .map(fan => table[fan.id]?.find(([, rpm]) => rpm > 0)?.[0])
        .filter((pct): pct is number => pct !== undefined);
    return starts.length ? Math.max(...starts) : null;
}

const STEP: Record<NumberField, number> = { deadband: 1, start_pct: 1, start_ms: 100, min_run: 1, up_step: 1, down_step: 1 };

function create(): FanModule {
    let sync: ((status: StatusResponse) => void) | undefined;

    return {
        id: 'shaping',
        host: 'curves',
        order: 20,
        visible: status => Boolean(status.fan_caps && status.fan_caps.channels > 0 && status.settings.fan_config),
        render: ctx => `<article class="unc-card unc-fan-shaping">
            ${ctx.ui.cardHead(ctx.t('shaping.title'), { ico: 'gauge' })}
            <p class="unc-muted">${escapeHtml(ctx.t('shaping.intro'))}</p>
            <details class="unc-fan-shaping-fold">
                <summary>${escapeHtml(ctx.t('shaping.open'))}</summary>
                <div class="unc-fan-shaping-body" data-body></div>
            </details>
            ${ctx.ui.saveDock('shaping', 'unc-fan-shaping-save')}
        </article>`,
        bind(ctx) {
            let draft: Draft | null = null;
            let lastHtml = '';
            const touched = new Set<string>();
            const body = ctx.el.querySelector<HTMLElement>('[data-body]');
            if (!body) return;

            const text = (field: NumberField, value: number): string =>
                field === 'start_ms'
                    ? `${value} ms`
                    : (field === 'up_step' || field === 'down_step') && value === 100
                      ? ctx.t('shaping.instant')
                      : `${value} %`;

            const rowHtml = (group: Group, field: NumberField, shape: Shape, extra = ''): string => {
                const id = `unc-fs-${group}-${field}`;
                const [min, max] = RANGES[field];
                return `<div class="unc-form">
                    <div class="unc-fan-shaping-label">
                        <label for="${id}">${escapeHtml(ctx.tRaw(`shaping.${field}`))}</label>
                        <small class="unc-muted">${escapeHtml(ctx.tRaw(`shaping.${field}_hint`))}</small>
                    </div>
                    <div class="unc-fan-shaping-control">
                        ${ctx.ui.rangeField({ id, min, max, step: STEP[field], value: shape[field], unit: field === 'start_ms' ? 'ms' : '%' })}
                        ${extra}
                    </div>
                </div>`;
            };

            const groupHtml = (group: Group, status: StatusResponse, cfg: FanConfigV2): string => {
                if (!draft) return '';
                const shape = draft[group];
                const measured = measuredStart(status, group);
                const own = Object.keys(cfg.channels).sort();
                const useMeasured =
                    measured === null
                        ? ''
                        : ctx.ui.btn({ label: ctx.t('shaping.measured', { pct: measured }), ico: 'gauge', small: true, attrs: `data-fs-measured="${group}"` });
                return `<section class="unc-fan-shaping-group">
                    ${ctx.ui.sectionHead(ctx.t(group === 'cpu' ? 'curves.cpu' : 'curves.case'))}
                    ${rowHtml(group, 'deadband', shape)}
                    ${rowHtml(group, 'start_pct', shape, useMeasured)}
                    ${rowHtml(group, 'start_ms', shape)}
                    ${rowHtml(group, 'min_run', shape)}
                    ${rowHtml(group, 'up_step', shape)}
                    ${rowHtml(group, 'down_step', shape)}
                    ${ctx.ui.switchRow({ id: `unc-fs-${group}-allow_off`, title: ctx.t('shaping.allow_off'), hint: ctx.t('shaping.allow_off_hint') })}
                    ${shape.allow_off ? ctx.ui.note(escapeHtml(ctx.t('shaping.allow_off_warn')), { tone: 'warn', box: true, ico: 'alert', tag: 'div' }) : ''}
                    ${own.length && group === 'case' ? ctx.ui.note(escapeHtml(ctx.t('shaping.overrides', { fans: own.join(', ') })), { tone: 'info' }) : ''}
                </section>`;
            };

            const paint = (force = false): void => {
                const status = ctx.status();
                const cfg = status?.settings.fan_config;
                if (!status || !cfg || !draft) return;
                // a poll never rewrites controls while the user edits them
                if (!force && ctx.isDirty()) return;
                const html = GROUPS.map(group => groupHtml(group, status, cfg)).join('');
                if (!force && html === lastHtml) return;
                body.innerHTML = html;
                lastHtml = html;
                for (const group of GROUPS) {
                    const box = body.querySelector<HTMLInputElement>(`#unc-fs-${group}-allow_off`);
                    if (box) box.checked = draft[group].allow_off;
                }
            };

            body.addEventListener('input', event => {
                const el = event.target;
                if (!(el instanceof HTMLInputElement) || el.type !== 'range' || !draft) return;
                const m = /^unc-fs-(cpu|case)-([a-z_]+)$/.exec(el.id);
                const group = GROUPS.find(g => g === m?.[1]);
                const field = NUMBERS.find(f => f === m?.[2]);
                if (!group || !field) return;
                const value = clamp(Number(el.value), RANGES[field]);
                draft[group][field] = value;
                touched.add(`${group}_${field}`);
                const out = body.querySelector(`#${el.id}-value`);
                if (out) out.textContent = text(field, value);
                ctx.markDirty('shaping');
            });

            body.addEventListener('change', event => {
                const el = event.target;
                if (!(el instanceof HTMLInputElement) || el.type !== 'checkbox' || !draft) return;
                const group = GROUPS.find(g => el.id === `unc-fs-${g}-allow_off`);
                if (!group) return;
                draft[group].allow_off = el.checked;
                touched.add(`${group}_allow_off`);
                ctx.markDirty('shaping');
                paint(true);
            });

            ctx.el.addEventListener('click', event => {
                const el = event.target instanceof Element ? event.target : null;
                const measuredBtn = el?.closest<HTMLElement>('[data-fs-measured]');
                if (measuredBtn && draft) {
                    const group = GROUPS.find(g => g === measuredBtn.dataset.fsMeasured);
                    const status = ctx.status();
                    const measured = group && status ? measuredStart(status, group) : null;
                    if (group && measured !== null) {
                        draft[group].start_pct = clamp(measured, RANGES.start_pct);
                        touched.add(`${group}_start_pct`);
                        ctx.markDirty('shaping');
                        paint(true);
                    }
                    return;
                }

                if (el?.closest('[data-discard="shaping"]')) {
                    const cfg = ctx.status()?.settings.fan_config;
                    if (cfg) draft = draftOf(cfg);
                    touched.clear();
                    ctx.clearDirty();
                    paint(true);
                    return;
                }

                if (el?.closest('#unc-fan-shaping-save')) {
                    const cfg = ctx.status()?.settings.fan_config;
                    if (!cfg || !draft) return;
                    const diff = changes(valuesOf(draft), valuesOf(draftOf(cfg)), touched);
                    if (!Object.keys(diff).length) {
                        ctx.clearDirty();
                        return;
                    }
                    if (!Object.entries(diff).every(([key, value]) => validValue(key, value))) {
                        ctx.toast(ctx.t('shaping.invalid'), 'error');
                        return;
                    }
                    void ctx.save(() => ctx.api.post('fan_v2', v2Params(diff)), ctx.t('save.done'));
                }
            });

            sync = status => {
                const cfg = status.settings.fan_config;
                if (!cfg) return;
                if (!draft || !ctx.isDirty()) {
                    draft = draftOf(cfg);
                    touched.clear();
                }
                paint();
            };
        },
        update: status => sync?.(status)
    };
}

registerFanModule(create());
