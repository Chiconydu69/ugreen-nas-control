import { expect, test } from 'bun:test';
import { MOD } from '../../../core/contract-fan-history';
import {
    berlinOffset,
    clock,
    dayClock,
    lines,
    maskRuns,
    modNames,
    nearest,
    runs,
    sampleIndexes,
    sourceArg,
    sourceKind,
    tempRange,
    timeTicks
} from './history-model';

test('sourceKind and sourceArg name the family and the detail of a source', () => {
    expect(sourceKind('cpu')).toBe('cpu');
    expect(sourceKind('disk:disk2')).toBe('disk');
    expect(sourceKind('disk_avg')).toBe('disk');
    expect(sourceKind('emergency:cpu_hot')).toBe('emergency');
    expect(sourceKind('nic:eth0')).toBe('nic');
    expect(sourceKind('bios')).toBe('off');
    expect(sourceKind('idle')).toBe('idle');
    expect(sourceKind('whatever')).toBe('other');
    expect(sourceArg('disk:disk2')).toBe('disk2');
    expect(sourceArg('cpu')).toBe('');
});

test('runs merge neighbours with the same value and skip rejected ones', () => {
    expect(runs([1, 1, 2, 2, 2, -1, 1])).toEqual([
        { from: 0, to: 1, value: 1 },
        { from: 2, to: 4, value: 2 },
        { from: 5, to: 5, value: -1 },
        { from: 6, to: 6, value: 1 }
    ]);
    expect(runs([1, -1, -1, 1], v => v >= 0)).toEqual([
        { from: 0, to: 0, value: 1 },
        { from: 3, to: 3, value: 1 }
    ]);
    // a value kept on both sides of a rejected one is two runs
    expect(runs([2, -1, 2], v => v >= 0).length).toBe(2);
    expect(maskRuns([0, MOD.quiet, MOD.quiet | MOD.floor, MOD.floor], MOD.quiet)).toEqual([{ from: 1, to: 2, value: MOD.quiet }]);
});

test('lines break at missing values and at holes longer than 2.5 steps', () => {
    const t = [0, 10, 20, 30, 100, 110];
    const v = [1, 2, null, 4, 5, 6];
    expect(lines(t, v, 10).map(l => l.map(p => p.v))).toEqual([[1, 2], [4], [5, 6]]);
    expect(lines([], [], 10)).toEqual([]);
    expect(lines([0, 10], [null, null], 10)).toEqual([]);
});

test('nearest picks the closest point', () => {
    const t = [0, 10, 20, 40];
    expect(nearest(t, -5)).toBe(0);
    expect(nearest(t, 4)).toBe(0);
    expect(nearest(t, 6)).toBe(1);
    expect(nearest(t, 29)).toBe(2);
    expect(nearest(t, 31)).toBe(3);
    expect(nearest(t, 99)).toBe(3);
    expect(nearest([], 1)).toBe(-1);
});

test('clock is 24 h Europe/Berlin, with the daylight saving offset of the day', () => {
    const summer = Date.UTC(2026, 6, 1, 10, 5, 9) / 1000; // CEST, +2
    const winter = Date.UTC(2026, 0, 1, 10, 5, 9) / 1000; // CET, +1
    expect(clock(summer)).toBe('12:05');
    expect(clock(winter, true)).toBe('11:05:09');
    expect(dayClock(summer)).toBe('01.07. 12:05');
    expect(berlinOffset(summer)).toBe(7200);
    expect(berlinOffset(winter)).toBe(3600);
});

test('timeTicks sit on round Berlin times', () => {
    const to = Date.UTC(2026, 9, 30, 12, 34, 0) / 1000; // 13:34 CET... CEST until 25.10., CET after
    const ticks = timeTicks(to - 3600, to, '1h', berlinOffset(to));
    expect(ticks.map(t => clock(t))).toEqual(['12:40', '12:50', '13:00', '13:10', '13:20', '13:30']);
    const day = timeTicks(to - 86400, to, '24h', berlinOffset(to));
    expect(day.map(t => clock(t))).toEqual(['16:00', '20:00', '00:00', '04:00', '08:00', '12:00']);
});

test('tempRange pads to whole 10 degrees and keeps at least 20 degrees', () => {
    expect(tempRange([[40.1, 55], [null]])).toEqual([30, 60]);
    expect(tempRange([[44, 45]])[1] - tempRange([[44, 45]])[0]).toBe(20);
    expect(tempRange([[null]])).toEqual([20, 60]);
});

test('modNames lists what applies, emergency first', () => {
    expect(modNames(0)).toEqual([]);
    expect(modNames(MOD.quiet | MOD.emergency | MOD.floor)).toEqual(['emergency', 'floor', 'quiet']);
});

test('sampleIndexes spreads rows over the points, newest last', () => {
    expect(sampleIndexes(3, 24)).toEqual([0, 1, 2]);
    const rows = sampleIndexes(360, 24);
    expect(rows.length).toBe(24);
    expect(rows[0]).toBe(0);
    expect(rows[23]).toBe(359);
});
