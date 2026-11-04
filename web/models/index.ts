import { readdirSync } from 'node:fs';
import { basename, resolve } from 'node:path';
import { type Model, SERIES, validate } from './schema';

const dir = import.meta.dir;

/** Every registered model, sorted by series order and name. */
export async function loadModels(): Promise<Model[]> {
    const files = readdirSync(dir).filter(f => f.endsWith('.ts') && !['schema.ts', 'index.ts', 'fixture.ts'].includes(f));
    const models: Model[] = [];
    for (const file of files) {
        const model = (await import(resolve(dir, file))).default as Model;
        const problems = validate(model, basename(file, '.ts'));
        if (problems.length) throw new Error(`models/${file}: ${problems.join('; ')}`);
        models.push(model);
    }
    const order = SERIES;
    return models.sort((a, b) => order.indexOf(a.series) - order.indexOf(b.series) || a.name.localeCompare(b.name, 'en', { numeric: true }));
}
