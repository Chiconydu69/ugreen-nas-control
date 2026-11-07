// The Feedback view: answers per capability, a background diag job, and the pre-filled GitHub issue (feedback.ts).
import { errorMessage } from '../../core/api';
import type { Client } from '../../core/api';
import type { DiagResult, NasGet, NasPost, NasPostReply } from '../../core/contract';
import { CAPABILITIES, type Capability } from '../../../models/schema';
import { ANSWERS, type Answer, buildIssueUrl, feedbackCaps, type Kind, type Report } from './feedback';
import type { Key } from './i18n/en';

const POLL_MS = 2000;

export interface FeedbackCtx {
    root: HTMLElement;
    api: Client<NasGet, NasPost, NasPostReply>;
    t: (key: Key, vars?: Record<string, string | number>) => string;
    /** DMI product name; empty until the status arrived. */
    product: () => string;
    /** Versions as the status reported them, used until the diag has its own. */
    versions: () => { plugin: string; unraid: string; kernel: string };
    toast: (message: string, type?: string) => void;
}

/** Copies in the click handler: the async API where the page is a secure context, else the old select-and-copy (Unraid is often plain http). */
export function copyText(text: string): Promise<boolean> {
    const legacy = (): boolean => {
        const area = document.createElement('textarea');
        area.value = text;
        area.setAttribute('readonly', '');
        area.style.cssText = 'position:fixed;top:0;left:0;opacity:0';
        document.body.appendChild(area);
        area.select();
        try {
            return document.execCommand('copy');
        } catch {
            return false;
        } finally {
            area.remove();
        }
    };
    try {
        if (navigator.clipboard?.writeText)
            return navigator.clipboard.writeText(text).then(
                () => true,
                () => legacy()
            );
    } catch {
        /* falls through */
    }
    return Promise.resolve(legacy());
}

export function createFeedback(ctx: FeedbackCtx) {
    const { root, api, t, toast } = ctx;
    const q = <T extends HTMLElement = HTMLElement>(selector: string): T => {
        const el = root.querySelector<T>(selector);
        if (!el) throw new Error(`Missing ${selector}`);
        return el;
    };
    const s = {
        answers: {} as Partial<Record<Capability, Answer>>,
        phase: 'idle' as 'idle' | 'running' | 'done' | 'error',
        elapsed: 0,
        error: '',
        result: null as DiagResult | null,
        timer: 0 as number | undefined,
        note: '',
        copyFailed: false
    };
    const ANSWER_KEY = { works: 'feedback.ans_works', fails: 'feedback.ans_fails', untried: 'feedback.ans_untried' } as const satisfies Record<Answer, Key>;
    const capKey = (c: Capability): Key => `feedback.cap_${c}` as Key;
    const label = (id: string, key: Key): void => {
        const text = q(id).querySelector('span:last-child');
        if (text) text.textContent = t(key);
    };

    function render(): void {
        const caps = feedbackCaps(ctx.product());
        q('#unc-fb-intro').textContent = t('feedback.intro_device', { model: ctx.product() || 'UGREEN NAS' });
        const el = (tag: string, cls: string, text = ''): HTMLElement => {
            const node = document.createElement(tag);
            node.className = cls;
            node.textContent = text;
            return node;
        };
        q('#unc-fb-rows').replaceChildren(
            ...caps.map(c => {
                const row = el('div', 'unc-fb-row');
                const opts = el('div', 'unc-fb-opts');
                opts.setAttribute('role', 'radiogroup');
                opts.setAttribute('aria-label', t(capKey(c)));
                row.dataset.fbRow = c;
                row.append(el('span', '', t(capKey(c))), opts);
                for (const a of ANSWERS) {
                    const input = document.createElement('input');
                    input.type = 'radio';
                    input.name = `fb-${c}`;
                    input.value = a;
                    input.dataset.cap = c;
                    input.checked = (s.answers[c] ?? 'untried') === a;
                    const choice = el('label', 'unc-fb-opt');
                    choice.append(input, el('span', '', t(ANSWER_KEY[a])));
                    opts.append(choice);
                }
                return row;
            })
        );

        const status = q('#unc-fb-status');
        status.hidden = s.phase === 'idle' || s.phase === 'done';
        status.textContent =
            s.phase === 'running'
                ? t('feedback.running', { sec: Math.floor(s.elapsed / 1000) })
                : s.phase === 'error'
                  ? t('feedback.error', { error: s.error })
                  : '';
        const running = s.phase === 'running';
        q<HTMLButtonElement>('#unc-fb-collect').disabled = running;
        label('#unc-fb-collect', s.phase === 'done' ? 'feedback.collect_again' : s.phase === 'error' ? 'feedback.retry' : 'feedback.collect');
        q('#unc-fb-result').hidden = !s.result;
        q('#unc-fb-summary').textContent = s.result?.summary ?? '';
        q('#unc-fb-diag').textContent = s.result?.diag_markdown ?? '';
        const note = q('#unc-fb-note');
        note.hidden = !s.note;
        note.textContent = s.note;
        q('#unc-fb-fallback').hidden = !s.copyFailed;
    }

    async function poll(job: string): Promise<void> {
        try {
            const r = await api.get('diag_status', { job });
            if (r.state === 'running') {
                s.elapsed = r.elapsed_ms;
                s.timer = window.setTimeout(() => void poll(job), POLL_MS);
            } else if (r.state === 'done') {
                s.phase = 'done';
                s.result = r.result;
                s.note = '';
                s.copyFailed = false;
            } else {
                s.phase = 'error';
                s.error = r.error;
            }
        } catch (error) {
            s.phase = 'error';
            s.error = errorMessage(error);
        }
        render();
    }

    async function collect(): Promise<void> {
        if (s.phase === 'running') return;
        clearTimeout(s.timer);
        s.phase = 'running';
        s.elapsed = 0;
        s.result = null;
        s.note = '';
        s.copyFailed = false;
        render();
        try {
            const { job } = await api.post('diag_start', {});
            s.timer = window.setTimeout(() => void poll(job), POLL_MS);
        } catch (error) {
            s.phase = 'error';
            s.error = errorMessage(error);
            render();
        }
    }

    function report(kind: Kind): Report {
        const v = s.result?.versions ?? ctx.versions();
        return {
            kind,
            model: s.result?.model.name || ctx.product() || 'unknown',
            versions: `Unraid ${v.unraid || '?'}, plugin ${v.plugin || '?'}, kernel ${v.kernel || '?'}`,
            caps: feedbackCaps(ctx.product()),
            answers: s.answers,
            summary: s.result?.summary ?? '',
            comment: q<HTMLTextAreaElement>('#unc-fb-comment').value,
            hasDiag: Boolean(s.result)
        };
    }

    // Both calls start inside the click: the copy first, the window right after, then the result of the copy is awaited.
    async function send(kind: Kind): Promise<void> {
        const built = buildIssueUrl(report(kind));
        const diag = s.result?.diag_markdown ?? '';
        const copying = diag ? copyText(diag) : Promise.resolve(true);
        window.open(built.url, '_blank', 'noopener');
        const ok = await copying;
        s.copyFailed = Boolean(diag) && !ok;
        s.note = [
            !diag ? t('feedback.no_data') : ok ? t('feedback.copied') : t('feedback.copy_failed'),
            built.summaryDropped ? t('feedback.summary_dropped') : '',
            built.commentShortened ? t('feedback.comment_cut') : ''
        ]
            .filter(Boolean)
            .join(' ');
        render();
    }

    function download(): void {
        const url = URL.createObjectURL(new Blob([s.result?.diag_markdown ?? ''], { type: 'text/markdown' }));
        const a = document.createElement('a');
        a.href = url;
        a.download = 'diag.md';
        a.click();
        URL.revokeObjectURL(url);
    }

    function bind(): void {
        q('#unc-fb-rows').addEventListener('change', event => {
            const input = event.target as HTMLInputElement;
            const cap = input.dataset.cap as Capability | undefined;
            if (cap && (CAPABILITIES as string[]).includes(cap)) s.answers[cap] = input.value as Answer;
        });
        q('#unc-fb-collect').addEventListener('click', () => void collect());
        q('#unc-fb-issue').addEventListener('click', () => void send('model'));
        q('#unc-fb-bug').addEventListener('click', () => void send('bug'));
        q('#unc-fb-download').addEventListener('click', download);
        q('#unc-fb-copy').addEventListener('click', () => {
            void copyText(s.result?.diag_markdown ?? '').then(ok => (ok ? toast(t('feedback.copied')) : toast(t('feedback.copy_failed'), 'error')));
        });
    }

    return { render, bind };
}
