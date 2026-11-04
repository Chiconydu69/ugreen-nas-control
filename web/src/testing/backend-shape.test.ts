// Guards the fixtures against drifting from the real backend: every key and every enum value the model fixtures
// emit for `led-status` must be one the real Go code can emit, and every key the real code always emits must be
// there. The record comes from the Go test backend/internal/led/ledctl/status_shape_test.go (runs the real
// `status` over many products and settings); it fails there when the backend changes, so this file stays honest.
import { expect, test } from 'bun:test';
import { join } from 'node:path';
import { ledOutputs, status } from '../../models/fixture';
import { loadModels } from '../../models/index';

interface Shape {
    keys: Record<string, string[]>;
    enums: Record<string, string[]>;
}
const shape: Shape = await Bun.file(join(import.meta.dir, '../../../backend/internal/led/ledctl/testdata/status-shape.json')).json();

const type = (v: unknown): string => (v === null ? 'null' : Array.isArray(v) ? 'array' : typeof v);

test('led-status fixtures use only keys, types and enum values the Go backend emits', async () => {
    const models = await loadModels();
    expect(models.length).toBeGreaterThan(0);
    const problems: string[] = [];
    for (const model of models) {
        const status = ledOutputs(model).status as Record<string, unknown>;
        const name = model.id ?? model.name;
        for (const [key, value] of Object.entries(status)) {
            const types = shape.keys[key];
            if (!types) problems.push(`${name}: key "${key}" is not emitted by the backend`);
            else if (!types.includes(type(value))) problems.push(`${name}: "${key}" is ${type(value)}, the backend emits ${types.join('|')}`);
            const values = shape.enums[key];
            if (values && !values.includes(String(value))) problems.push(`${name}: "${key}" = "${String(value)}", the backend emits ${values.join('|')}`);
        }
        for (const key of Object.keys(shape.keys)) {
            // i2c_* are null when the controller does not answer; every other key is always present.
            if (!(key in status)) problems.push(`${name}: key "${key}" is missing`);
        }
    }
    expect(problems.slice(0, 20)).toEqual([]);
});

// ---- ugreen-nas-fan status ----
// The same for `status` of the fan tool (recorded by backend/internal/fan/status_shape_test.go). web/plugin/api.php adds
// running, version, led and settings to the answer; the harness adds scenarios.
interface FanShape {
    keys: Record<string, string[]>;
    always: string[];
    nested: Record<string, string[]>;
}
const fanShape: FanShape = await Bun.file(join(import.meta.dir, '../../../backend/internal/fan/testdata/status-shape.json')).json();
const PHP_KEYS = ['running', 'version', 'led', 'settings', 'scenarios'];

test('fan status fixtures use only keys and types the Go backend emits, for every kind of model', async () => {
    const models = await loadModels();
    const problems: string[] = [];
    for (const model of models) {
        const st = status(model) as Record<string, unknown>;
        const name = model.id ?? model.name;
        for (const [key, value] of Object.entries(st)) {
            if (PHP_KEYS.includes(key)) continue;
            const types = fanShape.keys[key];
            if (!types) problems.push(`${name}: key "${key}" is not emitted by the backend`);
            else if (!types.includes(type(value)) && !(key === 'watchdog' && type(value) === 'object'))
                problems.push(`${name}: "${key}" is ${type(value)}, the backend emits ${types.join('|')}`);
            const sub = fanShape.nested[key];
            if (sub && value && typeof value === 'object' && !Array.isArray(value)) {
                for (const k of Object.keys(value)) if (!sub.includes(k)) problems.push(`${name}: ${key}.${k} is not emitted by the backend`);
                for (const k of sub) if (!(k in value) && key !== 'sensors' && k !== 'min_pct_floor') problems.push(`${name}: ${key}.${k} is missing`);
            }
        }
        for (const key of fanShape.always) if (!(key in st)) problems.push(`${name}: key "${key}" is missing`);
        for (const fan of (st.fans as Record<string, unknown>[] | undefined) ?? []) {
            for (const k of Object.keys(fan)) if (!fanShape.nested['fans[]']?.includes(k)) problems.push(`${name}: fans[].${k} is not emitted by the backend`);
        }
        // The chip answers or the status says why not; never both, never neither.
        if ('chip' in st === 'chip_error' in st) problems.push(`${name}: exactly one of chip and chip_error`);
        // Fan control keys come with a fan model only (backend/internal/fan/main.go: CapsFor); the watchdog sample with a petter.
        const settings = st.settings as Record<string, unknown>;
        if ('fan_caps' in st !== Boolean(model.fan)) problems.push(`${name}: fan_caps exactly when the model has fan control`);
        if ('fan_config' in settings !== Boolean(model.fan)) problems.push(`${name}: settings.fan_config exactly when the model has fan control`);
        const fanConfig = settings.fan_config as Record<string, unknown> | undefined;
        if (fanConfig) {
            for (const k of Object.keys(fanConfig))
                if (!fanShape.nested.fan_config?.includes(k)) problems.push(`${name}: fan_config.${k} is not emitted by the backend`);
            for (const k of fanShape.nested.fan_config ?? []) if (!(k in fanConfig)) problems.push(`${name}: fan_config.${k} is missing`);
            for (const g of ['cpu', 'case'])
                if (typeof (fanConfig[g] as Record<string, unknown>)?.allow_off !== 'boolean') problems.push(`${name}: fan_config.${g}.allow_off is missing`);
        }
        if ((st.watchdog !== null) !== Boolean(model.fan?.watchdog)) problems.push(`${name}: watchdog is a sample exactly on the models with a petter`);
        if (st.watchdog && Object.keys(st.watchdog).sort().join() !== 'armed,interval,last_pet,state,time,timeout')
            problems.push(`${name}: watchdog keys differ from WatchdogStatus`);
        if (model.fan?.minPctFloor !== (st.fan_caps as { min_pct_floor?: number } | undefined)?.min_pct_floor)
            problems.push(`${name}: fan_caps.min_pct_floor follows the model's floor`);
        // A probe model has no confirmed map in the sample data: no fans, no daemon.
        if (model.fan?.kind === 'probe' && ('fans' in st || 'daemon' in st)) problems.push(`${name}: a probe model without a map has no fans and no daemon`);
    }
    expect(problems.slice(0, 20)).toEqual([]);
});
