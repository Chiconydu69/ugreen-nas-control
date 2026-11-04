/*
 * Polling shared by the pages that load this file (NAS page, LED pages inside it, Dashboard tile).
 * The NAS is small and every poll forks processes on it, so:
 * - nothing runs while the tab is hidden or after IDLE_MS without input (pointer, key, scroll, focus);
 *   the first input or the tab coming back refreshes every task at once;
 * - a task never starts while its previous run is still in flight;
 * - GETs are shared: one request per URL while it is in flight or younger than the caller's maxAge,
 *   whichever app asked first; any write drops the cache so a read after a write is always fresh.
 */
import type { PollApi } from '../globals';

const IDLE_MS = 5 * 60 * 1000;
const TIMEOUT_MS = 15000;
const INPUTS = ['pointerdown', 'pointermove', 'keydown', 'wheel', 'scroll', 'touchstart', 'focus'];

interface Task {
    fn: () => unknown;
    ms: () => number;
    when: () => boolean;
    last: number;
    busy: boolean;
}

interface Fetched {
    ok: boolean;
    status: number;
    text: string;
    rx: number;
}

interface CacheEntry {
    pending: boolean;
    at: number;
    promise: Promise<Fetched>;
}

const tasks: Task[] = [];
const cache = new Map<string, CacheEntry>();
let lastInput = Date.now();
let asleep = false;
let idleMs = IDLE_MS;

const awake = (): boolean => document.visibilityState !== 'hidden' && Date.now() - lastInput < idleMs;

function run(task: Task): void {
    if (task.busy) return;
    task.busy = true;
    Promise.resolve()
        .then(task.fn)
        .catch(() => {})
        .finally(() => {
            task.busy = false;
            task.last = Date.now();
        });
}

function tick(): void {
    if (!awake()) {
        asleep = true;
        return;
    }
    if (asleep) {
        asleep = false;
        tasks.forEach(task => {
            task.last = 0;
        });
    }
    const now = Date.now();
    for (const task of tasks) {
        if (now - task.last >= task.ms() && task.when()) run(task);
    }
}

function input(): void {
    lastInput = Date.now();
    if (asleep) tick();
}

// Raw GET { ok, status, text, rx }. maxAge 0 always asks the server. rx = performance.now() when the
// response was received (also for cache hits), so phases can be advanced from the real receive time.
// Aborts after TIMEOUT_MS so a hung request cannot block its poll task forever.
function fetchText(url: string, maxAge = 0): Promise<Fetched> {
    const hit = cache.get(url);
    const share = maxAge > 0 && hit && (hit.pending || Date.now() - hit.at < maxAge);
    let promise: Promise<Fetched> | null = share ? hit.promise : null;

    if (!promise) {
        const abort = new AbortController();
        const timer = setTimeout(() => abort.abort(), TIMEOUT_MS);
        const entry: CacheEntry = {
            pending: true,
            at: Date.now(),
            promise: fetch(url, { credentials: 'same-origin', signal: abort.signal })
                .then(async response => ({ ok: response.ok, status: response.status, text: await response.text() }))
                .then(
                    result => {
                        entry.pending = false;
                        entry.at = Date.now();
                        return { ...result, rx: performance.now() };
                    },
                    error => {
                        if (cache.get(url) === entry) cache.delete(url);
                        throw error;
                    }
                )
                .finally(() => clearTimeout(timer))
        };
        cache.set(url, entry);
        promise = entry.promise;
    }

    return promise.then(result => {
        if (result.text.includes('"now_ms"')) poll.lastPhaseRx = result.rx;
        return result;
    });
}

export function startPolling(): void {
    for (const name of INPUTS) window.addEventListener(name, input, { capture: true, passive: true });
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'hidden') asleep = true;
        else input();
    });
    setInterval(tick, 1000);
}

export const poll: PollApi = {
    awake,
    // The Unraid dashboard is often left open on a screen without anyone touching it: its tile only pauses
    // while the tab is hidden (setIdle(Infinity)).
    setIdle: ms => {
        idleMs = ms;
    },
    fetchText,
    lastPhaseRx: null, // receive time (performance.now()) of the last response that carried a LED phase
    async json<T = unknown>(url: string, maxAge = 0): Promise<T> {
        const { text } = await fetchText(url, maxAge);
        return JSON.parse(text) as T;
    },
    invalidate: () => cache.clear(),
    // fn runs every ms() while when() holds (and the page is awake); the caller loads once itself at start.
    // Returns { poke } to run it now (a view switch), skipped while the last run is still going.
    every(fn, ms, when = () => true) {
        const task: Task = { fn, ms: typeof ms === 'function' ? ms : () => ms, when, last: Date.now(), busy: false };
        tasks.push(task);
        return { poke: () => run(task) };
    }
};
