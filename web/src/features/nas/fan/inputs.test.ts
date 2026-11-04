import { expect, test } from 'bun:test';
import { validValue } from './fields';
import { buildSpec, isMissing, specDisks, specMode, specParts, specScripts, valuesOf } from './inputs';

test('a spec splits into mode, drives and parts and builds back', () => {
    for (const spec of ['disk_max', 'disk_avg:disk1+cache', 'max:cpu|nvme|script:fan_probe', 'nic:eth0']) {
        expect(buildSpec(specMode(spec), specDisks(spec), specParts(spec))).toBe(spec);
    }
    expect(specMode('')).toBe('auto');
    expect(specDisks('disk_max:a+b')).toEqual(['a', 'b']);
    expect(specParts('max:cpu|gpu')).toEqual(['cpu', 'gpu']);
    expect(specScripts('max:cpu|script:a|script:b')).toEqual(['script:a', 'script:b']);
    expect(specScripts('cpu')).toEqual([]);
});

test('what the checklists build is always accepted by the backend', () => {
    expect(validValue('case_input', buildSpec('disk_avg', ['disk1', 'nvme0'], []))).toBe(true);
    expect(validValue('case_input', buildSpec('max', [], ['cpu', 'script:x']))).toBe(true);
    expect(validValue('cpu_input', 'max:cpu')).toBe(false);
    expect(valuesOf({ cpu: { input: '', on_missing: 'hold' }, case: { input: 'gpu', on_missing: 'fallback' } })).toEqual({
        cpu_input: 'auto',
        cpu_on_missing: 'hold',
        case_input: 'gpu',
        case_on_missing: 'fallback'
    });
});

test('inputs_missing concerns a group that uses the spec or one of its parts', () => {
    expect(isMissing('gpu', ['gpu'])).toBe(true);
    expect(isMissing('max:cpu|script:a', ['script:a'])).toBe(true);
    expect(isMissing('cpu', ['gpu'])).toBe(false);
    expect(isMissing('', null)).toBe(false);
});
