// The Feedback view: which rows a model gets and the pre-filled GitHub issue. Pure functions, no DOM; the issue forms in
// .github/ISSUE_TEMPLATE are generated from the same constants (docs/tools/gen-templates.ts), so a field id is written once.
import registry from '../../../../backend/internal/nasctl/models.json';
import { CAPABILITIES, CAPABILITY_LABEL, type Capability } from '../../../models/schema';

export const ISSUES_NEW = 'https://github.com/ugreen-nas-control/ugreen-nas-control/issues/new';

/** Longest link the page opens. GitHub answers 414 above a server limit it does not publish; this stays well below. */
export const URL_LIMIT = 7500;

export type Answer = 'works' | 'fails' | 'untried';
export const ANSWERS: Answer[] = ['works', 'fails', 'untried'];
/** The words after the capability name in a line of the results field. */
export const ANSWER_LABEL: Record<Answer, string> = { works: 'works', fails: 'does not work', untried: 'not tried' };

export type Kind = 'model' | 'bug';
export const TEMPLATE: Record<Kind, string> = { model: 'model-report.yml', bug: 'bug.yml' };

/** Field ids of the issue forms. GitHub pre-fills text fields only, so the answers go into one textarea, a line per capability. */
export const FIELD = {
    model: 'model',
    versions: 'versions',
    summary: 'summary',
    notes: 'notes',
    what: 'what',
    results: 'results',
    diag: 'diag'
} as const;

export const DIAG_NOTE = 'Paste the copied diag here (Ctrl+V / Cmd+V).';

/** Capabilities with a row for this DMI product name: what the registry offers on the model, all of them for an unknown model. */
export function feedbackCaps(
    product: string,
    models: readonly { dmiPattern: string; verified: Record<string, { status: string }> }[] = registry.models
): Capability[] {
    const hit = models.find(m => new RegExp(m.dmiPattern).test(product));
    return hit ? CAPABILITIES.filter(c => hit.verified[c]?.status !== 'unsupported') : [...CAPABILITIES];
}

/** `@name` would notify someone and `#12` would link an issue: a zero-width joiner after the sign keeps the text and stops both. */
export const neutralise = (text: string): string => text.replace(/@(?=\w)/g, '@\u200d').replace(/#(?=\d)/g, '#\u200d');

export interface Report {
    kind: Kind;
    model: string;
    versions: string;
    caps: Capability[];
    answers: Partial<Record<Capability, Answer>>;
    /** Check summary of the collected data; empty before it ran. */
    summary: string;
    comment: string;
    /** The full diag is in the clipboard: the form's diag field says where to paste it. */
    hasDiag: boolean;
}

export interface Built {
    url: string;
    summaryDropped: boolean;
    commentShortened: boolean;
}

const SHORTENED = ' [shortened to fit the link]';

/** One line per capability, e.g. "LEDs: works". */
export const resultLines = (r: Report): string[] => r.caps.map(c => `${CAPABILITY_LABEL[c]}: ${ANSWER_LABEL[r.answers[c] ?? 'untried']}`);

function link(r: Report, summary: string, comment: string): string {
    const fields: [string, string][] = [['template', TEMPLATE[r.kind]]];
    const title = r.kind === 'model' ? `Model report: ${r.model}` : `Problem on ${r.model}`;
    fields.push(['title', title], [FIELD.model, r.model], [FIELD.versions, r.versions]);
    const note = neutralise(comment);
    fields.push([FIELD.results, resultLines(r).join('\n')]);
    if (summary) fields.push([FIELD.summary, summary]);
    if (note) fields.push([r.kind === 'model' ? FIELD.notes : FIELD.what, note]);
    if (r.hasDiag) fields.push([FIELD.diag, DIAG_NOTE]);
    // Pre-filling issue-form fields: https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/creating-an-issue
    // (query parameter template=<file> plus <field id>=<value>; a link above the server limit answers 414).
    return `${ISSUES_NEW}?${fields.map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&')}`;
}

/** The issue link, at most `limit` characters: the summary goes first, then the comment is cut (the full diag is pasted, never linked). */
export function buildIssueUrl(r: Report, limit = URL_LIMIT): Built {
    const summary = r.summary.trim();
    const comment = r.comment.trim();
    let url = link(r, summary, comment);
    if (url.length <= limit) return { url, summaryDropped: false, commentShortened: false };
    url = link(r, '', comment);
    if (url.length <= limit) return { url, summaryDropped: Boolean(summary), commentShortened: false };
    let lo = 0;
    let hi = comment.length;
    while (lo < hi) {
        const mid = Math.ceil((lo + hi) / 2);
        if (link(r, '', comment.slice(0, mid) + SHORTENED).length <= limit) lo = mid;
        else hi = mid - 1;
    }
    return { url: link(r, '', comment.slice(0, lo) + SHORTENED), summaryDropped: Boolean(summary), commentShortened: true };
}
