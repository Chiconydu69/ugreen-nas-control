// What the PHP offers against what the case table and the goldens exercise: every GET/POST action of both APIs, every literal
// error message, every status code. A new action or error text in the PHP fails the oracle test until a case covers it.
import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import type { Case, Result } from './oracle';

const plugin = resolve(import.meta.dir, '../../plugin');
const read = (rel: string) => readFileSync(join(plugin, rel), 'utf8');

// Unreachable on a real install: the entry gate or the OS rules them out before these run.
const UNREACHABLE: Record<string, string> = {
    'The fan tool is missing or not executable': 'run_detached() runs after the gate that already checked FAN_TOOL',
    'Cannot create a temporary file': 'tempnam() in the system temp dir does not fail here',
    'The fan tool cannot be started': 'proc_open() of a file that passed is_executable()'
};

const all = (src: string, re: RegExp) => [...src.matchAll(re)].map(m => m[1]!);

export function phpActions(): { api: 'nas' | 'led'; method: 'GET' | 'POST'; action: string }[] {
    const nas = read('api.php');
    const post = nas.indexOf("if ($method !== 'POST')");
    const led = read('led/api.php');
    const ledPost = led.indexOf("if ($method === 'POST')");
    const ledWrite = led.indexOf("$led = '%LED%'");
    return [
        ...['status', ...all(nas.slice(0, post), /\$action === '(\w+)'/g)].map(action => ({ api: 'nas' as const, method: 'GET' as const, action })),
        ...all(nas.slice(post), /\$action === '(\w+)'/g).map(action => ({ api: 'nas' as const, method: 'POST' as const, action })),
        ...all(led.slice(led.indexOf("if ($method === 'GET')"), ledPost), /case '(\w+)':/g).map(action => ({
            api: 'led' as const,
            method: 'GET' as const,
            action
        })),
        ...[...all(led.slice(ledPost), /\$action === '(\w+)'/g), ...all(led.slice(ledWrite), /case '(\w+)':/g)].map(action => ({
            api: 'led' as const,
            method: 'POST' as const,
            action
        }))
    ];
}

export function caseAction(c: Case): { method: string; action: string } {
    const method = c.method ?? (c.body !== undefined || c.upload ? 'POST' : 'GET');
    const params = new URLSearchParams(method === 'GET' ? (typeof c.query === 'string' ? c.query : c.query) : typeof c.body === 'string' ? c.body : c.body);
    return { method, action: params.get('action') ?? (method === 'GET' ? 'status' : '') };
}

export function missingActions(cases: Case[]): string[] {
    const have = new Set(cases.map(c => `${c.api} ${caseAction(c).method} ${caseAction(c).action}`));
    return phpActions()
        .map(a => `${a.api} ${a.method} ${a.action}`)
        .filter(key => !have.has(key));
}

// The text each `'error' => ...` of the PHP starts with (what is built from variables is skipped, its parts are covered by cases).
export function errorTexts(): string[] {
    const files = [
        'api.php',
        'led/api.php',
        'include/fan_backlight.php',
        'include/fan_inputs.php',
        'include/fan_probe.php',
        'include/fan_profile.php',
        'include/panel.php',
        'include/power_ac.php'
    ];
    const out = new Set<string>();
    for (const file of files) {
        for (const line of read(file).split('\n')) {
            const rhs = /'error' => (.*)$/.exec(line)?.[1];
            if (!rhs) continue;
            const text = /^(?:CONFIG_FILE \. )?'((?:[^'\\]|\\.)*)'/.exec(rhs)?.[1] ?? /^"([^"{]*)/.exec(rhs)?.[1];
            if (text) out.add(text.replace(/\\'/g, "'"));
        }
    }
    return [...out].sort();
}

export function missingErrors(golden: Record<string, Result>): string[] {
    const bodies = Object.values(golden).map(r => r.raw);
    return errorTexts().filter(t => !(t in UNREACHABLE) && !bodies.some(b => b.includes(t)));
}

export const EXPECTED_STATUS: Record<'nas' | 'led', number[]> = {
    nas: [200, 400, 405, 409, 429, 500],
    led: [200, 400, 405, 500]
};

export function missingStatus(cases: Case[], golden: Record<string, Result>): string[] {
    return (['nas', 'led'] as const).flatMap(api => {
        const seen = new Set(cases.filter(c => c.api === api).map(c => golden[c.id]?.status));
        return EXPECTED_STATUS[api].filter(s => !seen.has(s)).map(s => `${api} ${s}`);
    });
}
