import { afterEach, beforeEach, expect, test } from 'bun:test';
import { createClient, errorMessage } from './api';

interface Seen {
    url: string;
    init?: RequestInit | undefined;
}

let seen: Seen[] = [];
let invalidated = 0;
let reply: { status: number; body: string } = { status: 200, body: '{"ok":true}' };
const realFetch = globalThis.fetch;
const win = globalThis as unknown as { window?: unknown };

beforeEach(() => {
    seen = [];
    invalidated = 0;
    reply = { status: 200, body: '{"ok":true}' };
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
        seen.push({ url, init });
        return new Response(reply.body, { status: reply.status });
    }) as unknown as typeof fetch;
    win.window = {
        UNCPoll: {
            invalidate: () => {
                invalidated++;
            },
            fetchText: async (url: string) => {
                seen.push({ url });
                return { ok: reply.status < 400, status: reply.status, text: reply.body, rx: 0 };
            }
        }
    };
});

afterEach(() => {
    globalThis.fetch = realFetch;
    win.window = undefined;
});

interface Get {
    status: { ok: boolean; n: number };
}
interface Post {
    mode: { value: string };
}

test('POST sends the action, the parameters and the csrf token, and drops the shared cache around it', async () => {
    const api = createClient<Get, Post>({ base: '/api.php', csrf: () => 'tok', reads: 'direct' });

    await api.post('mode', { value: 'quiet' });

    expect(invalidated).toBe(2);
    expect(seen[0]?.url).toBe('/api.php');
    expect(seen[0]?.init?.method).toBe('POST');
    expect(String(seen[0]?.init?.body)).toBe('action=mode&value=quiet&csrf_token=tok');
});

test('direct GET asks the network, shared GET goes through the poller', async () => {
    reply = { status: 200, body: '{"ok":true,"n":3}' };

    const direct = createClient<Get, Post>({ base: '/api.php', csrf: () => '', reads: 'direct' });
    expect((await direct.get('status')).n).toBe(3);
    expect(seen[0]?.url).toBe('/api.php?action=status');
    expect(seen[0]?.init?.credentials).toBe('same-origin');

    const shared = createClient<Get, Post>({ base: '/api.php', csrf: () => '', reads: 'shared' });
    expect((await shared.get('status', { led: 'disk1' }, 5000)).n).toBe(3);
    expect(seen[1]).toEqual({ url: '/api.php?action=status&led=disk1' });
    expect(invalidated).toBe(0);
});

test('a refused request throws the API message, else the HTTP status', async () => {
    const api = createClient<Get, Post>({ base: '/api.php', csrf: () => 'tok', reads: 'direct' });

    reply = { status: 400, body: '{"ok":false,"error":"Invalid mode"}' };
    await expect(api.post('mode', { value: 'x' })).rejects.toThrow('Invalid mode');

    reply = { status: 500, body: '{"ok":false}' };
    await expect(api.get('status')).rejects.toThrow('HTTP 500');

    reply = { status: 200, body: '{"ok":false,"error":"nope"}' };
    await expect(api.get('status')).rejects.toThrow('nope');
});

test('a body that is not JSON throws the status', async () => {
    const api = createClient<Get, Post>({ base: '/api.php', csrf: () => '', reads: 'direct' });

    reply = { status: 502, body: '<html>Bad gateway</html>' };
    await expect(api.get('status')).rejects.toThrow('HTTP 502');
});

test('a body that is JSON but not an object is a bad response', async () => {
    const api = createClient<Get, Post>({ base: '/api.php', csrf: () => 'tok', reads: 'direct' });

    reply = { status: 200, body: 'null' };
    await expect(api.get('status')).rejects.toThrow('HTTP 200');
    reply = { status: 200, body: '3' };
    await expect(api.get('status')).rejects.toThrow('HTTP 200');
});

test('a POST without csrf token throws before any request; a failing POST still drops the cache', async () => {
    const readOnly = createClient<Get, Post>({ base: '/api.php', csrf: () => '', reads: 'direct' });
    await expect(readOnly.post('mode', { value: 'x' })).rejects.toThrow('csrf_token');
    expect(seen.length).toBe(0);

    const api = createClient<Get, Post>({ base: '/api.php', csrf: () => 'tok', reads: 'direct' });
    globalThis.fetch = (async () => {
        throw new TypeError('network');
    }) as unknown as typeof fetch;
    await expect(api.post('mode', { value: 'x' })).rejects.toThrow('network');
    expect(invalidated).toBe(2);
});

test('the messages can be replaced', async () => {
    const api = createClient<Get, Post>({
        base: '/api.php',
        csrf: () => '',
        reads: 'direct',
        badResponse: status => `bad ${status}`,
        failed: (failure, status) => `failed ${failure.error} ${status}`
    });

    reply = { status: 502, body: 'x' };
    await expect(api.get('status')).rejects.toThrow('bad 502');
    reply = { status: 400, body: '{"ok":false,"error":"e"}' };
    await expect(api.get('status')).rejects.toThrow('failed e 400');
});

test('errorMessage', () => {
    expect(errorMessage(new Error('boom'))).toBe('boom');
    expect(errorMessage('text')).toBe('text');
});
