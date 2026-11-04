// The markup components of the pages: pure functions that return html (escaped). One CSS family each (.unc-<name> in
// plugin/css/app.css). Used by app.ts and by the fan modules (fan/module.ts).
import { rangeFill } from '../../../core/format';
import { escapeHtml } from '../../../core/html';
import { NAS_ICONS } from '../../../core/icons';
import { t } from '../text';

const ICONS = NAS_ICONS;

export type IconName = keyof typeof ICONS;
export const icon = (name: IconName): string => `<svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[name]}</svg>`;
export const iconOf = (name: string | null | undefined): string => `<svg viewBox="0 0 24 24" aria-hidden="true">${ICONS[name as IconName] || ''}</svg>`;

export type Ico = IconName | '';
export const cardHead = (
    title: string,
    { ico = '', actions = '', tag = 'div', titleId = '' }: { ico?: Ico; actions?: string; tag?: string; titleId?: string } = {}
): string => `
    <${tag} class="unc-card-head">
        <h2>${ico ? `<span class="unc-ico">${icon(ico)}</span>` : ''}<span${titleId ? ` id="${titleId}"` : ''}>${escapeHtml(title)}</span></h2>${actions}
    </${tag}>`;

// A section inside a card. With actions it is a row with the action on the right; id goes to the h3 (aria-labelledby).
export const sectionHead = (title: string, { actions = '', id = '' }: { actions?: string; id?: string } = {}): string => {
    const h3 = `<h3 class="unc-sec"${id ? ` id="${id}"` : ''}>${escapeHtml(title)}</h3>`;

    return actions ? `<div class="unc-sec-head">${h3}${actions}</div>` : h3;
};

export const pill = ({
    tone = 'plain',
    text,
    sub = '',
    ico = '',
    disc = false,
    lg = false,
    tag = 'span',
    attrs = ''
}: {
    tone?: string;
    text: string;
    sub?: string;
    ico?: Ico;
    disc?: boolean;
    lg?: boolean;
    tag?: string;
    attrs?: string;
}): string => `
    <${tag} class="unc-pill is-${tone}${disc ? ' is-disc' : ''}${lg ? ' is-lg' : ''}"${tag === 'button' ? ' type="button"' : ''} ${attrs}>${ico ? `<span class="unc-ico">${icon(ico)}</span>` : ''}<span>${escapeHtml(text)}</span>${sub ? `<small>${escapeHtml(sub)}</small>` : ''}</${tag}>`;

// html is already escaped. tone: info warn bad violet; box makes it a panel; ico puts an icon before the content.
export const note = (
    html: string,
    { tone = 'info', box = false, ico = '', tag = 'p', attrs = '' }: { tone?: string; box?: boolean; ico?: Ico; tag?: string; attrs?: string } = {}
): string => `
    <${tag} class="unc-note is-${tone}${box ? ' is-box' : ''}${ico ? ' is-icon' : ''}" ${attrs}>${ico ? `<span class="unc-ico">${icon(ico)}</span><div>${html}</div>` : html}</${tag}>`;

export const emptyText = (text: string, cls = ''): string => `<p class="unc-empty ${cls ? `is-${cls}` : ''}">${escapeHtml(text)}</p>`;

export const meter = (pct: number, tone = ''): string => `<span class="unc-meter${tone ? ` is-${tone}` : ''}"><span style="width:${pct}%"></span></span>`;

// tone: primary or secondary (default); small sits in card heads. attrs is html, already escaped.
export const btn = ({
    label,
    ico = '',
    tone = '',
    small = false,
    attrs = ''
}: {
    label: string;
    ico?: Ico;
    tone?: string;
    small?: boolean;
    attrs?: string;
}): string =>
    `<button type="button" class="unc-btn${tone === 'primary' ? ' primary' : ''}${small ? ' small' : ''}" ${attrs}>${ico ? `<span class="unc-ico">${icon(ico)}</span>` : ''}<span>${escapeHtml(label)}</span></button>`;

export const linkBtn = (label: string, attrs = ''): string => `<button type="button" class="unc-linkbtn" ${attrs}>${escapeHtml(label)}</button>`;

// hint: text; hintId: an empty small the page fills later; cls: extra class on the row.
export const switchRow = ({
    id,
    title,
    hint = '',
    hintId = '',
    cls = '',
    attrs = ''
}: {
    id: string | null;
    title: string;
    hint?: string;
    hintId?: string;
    cls?: string;
    attrs?: string;
}): string => `
    <label class="unc-switch-row${cls ? ` ${cls}` : ''}" ${attrs}>
        <span class="unc-switch"><input type="checkbox" id="${id}"><span></span></span>
        <span><strong>${escapeHtml(title)}</strong><small${hintId ? ` id="${hintId}"` : ''}>${escapeHtml(hint)}</small></span>
    </label>`;

// A slider with its value in a box; showValue / the page keeps `#<id>-value` current. derived: no slider, a calculated
// value (dashed box) and a small text `derived` (an id) in front of it.
export const rangeField = ({
    id,
    min = 0,
    max = 100,
    step = 1,
    value,
    unit,
    derived = '',
    labelledby = ''
}: {
    id: string | null;
    min?: string | number | null;
    max?: string | number | null;
    step?: string | number;
    value: string | number | null;
    unit: string | null;
    derived?: string;
    labelledby?: string;
}): string => `
    <div class="unc-range">
        ${derived ? `<small class="unc-lim-derived" id="${derived}"></small>` : `<input type="range" id="${id}" min="${min}" max="${max}" step="${step}" value="${value}" style="--fill:${rangeFill(Number(value), Number(min), Number(max))}"${labelledby ? ` aria-labelledby="${labelledby}"` : ''}>`}
        <output class="unc-value${derived ? ' is-derived' : ''}" id="${id}-value">${value} ${unit}</output>
    </div>`;

export const saveBar = (name: string | null, saveId: string | null): string => `
    <div class="unc-savebar" data-savebar="${name}">
        <span class="unc-savestate">${escapeHtml(t('save.clean'))}</span>
        ${btn({ label: t('save.discard'), attrs: `data-discard="${name}" disabled` })}
        ${btn({ label: t('common.save'), ico: 'check', tone: 'primary', attrs: `id="${saveId}" disabled` })}
    </div>`;

export const linkRow = (...items: string[]): string => `<div class="unc-links">${items.join('')}</div>`;

// markup: an icon (icon(), a fan, the Unraid mark); cls: lg for rows, warn for an alert.
export const glyph = (markup: string, cls = ''): string => `<span class="unc-glyph ${cls ? `is-${cls}` : ''}">${markup}</span>`;

export const chipCheck = (attr: string, checked: boolean, html: string): string => `
    <label class="unc-chipcheck"><input type="checkbox" ${attr} ${checked ? 'checked' : ''}><i class="unc-chipcheck-box"></i><span>${html}</span></label>`;

// The save bar in its dock (it floats above Unraid's footer when the card is taller than the window).
export const saveDock = (name: string, saveId: string): string => `<div class="unc-savedock">${saveBar(name, saveId)}</div>`;
