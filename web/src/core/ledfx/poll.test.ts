import { afterEach, beforeEach, expect, test } from 'bun:test';
import { poll } from './poll';

const realFetch = globalThis.fetch;
const doc = globalThis as unknown as { document?: unknown };
let calls = 0;
let body = '{"ok":true}';

beforeEach(() => {
    calls = 0;
    body = '{"ok":true}';
    poll.invalidate();
    poll.lastPhaseRx = null;
    globalThis.fetch = (async () => {
        calls++;
        return new Response(body);
    }) as unknown as typeof fetch;
    doc.document = { visibilityState: 'visible' };
});

afterEach(() => {
    globalThis.fetch = realFetch;
    doc.document = undefined;
});

test('a GET is shared while it is younger than maxAge, and always asked with maxAge 0', async () => {
    await Promise.all([poll.fetchText('/a', 5000), poll.fetchText('/a', 5000)]);
    await poll.fetchText('/a', 5000);
    expect(calls).toBe(1);

    await poll.fetchText('/a');
    expect(calls).toBe(2);

    await poll.fetchText('/b', 5000);
    expect(calls).toBe(3);
});

test('a write drops the cache', async () => {
    await poll.fetchText('/a', 5000);
    poll.invalidate();
    await poll.fetchText('/a', 5000);
    expect(calls).toBe(2);
});

test('json parses the body', async () => {
    body = '{"n":3}';
    expect(await poll.json<{ n: number }>('/j')).toEqual({ n: 3 });
});

test('the receive time of an answer that carries a LED phase is remembered', async () => {
    await poll.fetchText('/plain');
    expect(poll.lastPhaseRx).toBeNull();

    body = '{"now_ms":5}';
    const answer = await poll.fetchText('/phase');
    expect(poll.lastPhaseRx).toBe(answer.rx);
});

test('a failed request is not cached', async () => {
    globalThis.fetch = (async () => {
        calls++;
        throw new Error('down');
    }) as unknown as typeof fetch;

    await expect(poll.fetchText('/x', 5000)).rejects.toThrow('down');
    await expect(poll.fetchText('/x', 5000)).rejects.toThrow('down');
    expect(calls).toBe(2);
});

test('every registers a task and returns a handle to run it now', async () => {
    let runs = 0;
    const task = poll.every(() => {
        runs++;
    }, 60_000);

    task.poke();
    task.poke();
    await new Promise(resolve => setTimeout(resolve, 5));
    expect(runs).toBeGreaterThanOrEqual(1);
});
