#!/usr/bin/env bun
// Stand-in for ugreen-nas-fan, ugreen-nas-ctl and ugreen-led-ctl in the API oracle. Installed by run.ts with absolute paths
// (the /var/run and /proc below are rewritten into the temp root like the PHP's own paths) and a shebang of the running bun.
// The first rule of the case whose args are a prefix of the argv (and whose stdin matches, when given) answers; every call is logged.
import { appendFileSync, mkdirSync, readFileSync, rmSync } from 'node:fs';
import { basename } from 'node:path';

interface Rule {
    args: string[];
    stdin?: string;
    out?: unknown;
    err?: string;
    code?: number;
    sleep?: number;
}

const dir = '/var/run/unc-oracle';
const names: Record<string, string> = { 'ugreen-nas-fan': 'fan', 'ugreen-nas-ctl': 'ctl', 'ugreen-led-ctl': 'led', 'ugreen-nas': 'nas' };
const tool = names[basename(process.argv[1] ?? '')] ?? 'unknown';
const args = process.argv.slice(2);
const stdin = args.includes('--stdin') ? await Bun.stdin.text() : undefined;

appendFileSync(`${dir}/calls.jsonl`, JSON.stringify({ tool, args, ...(stdin === undefined ? {} : { stdin }) }) + '\n');

const rules: Rule[] = (JSON.parse(readFileSync(`${dir}/tools.json`, 'utf8')) as Record<string, Rule[]>)[tool] ?? [];
const rule = rules.find(r => r.args.every((a, i) => args[i] === a) && (r.stdin === undefined || r.stdin === stdin));
if (!rule) {
    process.stderr.write(`oracle: no rule for ${tool} ${args.join(' ')}\n`);
    process.exit(99);
}

if (rule.sleep) {
    const alive = `/proc/${process.pid}`;
    mkdirSync(alive, { recursive: true });
    await Bun.sleep(rule.sleep);
    rmSync(alive, { recursive: true, force: true });
}
if (rule.out !== undefined) process.stdout.write(typeof rule.out === 'string' ? rule.out : JSON.stringify(rule.out) + '\n');
if (rule.err) process.stderr.write(rule.err);
process.exit(rule.code ?? 0);
