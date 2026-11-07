import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import registry from '../../../../backend/internal/nasctl/models.json';
import { CAPABILITIES, CAPABILITY_LABEL } from '../../../models/schema';
import { en } from './i18n/en';
import { ANSWER_LABEL, buildIssueUrl, feedbackCaps, FIELD, neutralise, type Report, resultLines, TEMPLATE, URL_LIMIT } from './feedback';

const base: Report = {
    kind: 'model',
    model: 'DXP4800 Plus',
    versions: 'Unraid 7.1.4, plugin 5.3.1, kernel 6.12',
    caps: ['leds', 'fans', 'case', 'sensors'],
    answers: { leds: 'works', fans: 'fails' },
    summary: 'LED controller found, fan chip found',
    comment: 'looks fine'
};
const query = (url: string): URLSearchParams => new URL(url).searchParams;
const form = (kind: 'model' | 'bug'): string => readFileSync(join(import.meta.dir, '../../../../.github/ISSUE_TEMPLATE', TEMPLATE[kind]), 'utf8');
const ids = (text: string): string[] => [...text.matchAll(/^\s+id: (\S+)$/gm)].map(m => m[1] as string);

test('the issue link carries the template, the model, the answers, the summary; the diag field stays empty', () => {
    const { url } = buildIssueUrl(base);
    const q = query(url);
    expect(url.startsWith('https://github.com/ugreen-nas-control/ugreen-nas-control/issues/new?template=model-report.yml&')).toBe(true);
    expect(q.get(FIELD.model)).toBe('DXP4800 Plus');
    expect(q.get(FIELD.results)).toBe('LEDs: works\nFan control: does not work\nCase view: not tried\nSensors: not tried');
    expect(q.get(FIELD.summary)).toBe(base.summary);
    expect(q.has(FIELD.diag)).toBe(false);
});

test('the bug link keeps the same data in the bug form fields', () => {
    const q = query(buildIssueUrl({ ...base, kind: 'bug' }).url);
    expect(q.get('template')).toBe('bug.yml');
    expect(q.get(FIELD.results)).toContain('LEDs: works');
    expect(q.get(FIELD.summary)).toBe(base.summary);
    expect(q.get(FIELD.what)).toBe('looks fine');
});

test('@ and # in the comment neither notify nor link', () => {
    expect(neutralise('thanks @alex, see #12 and a@b')).toBe('thanks @\u200dalex, see #\u200d12 and a@\u200db');
    expect(neutralise('# heading-less, 5 @ home')).toBe('# heading-less, 5 @ home');
});

test('over the limit the summary goes first, then the comment is cut with a notice', () => {
    const summary = 'x'.repeat(600);
    const mid = buildIssueUrl({ ...base, summary, comment: 'y'.repeat(600) }, 1100);
    expect(mid.summaryDropped).toBe(true);
    expect(mid.url.length).toBeLessThanOrEqual(1100);
    const cut = buildIssueUrl({ ...base, summary, comment: 'y'.repeat(600) }, 700);
    expect(cut.commentShortened).toBe(true);
    expect(cut.url.length).toBeLessThanOrEqual(700);
    expect(decodeURIComponent(cut.url)).toContain('[shortened to fit the link]');
});

test('the worst case stays under the limit', () => {
    const worst = buildIssueUrl({ ...base, summary: '@'.repeat(600), comment: '@'.repeat(600), model: 'M'.repeat(60) });
    expect(worst.url.length).toBeLessThanOrEqual(URL_LIMIT);
});

test('a model gets a row for each capability the registry offers, an unknown model for all', () => {
    for (const m of registry.models) {
        const caps = feedbackCaps(m.fixtureDmi);
        for (const c of CAPABILITIES) expect(caps.includes(c)).toBe(m.verified[c].status !== 'unsupported');
    }
    expect(feedbackCaps('Something else')).toEqual(CAPABILITIES);
});

test('every registry capability has a row text and a results line, and the forms have the fields the link fills', () => {
    const lines = resultLines({ ...base, caps: CAPABILITIES, answers: {} });
    expect(lines).toHaveLength(CAPABILITIES.length);
    CAPABILITIES.forEach((c, i) => expect(lines[i]).toBe(`${CAPABILITY_LABEL[c]}: ${ANSWER_LABEL.untried}`));
    for (const c of CAPABILITIES) expect(en[`feedback.cap_${c}` as keyof typeof en]).toBeTruthy();
    expect(
        Object.keys(en)
            .filter(k => k.startsWith('feedback.cap_'))
            .sort()
    ).toEqual(CAPABILITIES.map(c => `feedback.cap_${c}`).sort());
    for (const kind of ['model', 'bug'] as const) {
        const have = ids(form(kind));
        for (const id of [FIELD.model, FIELD.versions, FIELD.results, FIELD.summary, FIELD.diag, kind === 'model' ? FIELD.notes : FIELD.what])
            expect(have).toContain(id);
        expect(have.filter(id => id.startsWith('cap_'))).toEqual([]);
    }
    expect(form('model')).toMatch(/id: results[\s\S]*?required: true/);
});

test('the model and bug forms need the device data and tell where it comes from; the report field is gone', () => {
    const model = form('model');
    expect(model).toMatch(/id: diag[\s\S]*?placeholder: [\s\S]*?required: true/);
    expect(model).not.toContain('id: report');
    expect(form('bug')).not.toContain('id: report');
    expect(form('bug')).toMatch(/id: diag[\s\S]*?placeholder: "Paste the copied device data, or write why it's not possible\."[\s\S]*?required: true/);
});
