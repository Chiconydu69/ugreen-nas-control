import { expect, test } from 'bun:test';
import { curveAt, curveText, effectivePoints, parseCurve, rpmAt, rpmTicks, validCurve } from './curve';

const standard = parseCurve('45:25,60:35,70:55,80:80,88:100');

test('parseCurve and curveText round-trip', () => {
    expect(parseCurve('40:30,60:50')).toEqual([
        { temp: 40, pct: 30 },
        { temp: 60, pct: 50 }
    ]);
    expect(curveText(standard)).toBe('45:25,60:35,70:55,80:80,88:100');
});

test('parseCurve drops what is not a number (an empty half counts as 0)', () => {
    expect(parseCurve('40:30,x:5,60:60')).toEqual([
        { temp: 40, pct: 30 },
        { temp: 60, pct: 60 }
    ]);
    expect(parseCurve('50:')).toEqual([{ temp: 50, pct: 0 }]);
    expect(parseCurve('')).toEqual([]);
    expect(parseCurve(undefined)).toEqual([]);
});

test('curveAt holds the end values and interpolates linearly', () => {
    expect(curveAt(standard, 20)).toBe(25);
    expect(curveAt(standard, 45)).toBe(25);
    expect(curveAt(standard, 52.5)).toBe(30);
    expect(curveAt(standard, 75)).toBe(68);
    expect(curveAt(standard, 99)).toBe(100);
    expect(curveAt([], 50)).toBe(100);
});

// The same rule as valid_curve() in web/plugin/api.php.
test('validCurve mirrors the PHP rules', () => {
    expect(validCurve(standard)).toBe(true);
    expect(validCurve(parseCurve('40:30'))).toBe(false);
    expect(validCurve(parseCurve('40:30,40:50'))).toBe(false);
    expect(validCurve(parseCurve('50:30,40:50'))).toBe(false);
    expect(validCurve(parseCurve('0:0,120:100'))).toBe(true);
    expect(validCurve(parseCurve('0:0,121:100'))).toBe(false);
    expect(validCurve(parseCurve('0:0,50:101'))).toBe(false);
    expect(
        validCurve([
            { temp: 10.5, pct: 10 },
            { temp: 20, pct: 20 }
        ])
    ).toBe(false);
});

test('effectivePoints raises the curve to the floor and adds the crossings', () => {
    const points = parseCurve('40:20,60:60');
    const out = effectivePoints(points, 20, 100, 40);

    expect(out.every(p => p.pct >= 40)).toBe(true);
    expect(out.some(p => p.temp === 50 && p.pct === 40)).toBe(true);
    expect(out[0]).toEqual({ temp: 20, pct: 40 });
    expect(out[out.length - 1]).toEqual({ temp: 100, pct: 60 });
    expect(effectivePoints([], 20, 100, 40)).toEqual([]);
});

const rows: [number, number][] = [
    [0, 0],
    [20, 400],
    [50, 1000],
    [100, 3000]
];

test('rpmAt interpolates the measured table', () => {
    expect(rpmAt(rows, 20)).toBe(400);
    expect(rpmAt(rows, 35)).toBe(700);
    expect(rpmAt(rows, 100)).toBe(3000);
    expect(rpmAt(rows, 120)).toBe(3000);
    expect(rpmAt(undefined, 20)).toBeNull();
    expect(rpmAt([], 20)).toBeNull();
    expect(rpmAt(rows, Number.NaN)).toBeNull();
});

test('rpmAt below the lowest point only knows a measured zero', () => {
    expect(
        rpmAt(
            [
                [10, 300],
                [100, 3000]
            ],
            5
        )
    ).toBeNull();
    expect(
        rpmAt(
            [
                [0, 0],
                [100, 3000]
            ],
            0
        )
    ).toBe(0);
});

test('rpmTicks places round rpm values on the measured curve', () => {
    const ticks = rpmTicks(rows);

    expect(ticks.map(t => t.rpm)).toEqual([500, 1000, 1500, 2000, 2500, 3000]);
    expect(ticks.find(t => t.rpm === 1000)?.pct).toBe(50);
    expect(rpmTicks([[0, 0]])).toEqual([]);
    expect(rpmTicks(undefined)).toEqual([]);
});
