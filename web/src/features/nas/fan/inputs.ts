// Temperature source per fan group (cpu_input, case_input, *_on_missing): the select offers what fan_inputs reports as
// present, drives and "hottest of several" are built from checklists, scripts get a test button.
import { errorMessage } from '../../../core/api';
import type { FanConfigV2, FanOnMissing, StatusResponse } from '../../../core/contract';
import type { FanInputDisk, FanInputOptionDetail, FanInputsResponse } from '../../../core/contract-fan-inputs';
import { escapeHtml } from '../../../core/html';
import { changes, ON_MISSING, selectHtml, v2Params, validValue, type Choice } from './fields';
import { registerFanModule, type FanModule } from './module';

const GROUPS = ['cpu', 'case'] as const;
type Group = (typeof GROUPS)[number];
type Draft = Record<Group, { input: string; on_missing: FanOnMissing }>;

const REFRESH_MS = 8000;
const MAX_PARTS = [2, 8] as const;
const MAX_DRIVES = 32;
const COMPONENT = ['cpu', 'nvme', 'gpu', 'hba', 'nic', 'script', 'disk_max', 'disk_avg'];
const DRIVE_NAME = /^[A-Za-z0-9_.-]{1,40}$/;

export const specMode = (spec: string): string => {
    if (spec === '') return 'auto';
    if (spec.startsWith('disk_max')) return 'disk_max';
    if (spec.startsWith('disk_avg')) return 'disk_avg';
    return spec.startsWith('max:') ? 'max' : spec;
};

export const specDisks = (spec: string): string[] =>
    specMode(spec).startsWith('disk_') && spec.includes(':') ? spec.slice(spec.indexOf(':') + 1).split('+') : [];

export const specParts = (spec: string): string[] => (spec.startsWith('max:') ? spec.slice(4).split('|') : []);

export const buildSpec = (mode: string, disks: string[], parts: string[]): string => {
    if (mode.startsWith('disk_')) return disks.length ? `${mode}:${disks.join('+')}` : mode;
    return mode === 'max' ? `max:${parts.join('|')}` : mode;
};

export const specScripts = (spec: string): string[] => (spec.startsWith('max:') ? specParts(spec) : [spec]).filter(part => part.startsWith('script:'));

export const isMissing = (spec: string, missing: readonly string[] | null | undefined): boolean => {
    const own = spec === '' ? ['auto', ''] : [spec, ...specParts(spec)];
    return (missing ?? []).some(m => own.includes(m));
};

const draftOf = (cfg: FanConfigV2): Draft => ({
    cpu: { input: cfg.cpu.input === 'auto' ? '' : cfg.cpu.input, on_missing: cfg.cpu.on_missing },
    case: { input: cfg.case.input === 'auto' ? '' : cfg.case.input, on_missing: cfg.case.on_missing }
});

export const valuesOf = (draft: Draft): Record<string, string> =>
    Object.fromEntries(
        GROUPS.flatMap(group => [
            [`${group}_input`, draft[group].input || 'auto'],
            [`${group}_on_missing`, draft[group].on_missing]
        ])
    );

interface TestResult {
    reading: number | null;
    usable: boolean;
    detail: string;
    error: string;
}

function asTest(value: unknown): TestResult {
    const r = typeof value === 'object' && value !== null ? (value as Record<string, unknown>) : {};
    return {
        reading: typeof r.reading === 'number' ? r.reading : null,
        usable: r.usable === true,
        detail: typeof r.detail === 'string' ? r.detail : '',
        error: ''
    };
}

function create(): FanModule {
    let sync: ((status: StatusResponse) => void) | undefined;

    return {
        id: 'inputs',
        host: 'curves',
        order: 10,
        visible: status => Boolean(status.fan_caps && status.fan_caps.channels > 0 && status.settings.fan_config),
        render: ctx =>
            `<article class="unc-card unc-fan-inputs">
            ${ctx.ui.cardHead(ctx.t('inputs.title'), { ico: 'thermo' })}
            <p class="unc-muted">${escapeHtml(ctx.t('inputs.intro'))}</p>
            <div class="unc-fan-inputs-body" data-body></div>
            ${ctx.ui.saveDock('inputs', 'unc-fan-inputs-save')}
        </article>`,
        bind(ctx) {
            let draft: Draft | null = null;
            let data: FanInputsResponse | null = null;
            let loadError = '';
            let fetchedAt = 0;
            let pending = false;
            let lastHtml = '';
            const touched = new Set<string>();
            const tests = new Map<string, TestResult>();
            const testing = new Set<string>();
            const body = ctx.el.querySelector<HTMLElement>('[data-body]');
            if (!body) return;

            const config = (): FanConfigV2 | undefined => ctx.status()?.settings.fan_config;

            const refresh = (): void => {
                if (pending || Date.now() - fetchedAt < REFRESH_MS) return;
                pending = true;
                ctx.api
                    .get('fan_inputs')
                    .then(r => {
                        data = Array.isArray(r.options) ? r : null;
                        loadError = data ? '' : 'bad answer';
                    })
                    .catch((error: unknown) => {
                        loadError = errorMessage(error);
                    })
                    .finally(() => {
                        pending = false;
                        fetchedAt = Date.now();
                        ctx.redraw();
                    });
            };

            const specLabel = (spec: string): string => {
                const kind = spec === '' ? 'auto' : (spec.split(':')[0] ?? spec);
                const name = spec.includes(':') ? spec.slice(spec.indexOf(':') + 1) : '';
                const base = ctx.tEnum(`inputs.kind_${kind}`, kind, { name });
                return kind.startsWith('disk_') && name ? `${base} (${name.split('+').join(', ')})` : base;
            };

            const modeChoices = (options: FanInputOptionDetail[], mode: string): Choice[] => {
                const list: Choice[] = [{ value: 'auto', label: specLabel('') }];
                for (const o of options) {
                    if (o.kind === 'auto' || o.kind === 'max' || list.some(c => c.value === o.spec)) continue;
                    if (o.usable) list.push({ value: o.spec, label: specLabel(o.spec) });
                    else if (o.spec === mode) list.push({ value: o.spec, label: ctx.t('inputs.unavailable', { label: specLabel(o.spec) }), disabled: true });
                }
                if (!list.some(c => c.value === mode) && mode !== 'max')
                    list.push({ value: mode, label: ctx.t('inputs.unavailable', { label: specLabel(mode) }), disabled: true });
                if (mode === 'max' || components(options).length >= MAX_PARTS[0]) list.push({ value: 'max', label: specLabel('max') });
                return list;
            };

            const components = (options: FanInputOptionDetail[]): string[] =>
                options.filter(o => o.usable && COMPONENT.includes(o.kind) && !o.spec.includes('+')).map(o => o.spec);

            const readingOf = (group: Group, spec: string): number | null | undefined => {
                if (!data) return undefined;
                const state = data.groups[group];
                if (state && (state.spec === (spec || 'auto') || state.spec === spec)) return state.reading;
                return data.options.find(o => o.spec === (spec || 'auto'))?.reading;
            };

            const testRow = (spec: string): string => {
                const result = tests.get(spec);
                const text = testing.has(spec)
                    ? ctx.t('inputs.testing')
                    : !result
                      ? ''
                      : result.error
                        ? ctx.t('inputs.test_error', { error: result.error })
                        : result.usable && result.reading !== null
                          ? ctx.t('inputs.test_ok', { temp: result.reading })
                          : [ctx.t('inputs.test_fail'), result.detail].filter(Boolean).join(': ');
                return `<div class="unc-fan-inputs-test">
                ${ctx.ui.btn({ label: ctx.t('inputs.test'), ico: 'gauge', small: true, attrs: `data-fi-test="${escapeHtml(spec)}"${testing.has(spec) ? ' disabled' : ''}` })}
                <small class="unc-muted" aria-live="polite">${escapeHtml(text)}</small>
            </div>`;
            };

            const diskList = (group: Group, spec: string, disks: FanInputDisk[]): string => {
                const picked = new Set(specDisks(spec));
                const shown = disks.filter(d => DRIVE_NAME.test(d.name));
                const items = shown.map(d =>
                    ctx.ui.chipCheck(
                        `data-fi-disk="${escapeHtml(d.name)}" data-group="${group}"${!picked.has(d.name) && picked.size >= MAX_DRIVES ? ' disabled' : ''}`,
                        picked.has(d.name),
                        `${ctx.ui.glyph(ctx.ui.icon(d.nvme ? 'm2' : 'drives'), 'lg')}
                    <span class="unc-cc-name"><strong>${escapeHtml(d.name)}</strong><small class="unc-muted">${escapeHtml(d.device ?? '')}</small></span>
                    <span class="unc-cc-temp unc-muted" data-live-disk="${escapeHtml(d.name)}"></span>`
                    )
                );
                // saved names whose drive is not listed now stay in the spec and on the list
                for (const name of picked) {
                    if (!shown.some(d => d.name === name)) {
                        items.push(
                            ctx.ui.chipCheck(
                                `data-fi-disk="${escapeHtml(name)}" data-group="${group}"`,
                                true,
                                `<span class="unc-cc-name"><strong>${escapeHtml(name)}</strong></span>`
                            )
                        );
                    }
                }
                return `<div class="unc-fan-inputs-sub">
                <span class="unc-label">${escapeHtml(ctx.t('inputs.drives'))}</span>
                <div class="unc-ignore-list unc-fan-inputs-list">${items.join('') || ctx.ui.emptyText(ctx.t('inputs.no_drives'))}</div>
                <small class="unc-muted">${escapeHtml(ctx.t('inputs.drives_hint'))}</small>
            </div>`;
            };

            const partList = (group: Group, spec: string, options: FanInputOptionDetail[]): string => {
                const picked = specParts(spec);
                const all = [...components(options), ...picked.filter(p => !components(options).includes(p))];
                const items = all.map(part => {
                    const on = picked.includes(part);
                    const locked = (on && picked.length <= MAX_PARTS[0]) || (!on && picked.length >= MAX_PARTS[1]);
                    return `<div class="unc-fan-inputs-part">${ctx.ui.chipCheck(
                        `data-fi-part="${escapeHtml(part)}" data-group="${group}"${locked ? ' data-locked' : ''}`,
                        on,
                        `<span class="unc-cc-name"><strong>${escapeHtml(specLabel(part))}</strong></span><span class="unc-cc-temp unc-muted" data-live-part="${escapeHtml(part)}"></span>`
                    )}</div>`;
                });
                return `<div class="unc-fan-inputs-sub">
                <span class="unc-label">${escapeHtml(ctx.t('inputs.parts'))}</span>
                <div class="unc-ignore-list unc-fan-inputs-list">${items.join('')}</div>
                <small class="unc-muted">${escapeHtml(ctx.t('inputs.parts_hint', { min: MAX_PARTS[0], max: MAX_PARTS[1] }))}</small>
            </div>`;
            };

            const groupHtml = (group: Group, status: StatusResponse): string => {
                if (!draft) return '';
                const { input, on_missing } = draft[group];
                const mode = specMode(input);
                const options = data?.options ?? [];
                const scripts = specScripts(input);
                const missing = isMissing(input, status.daemon?.inputs_missing);
                const what = group === 'cpu' ? ctx.t('curves.cpu') : ctx.t('curves.case');
                return `<section class="unc-fan-inputs-group" data-group="${group}">
                ${ctx.ui.sectionHead(what)}
                ${
                    missing
                        ? ctx.ui.note(escapeHtml(ctx.t('inputs.missing_now', { specs: (status.daemon?.inputs_missing ?? []).join(', ') })), {
                              tone: 'warn',
                              box: true,
                              ico: 'alert'
                          })
                        : ''
                }
                <div class="unc-form">
                    <label for="unc-fi-${group}-src">${escapeHtml(ctx.t('inputs.source'))}</label>
                    <div class="unc-fan-inputs-pick">
                        ${selectHtml(`id="unc-fi-${group}-src" data-fi-mode data-group="${group}" data-ctl="${group}-src"`, modeChoices(options, mode), mode)}
                        <span class="unc-pill" data-live="${group}" hidden></span>
                    </div>
                </div>
                ${mode.startsWith('disk_') && data ? diskList(group, input, data.disks) : ''}
                ${mode === 'max' && data ? partList(group, input, options) : ''}
                ${scripts.map(testRow).join('')}
                <div class="unc-form">
                    <label for="unc-fi-${group}-miss">${escapeHtml(ctx.t('inputs.on_missing'))}</label>
                    ${selectHtml(
                        `id="unc-fi-${group}-miss" data-fi-missing data-group="${group}" data-ctl="${group}-miss"`,
                        ON_MISSING.map(value => ({ value, label: ctx.tEnum(`inputs.missing.${value}`, value) })),
                        on_missing
                    )}
                </div>
            </section>`;
            };

            const paintLive = (): void => {
                if (!draft) return;
                for (const group of GROUPS) {
                    const pill = body.querySelector<HTMLElement>(`[data-live="${group}"]`);
                    if (!pill) continue;
                    const reading = readingOf(group, draft[group].input);
                    pill.hidden = reading === undefined;
                    pill.textContent =
                        reading === undefined ? '' : reading === null ? ctx.t('inputs.no_reading') : ctx.t('inputs.now', { temp: Math.round(reading) });
                }
                body.querySelectorAll<HTMLElement>('[data-live-disk]').forEach(el => {
                    const disk = data?.disks.find(d => d.name === el.dataset.liveDisk);
                    el.textContent = !disk ? '' : disk.standby ? ctx.t('inputs.standby') : disk.temp === null ? '' : `${disk.temp} °C`;
                });
                body.querySelectorAll<HTMLElement>('[data-live-part]').forEach(el => {
                    const reading = data?.options.find(o => o.spec === el.dataset.livePart)?.reading;
                    el.textContent = reading == null ? '' : `${Math.round(reading)} °C`;
                });
            };

            const paint = (force = false): void => {
                const status = ctx.status();
                if (!status || !draft) return;
                const html = loadError
                    ? ctx.ui.note(escapeHtml(ctx.t('inputs.load_failed', { error: loadError })), { tone: 'warn', box: true, ico: 'alert', tag: 'div' })
                    : !data
                      ? ctx.ui.emptyText(ctx.t('inputs.loading'))
                      : GROUPS.map(group => groupHtml(group, status)).join('');
                if (force || html !== lastHtml) {
                    const focus = (document.activeElement as HTMLElement | null)?.dataset?.ctl;
                    body.innerHTML = html;
                    lastHtml = html;
                    if (focus) body.querySelector<HTMLElement>(`[data-ctl="${focus}"]`)?.focus();
                }
                paintLive();
            };

            const edit = (group: Group, field: 'input' | 'on_missing'): void => {
                touched.add(`${group}_${field}`);
                ctx.markDirty('inputs');
                paint(true);
            };

            const groupOf = (el: HTMLElement): Group | null => {
                const g = el.dataset.group;
                return g === 'cpu' || g === 'case' ? g : null;
            };

            ctx.el.addEventListener('change', event => {
                const el = event.target;
                if (!(el instanceof HTMLElement) || !draft) return;
                const group = groupOf(el);
                if (!group) return;

                if (el instanceof HTMLSelectElement && 'fiMode' in el.dataset) {
                    const mode = el.value;
                    const parts = mode === 'max' ? components(data?.options ?? []).slice(0, MAX_PARTS[0]) : [];
                    draft[group].input = mode === 'auto' ? '' : buildSpec(mode, [], parts);
                    edit(group, 'input');
                } else if (el instanceof HTMLSelectElement && 'fiMissing' in el.dataset) {
                    const value = ON_MISSING.find(v => v === el.value);
                    if (value) draft[group].on_missing = value;
                    edit(group, 'on_missing');
                } else if (el instanceof HTMLInputElement && 'fiDisk' in el.dataset) {
                    const name = el.dataset.fiDisk ?? '';
                    const picked = specDisks(draft[group].input).filter(n => n !== name);
                    if (el.checked) picked.push(name);
                    draft[group].input = buildSpec(specMode(draft[group].input), picked, []);
                    edit(group, 'input');
                } else if (el instanceof HTMLInputElement && 'fiPart' in el.dataset) {
                    const part = el.dataset.fiPart ?? '';
                    const current = specParts(draft[group].input);
                    const next = el.checked ? [...current, part] : current.filter(p => p !== part);
                    if (next.length >= MAX_PARTS[0] && next.length <= MAX_PARTS[1]) draft[group].input = buildSpec('max', [], next);
                    edit(group, 'input');
                }
            });

            ctx.el.addEventListener('click', event => {
                const el = event.target instanceof Element ? event.target : null;
                const test = el?.closest<HTMLElement>('[data-fi-test]');
                if (test) {
                    const spec = test.dataset.fiTest ?? '';
                    if (!spec || testing.has(spec)) return;
                    testing.add(spec);
                    paint(true);
                    ctx.api
                        .post('fan_input_test', { spec })
                        .then(answer => tests.set(spec, asTest(answer)))
                        .catch((error: unknown) => tests.set(spec, { ...asTest(null), error: errorMessage(error) }))
                        .finally(() => {
                            testing.delete(spec);
                            paint(true);
                        });
                    return;
                }

                if (el?.closest('[data-discard="inputs"]')) {
                    const cfg = config();
                    if (cfg) draft = draftOf(cfg);
                    touched.clear();
                    ctx.clearDirty();
                    paint(true);
                    return;
                }

                if (el?.closest('#unc-fan-inputs-save')) {
                    const cfg = config();
                    if (!cfg || !draft) return;
                    const diff = changes(valuesOf(draft), valuesOf(draftOf(cfg)), touched);
                    if (!Object.keys(diff).length) {
                        ctx.clearDirty();
                        return;
                    }
                    if (!Object.entries(diff).every(([key, value]) => validValue(key, value))) {
                        ctx.toast(ctx.t('inputs.invalid'), 'error');
                        return;
                    }
                    fetchedAt = 0;
                    void ctx.save(() => ctx.api.post('fan_v2', v2Params(diff)), ctx.t('save.done'));
                }
            });

            // update() hands over through this hook so the closure above sees the newest status and draft.
            sync = status => {
                const cfg = status.settings.fan_config;
                if (!cfg) return;
                if (!draft || !ctx.isDirty()) {
                    draft = draftOf(cfg);
                    touched.clear();
                }
                refresh();
                paint();
            };
        },
        update: status => sync?.(status)
    };
}

registerFanModule(create());
