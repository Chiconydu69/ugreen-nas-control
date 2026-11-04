// Quiet hours (quiet_*) and the mover / parity-check override (job_*): its own card under the curves, saved through
// fan_v2. What the daemon does now (quiet_active, job, cap) comes with the status.
import type { FanConfigV2, FanJobMode, StatusResponse } from '../../../core/contract';
import { escapeHtml } from '../../../core/html';
import { changes, clamp, DAYS, JOB_MODES, RANGES, selectHtml, v2Params, validValue } from './fields';
import { registerFanModule, type FanModule } from './module';

const JOBS = ['mover', 'parity'] as const;
type Job = (typeof JOBS)[number];

export interface Draft {
    enabled: boolean;
    from: string;
    to: string;
    days: string[];
    cap: number;
    scope: 'case' | 'all';
    mover: { mode: FanJobMode; pct: number };
    parity: { mode: FanJobMode; pct: number };
}

const daysOf = (days: string): string[] => DAYS.filter(day => days.split(',').includes(day));

export const draftOf = (cfg: FanConfigV2): Draft => ({
    enabled: cfg.quiet.enabled,
    from: cfg.quiet.from,
    to: cfg.quiet.to,
    days: daysOf(cfg.quiet.days),
    cap: cfg.quiet.cap,
    scope: cfg.quiet.scope,
    mover: { mode: cfg.mover.mode, pct: cfg.mover.pct },
    parity: { mode: cfg.parity.mode, pct: cfg.parity.pct }
});

export const valuesOf = (d: Draft): Record<string, string> => ({
    quiet_enabled: d.enabled ? '1' : '0',
    quiet_from: d.from,
    quiet_to: d.to,
    quiet_days: DAYS.filter(day => d.days.includes(day)).join(','),
    quiet_cap: String(d.cap),
    quiet_scope: d.scope,
    job_mover: d.mover.mode,
    job_mover_pct: String(d.mover.pct),
    job_parity: d.parity.mode,
    job_parity_pct: String(d.parity.pct)
});

/** Why the draft cannot be saved (an i18n key), or '' when the backend takes it. */
export const problem = (d: Draft): '' | 'schedule.err_same' | 'schedule.err_days' => {
    if (d.enabled && d.from === d.to) return 'schedule.err_same';
    return d.days.length ? '' : 'schedule.err_days';
};

const pad = (n: number): string => String(n).padStart(2, '0');
const HOURS = Array.from({ length: 24 }, (_, h) => pad(h));
const MINUTES = Array.from({ length: 12 }, (_, m) => pad(m * 5));

function weekdays(lang: string): string[] {
    try {
        const format = new Intl.DateTimeFormat(lang, { weekday: 'short', timeZone: 'UTC' });
        // 2024-01-01 was a Monday
        return DAYS.map((_, i) => format.format(new Date(Date.UTC(2024, 0, 1 + i))));
    } catch {
        return [...DAYS];
    }
}

function create(): FanModule {
    let sync: ((status: StatusResponse) => void) | undefined;

    return {
        id: 'schedule',
        host: 'curves',
        order: 30,
        visible: status => Boolean(status.fan_caps && status.fan_caps.channels > 0 && status.settings.fan_config),
        render: ctx => `<article class="unc-card unc-fan-schedule">
            ${ctx.ui.cardHead(ctx.t('schedule.title'), { ico: 'clock' })}
            <div class="unc-fan-schedule-live" data-live></div>
            <div class="unc-fan-schedule-body" data-body></div>
            ${ctx.ui.saveDock('schedule', 'unc-fan-schedule-save')}
        </article>`,
        bind(ctx) {
            let draft: Draft | null = null;
            let lastHtml = '';
            let lastLive = '';
            const touched = new Set<string>();
            const body = ctx.el.querySelector<HTMLElement>('[data-body]');
            const live = ctx.el.querySelector<HTMLElement>('[data-live]');
            if (!body || !live) return;

            const timeHtml = (id: string, value: string): string => {
                const [h = '00', m = '00'] = value.split(':');
                const minutes = MINUTES.includes(m) ? MINUTES : [...MINUTES, m].sort();
                const choice = (list: string[]) => list.map(v => ({ value: v, label: v }));
                return `<div class="unc-fan-schedule-time">
                    ${selectHtml(`id="${id}" data-sq-time="${id}-h" aria-label="${escapeHtml(ctx.t('schedule.hour'))}"`, choice(HOURS), h)}
                    <span aria-hidden="true">:</span>
                    ${selectHtml(`data-sq-time="${id}-m" aria-label="${escapeHtml(ctx.t('schedule.minute'))}"`, choice(minutes), m)}
                </div>`;
            };

            const quietHtml = (d: Draft): string => {
                const names = weekdays(window.UGREEN_NAS_LANG || 'en');
                return `<section class="unc-fan-schedule-sec">
                    ${ctx.ui.sectionHead(ctx.t('schedule.quiet'))}
                    ${ctx.ui.switchRow({ id: 'unc-sq-enabled', title: ctx.t('schedule.quiet_on'), hint: ctx.t('schedule.quiet_hint') })}
                    ${
                        d.enabled
                            ? `<div class="unc-form"><label for="unc-sq-from">${escapeHtml(ctx.t('schedule.from'))}</label>${timeHtml('unc-sq-from', d.from)}</div>
                    <div class="unc-form"><label for="unc-sq-to">${escapeHtml(ctx.t('schedule.to'))}</label>${timeHtml('unc-sq-to', d.to)}</div>
                    ${d.from === d.to ? ctx.ui.note(escapeHtml(ctx.t('schedule.err_same')), { tone: 'warn', box: true, ico: 'alert', tag: 'div' }) : ''}
                    <div class="unc-form">
                        <div class="unc-fan-shaping-label"><span class="unc-label">${escapeHtml(ctx.t('schedule.days'))}</span><small class="unc-muted">${escapeHtml(ctx.t('schedule.days_hint'))}</small></div>
                        <div class="unc-fan-schedule-days">${DAYS.map((day, i) =>
                            ctx.ui.chipCheck(
                                `data-sq-day="${day}"`,
                                d.days.includes(day),
                                `<span class="unc-cc-name"><strong>${escapeHtml(names[i] ?? day)}</strong></span>`
                            )
                        ).join('')}</div>
                    </div>
                    <div class="unc-form">
                        <div class="unc-fan-shaping-label"><label for="unc-sq-cap">${escapeHtml(ctx.t('schedule.cap'))}</label><small class="unc-muted">${escapeHtml(ctx.t('schedule.cap_hint'))}</small></div>
                        ${ctx.ui.rangeField({ id: 'unc-sq-cap', min: RANGES.quiet_cap[0], max: RANGES.quiet_cap[1], step: 5, value: d.cap, unit: '%' })}
                    </div>
                    <div class="unc-form">
                        <label for="unc-sq-scope">${escapeHtml(ctx.t('schedule.scope'))}</label>
                        ${selectHtml(
                            'id="unc-sq-scope" data-sq-scope',
                            [
                                { value: 'case', label: ctx.t('schedule.scope_case') },
                                { value: 'all', label: ctx.t('schedule.scope_all') }
                            ],
                            d.scope
                        )}
                    </div>`
                            : ''
                    }
                </section>`;
            };

            const jobHtml = (d: Draft, job: Job): string => {
                const { mode, pct } = d[job];
                const id = `unc-sj-${job}`;
                return `<div class="unc-form">
                    <label for="${id}-mode">${escapeHtml(ctx.t(job === 'mover' ? 'schedule.mover' : 'schedule.parity'))}</label>
                    <div class="unc-fan-schedule-job">
                        ${selectHtml(
                            `id="${id}-mode" data-sj-mode="${job}"`,
                            JOB_MODES.map(value => ({ value, label: ctx.tEnum(`schedule.mode.${value}`, value) })),
                            mode
                        )}
                        ${
                            mode === 'off'
                                ? ''
                                : ctx.ui.rangeField({ id, min: RANGES.job_mover_pct[0], max: RANGES.job_mover_pct[1], step: 5, value: pct, unit: '%' })
                        }
                        <small class="unc-muted" data-sj-hint="${job}">${escapeHtml(ctx.tEnum(`schedule.mode_hint.${mode}`, mode, { pct }))}</small>
                    </div>
                </div>`;
            };

            const bodyHtml = (d: Draft): string => `${quietHtml(d)}
                <section class="unc-fan-schedule-sec">
                    ${ctx.ui.sectionHead(ctx.t('schedule.jobs'))}
                    <p class="unc-muted">${escapeHtml(ctx.t('schedule.jobs_intro'))}</p>
                    ${JOBS.map(job => jobHtml(d, job)).join('')}
                </section>
                ${ctx.ui.note(escapeHtml(ctx.t('schedule.precedence')), { tone: 'info', box: true, ico: 'info', tag: 'div' })}`;

            const paint = (force = false): void => {
                if (!draft) return;
                if (!force && ctx.isDirty()) return;
                const html = bodyHtml(draft);
                if (!force && html === lastHtml) return;
                body.innerHTML = html;
                lastHtml = html;
                const box = body.querySelector<HTMLInputElement>('#unc-sq-enabled');
                if (box) box.checked = draft.enabled;
            };

            const paintLive = (status: StatusResponse): void => {
                const cfg = status.settings.fan_config;
                const daemon = status.daemon;
                if (!cfg || !daemon) {
                    live.innerHTML = '';
                    lastLive = '';
                    return;
                }
                const quiet = daemon.quiet_active
                    ? ctx.ui.pill({ tone: 'accent', ico: 'clock', text: ctx.t('schedule.live_quiet', { to: cfg.quiet.to, cap: daemon.cap ?? cfg.quiet.cap }) })
                    : cfg.quiet.enabled
                      ? ctx.ui.pill({ ico: 'clock', text: ctx.t('schedule.live_wait', { from: cfg.quiet.from }) })
                      : '';
                const job = daemon.job
                    ? ctx.ui.pill({ tone: 'violet', ico: 'fan', text: ctx.tEnum(`schedule.live_job.${daemon.job}`, daemon.job) })
                    : cfg.mover.mode !== 'off' || cfg.parity.mode !== 'off'
                      ? ctx.ui.pill({ ico: 'fan', text: ctx.t('schedule.live_idle') })
                      : '';
                const html = quiet + job;
                if (html !== lastLive) live.innerHTML = html;
                lastLive = html;
            };

            const edit = (what: 'quiet' | 'jobs', keys: string[], repaint: boolean): void => {
                for (const key of keys) touched.add(key);
                ctx.markDirty(what);
                if (repaint) paint(true);
            };

            const timePart = (el: HTMLSelectElement): void => {
                if (!draft) return;
                const [id, part] = (el.dataset.sqTime ?? '').split(/-(?=[hm]$)/);
                const key = id === 'unc-sq-from' ? 'from' : id === 'unc-sq-to' ? 'to' : null;
                if (!key) return;
                const [h = '00', m = '00'] = draft[key].split(':');
                draft[key] = part === 'h' ? `${el.value}:${m}` : `${h}:${el.value}`;
                edit('quiet', [`quiet_${key}`], true);
            };

            body.addEventListener('input', event => {
                const el = event.target;
                if (!(el instanceof HTMLInputElement) || el.type !== 'range' || !draft) return;
                const out = body.querySelector(`#${el.id}-value`);
                if (el.id === 'unc-sq-cap') {
                    draft.cap = clamp(Number(el.value), RANGES.quiet_cap);
                    if (out) out.textContent = `${draft.cap} %`;
                    touched.add('quiet_cap');
                    ctx.markDirty('quiet');
                    return;
                }
                const job = JOBS.find(j => el.id === `unc-sj-${j}`);
                if (!job) return;
                const range = job === 'mover' ? RANGES.job_mover_pct : RANGES.job_parity_pct;
                draft[job].pct = clamp(Number(el.value), range);
                if (out) out.textContent = `${draft[job].pct} %`;
                const hint = body.querySelector(`[data-sj-hint="${job}"]`);
                if (hint) hint.textContent = ctx.tEnum(`schedule.mode_hint.${draft[job].mode}`, draft[job].mode, { pct: draft[job].pct });
                touched.add(`job_${job}_pct`);
                ctx.markDirty('jobs');
            });

            body.addEventListener('change', event => {
                const el = event.target;
                if (!draft) return;
                if (el instanceof HTMLInputElement && el.id === 'unc-sq-enabled') {
                    draft.enabled = el.checked;
                    edit('quiet', ['quiet_enabled'], true);
                } else if (el instanceof HTMLInputElement && el.dataset.sqDay) {
                    const day = el.dataset.sqDay;
                    const next = el.checked ? [...draft.days, day] : draft.days.filter(d => d !== day);
                    if (next.length) draft.days = DAYS.filter(d => next.includes(d));
                    edit('quiet', ['quiet_days'], true);
                } else if (el instanceof HTMLSelectElement && el.dataset.sqTime) {
                    timePart(el);
                } else if (el instanceof HTMLSelectElement && 'sqScope' in el.dataset) {
                    draft.scope = el.value === 'all' ? 'all' : 'case';
                    edit('quiet', ['quiet_scope'], false);
                } else if (el instanceof HTMLSelectElement && el.dataset.sjMode) {
                    const job = JOBS.find(j => j === el.dataset.sjMode);
                    const mode = JOB_MODES.find(m => m === el.value);
                    if (job && mode) {
                        draft[job].mode = mode;
                        edit('jobs', [`job_${job}`], false);
                        paint(true);
                    }
                }
            });

            ctx.el.addEventListener('click', event => {
                const el = event.target instanceof Element ? event.target : null;
                const cfg = ctx.status()?.settings.fan_config;

                if (el?.closest('[data-discard="schedule"]')) {
                    if (cfg) draft = draftOf(cfg);
                    touched.clear();
                    ctx.clearDirty();
                    paint(true);
                    return;
                }

                if (el?.closest('#unc-fan-schedule-save')) {
                    if (!cfg || !draft) return;
                    const blocked = problem(draft);
                    if (blocked) {
                        ctx.toast(ctx.t(blocked), 'error');
                        return;
                    }
                    const diff = changes(valuesOf(draft), valuesOf(draftOf(cfg)), touched);
                    if (!Object.keys(diff).length) {
                        ctx.clearDirty();
                        return;
                    }
                    if (!Object.entries(diff).every(([key, value]) => validValue(key, value))) {
                        ctx.toast(ctx.t('schedule.invalid'), 'error');
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
                paintLive(status);
                paint();
            };
        },
        update: status => sync?.(status)
    };
}

registerFanModule(create());
