import { beforeAll, describe, expect, test } from 'bun:test';
import { missingActions, missingErrors, missingStatus } from './coverage';
import { diff, loadCases, loadGolden, replay, type Result } from './oracle';

const table = loadCases();
const golden = loadGolden();
const php = Bun.which('php');

test('every action of both PHP APIs has a case', () => {
    expect(missingActions(table.cases)).toEqual([]);
});

test('every error message of the PHP appears in a golden reply', () => {
    expect(missingErrors(golden)).toEqual([]);
});

test('every status code of an API has a case', () => {
    expect(missingStatus(table.cases, golden)).toEqual([]);
});

test('cases and goldens list the same ids, ids are unique', () => {
    const ids = table.cases.map(c => c.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(Object.keys(golden)).toEqual(ids);
});

describe.skipIf(!php)('the real PHP answers as recorded', () => {
    let results: Record<string, Result> = {};
    beforeAll(async () => {
        results = await replay();
    }, 300_000);
    for (const c of table.cases) test(c.id, () => expect(diff(golden[c.id]!, results[c.id]!)).toEqual([]));
});
