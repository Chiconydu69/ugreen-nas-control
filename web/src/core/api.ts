import type { ApiFailure } from './contract';

export type Param = string | number | boolean;
export type Params = Record<string, Param | undefined>;

export interface ClientConfig<Get extends object = object> {
    base: string;
    csrf: () => string;
    /** GETs through the shared poller (cached by URL for `maxAge`) or straight from the network. */
    reads: 'shared' | 'direct';
    badResponse?: (status: number) => string;
    failed?: (failure: Partial<ApiFailure>, status: number) => string;
    /** Checks of the top-level shape of a GET answer; a failed one is logged (once per action), nothing else changes. */
    checks?: { [A in keyof Get]?: (data: unknown) => boolean };
}

const httpStatus = (status: number): string => `HTTP ${status}`;

/**
 * One api.php: GET actions and POST actions with their parameters typed (see contract.ts). Every write drops
 * the shared GET cache before and after it, so a read that follows a write is always fresh.
 */
export function createClient<Get extends object, Post extends object, Reply extends object = Record<never, never>>(config: ClientConfig<Get>) {
    const badResponse = config.badResponse ?? httpStatus;
    const failed = config.failed ?? ((failure, status) => failure.error || httpStatus(status));

    async function send<R>(params: Params, post: boolean, maxAge: number): Promise<R> {
        const options: RequestInit = { credentials: 'same-origin' };
        let url = config.base;
        const entries = Object.entries(params).filter((entry): entry is [string, Param] => entry[1] !== undefined);

        if (post) {
            options.method = 'POST';
            // Unraid rejects POSTs without the WebGUI csrf_token before api.php runs; a client without one is read-only.
            const token = config.csrf();
            if (!token) throw new Error('No csrf_token: this client cannot write.');
            options.body = new URLSearchParams({
                ...Object.fromEntries(entries.map(([key, value]) => [key, String(value)])),
                csrf_token: token
            });
        } else {
            url += `?${new URLSearchParams(entries.map(([key, value]) => [key, String(value)]))}`;
        }

        let response: { ok: boolean; status: number };
        let text: string;

        if (post) {
            window.UNCPoll.invalidate();
            try {
                const raw = await fetch(url, options);
                response = { ok: raw.ok, status: raw.status };
                text = await raw.text();
            } finally {
                window.UNCPoll.invalidate();
            }
        } else if (config.reads === 'shared') {
            const polled = await window.UNCPoll.fetchText(url, maxAge);
            response = polled;
            text = polled.text;
        } else {
            const raw = await fetch(url, options);
            response = { ok: raw.ok, status: raw.status };
            text = await raw.text();
        }

        let data: R & Partial<ApiFailure>;

        try {
            data = JSON.parse(text);
        } catch {
            throw new Error(badResponse(response.status));
        }
        if (typeof data !== 'object' || data === null) throw new Error(badResponse(response.status));

        if (!response.ok || data.ok === false) throw new Error(failed(data, response.status));
        return data;
    }

    const warned = new Set<string>();

    async function read<A extends keyof Get & string>(action: A, params: Params, maxAge: number): Promise<Get[A]> {
        const data = await send<Get[A]>({ action, ...params }, false, maxAge);
        const check = config.checks?.[action];

        if (check && !check(data) && !warned.has(action)) {
            warned.add(action);
            console.warn(`[UGREEN NAS] unexpected shape of the "${action}" answer from ${config.base}`);
        }
        return data;
    }

    return {
        get: <A extends keyof Get & string>(action: A, params: Params = {}, maxAge = 0): Promise<Get[A]> => read(action, params, maxAge),
        post: <A extends keyof Post & string>(action: A, params: Post[A]): Promise<A extends keyof Reply ? Reply[A] : { ok: true }> =>
            send({ action, ...(params as Params) }, true, 0)
    };
}

export type Client<Get extends object, Post extends object, Reply extends object = Record<never, never>> = ReturnType<typeof createClient<Get, Post, Reply>>;

export const errorMessage = (error: unknown): string => (error instanceof Error ? error.message : String(error));
