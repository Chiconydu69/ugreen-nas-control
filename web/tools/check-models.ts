// Validates web/models/*.ts (schema, unique ids, fixtures build, evidence versions) and warns about stale checks.
// `just models-check` / part of `just site`.
//
// A verified check names the plugin release it was done on (version). It must be a release of CHANGELOG.md. A check
// more than STALE_AFTER releases older than the newest release is stale: the warning lists it so the maintainers
// re-test it on their own device before the release (docs/releasing.md). Warnings do not fail the check.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { hardware, ledOutputs, status } from '../models/fixture';
import { loadModels } from '../models/index';
import { CAPABILITIES, overallStatus } from '../models/schema';

const STALE_AFTER = 3;

const changelog = readFileSync(resolve(import.meta.dir, '../../CHANGELOG.md'), 'utf8');
const releases = [...changelog.matchAll(/^## (\d+\.\d+(?:\.\d+)?)\s*$/gm)].map(m => m[1] ?? '');

const models = await loadModels();
const ids = new Set<string>();
const errors: string[] = [];
const stale: string[] = [];
for (const m of models) {
    if (ids.has(m.id)) throw new Error(`duplicate id ${m.id}`);
    ids.add(m.id);
    JSON.stringify([hardware(m), status(m), ledOutputs(m)]);
    for (const cap of CAPABILITIES) {
        const c = m.verified[cap];
        if (!c.version) continue;
        const age = releases.indexOf(c.version);
        if (age < 0) errors.push(`${m.id}: verified.${cap}.version ${c.version} is not a release in CHANGELOG.md`);
        else if (c.status === 'verified' && age > STALE_AFTER)
            stale.push(`${m.name}: ${cap} verified on ${c.version}, ${age} releases ago (current ${releases[0]})`);
    }
}
if (errors.length) {
    for (const e of errors) console.error(e);
    process.exit(1);
}
for (const s of stale) console.warn(`stale: ${s}`);
const count = (o: string) => models.filter(m => overallStatus(m) === o).length;
console.log(
    `models ok: ${models.length} (${models.filter(m => m.shots).length} with screenshots; ${count('verified')} verified, ${count('partial')} partly, ${count('community')} community, ${count('expected')} expected; ${stale.length} stale checks)`
);
