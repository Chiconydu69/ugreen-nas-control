import { expect, test } from 'bun:test';
import type { StatusResponse } from '../../../core/contract';
import { measuredStart } from './shaping';

const status = (fans: { id: string; role: 'cpu' | 'case' | '' }[], table: Record<string, [number, number][]> | null): StatusResponse =>
    ({ fans, calibration: { table: table && { time: 1, fans: table }, progress: null } }) as unknown as StatusResponse;

test('the measured kick-start is the highest first-turning speed of the group', () => {
    const s = status(
        [
            { id: 'fan1', role: 'cpu' },
            { id: 'fan2', role: 'case' },
            { id: 'fan3', role: 'case' }
        ],
        {
            fan1: [
                [10, 0],
                [20, 600]
            ],
            fan2: [[10, 300]],
            fan3: [
                [10, 0],
                [30, 0],
                [40, 500]
            ]
        }
    );
    expect(measuredStart(s, 'cpu')).toBe(20);
    expect(measuredStart(s, 'case')).toBe(40);
    expect(measuredStart(status([{ id: 'fan1', role: 'cpu' }], null), 'cpu')).toBeNull();
    expect(measuredStart(status([{ id: 'fan1', role: 'cpu' }], { fan9: [[10, 1]] }), 'cpu')).toBeNull();
});
