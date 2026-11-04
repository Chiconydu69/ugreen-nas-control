import { expect, test } from 'bun:test';
import { berlin, biosDate, chipName, deg, fmtRam, fmtSize, friendly, linkSpeed, rangeFill, shortCpu, speedShort, speedText, tempClass } from './format';

test('deg', () => {
    expect(deg(51)).toBe('51 °C');
    expect(deg(null)).toBe('–');
    expect(deg(0)).toBe('0 °C');
});

test('tempClass uses the limits of the kind', () => {
    expect(tempClass(69, 'cpu')).toBe('ok');
    expect(tempClass(70, 'cpu')).toBe('warm');
    expect(tempClass(85, 'cpu')).toBe('hot');
    expect(tempClass(45, 'disk')).toBe('warm');
    expect(tempClass(70, 'nvme')).toBe('hot');
    expect(tempClass(null, 'cpu')).toBe('');
});

test('drive sizes are powers of ten', () => {
    expect(fmtSize(0)).toBe('');
    expect(fmtSize(500_107_862_016)).toBe('500 GB');
    expect(fmtSize(4_000_787_030_016)).toBe('4 TB');
    expect(fmtSize(1_920_383_410_176)).toBe('1.9 TB');
    expect(fmtSize(12_000_138_625_024)).toBe('12 TB');
});

test('memory and link speeds', () => {
    expect(fmtRam(512)).toBe('512 MB');
    expect(fmtRam(16384)).toBe('16 GB');
    expect(speedText(2500)).toBe('2.5 Gbit/s');
    expect(speedText(100)).toBe('100 Mbit/s');
    expect(speedShort(2500)).toBe('2.5G');
    expect(speedShort(100)).toBe('100M');
    expect(speedShort(0)).toBe('');
    expect(linkSpeed(2500, false)).toBe('2,5 Gbit/s');
    expect(linkSpeed(2500, true)).toBe('2.5 Gbit/s');
    expect(linkSpeed(100, true)).toBe('100 Mbit/s');
});

test('names', () => {
    expect(chipName('IT8613')).toBe('ITE IT8613E');
    expect(chipName('IT5571')).toBe('ITE IT5571');
    expect(chipName(undefined)).toBe('');
    expect(shortCpu('12th Gen Intel(R) Core(TM) i5-1235U @ 2.50GHz')).toBe('Core i5-1235U');
    expect(biosDate('03/24/2024')).toBe('24.03.2024');
    expect(biosDate('unknown')).toBe('unknown');
    expect(friendly('Samsung_SSD__850_EVO ')).toBe('Samsung SSD 850 EVO');
});

test('berlin time is day.month.year 24 h', () => {
    expect(berlin(1_761_600_000)).toBe('27.10.2025 22:20');
    expect(berlin(1_767_225_600)).toBe('01.01.2026 01:00');
});

test('rangeFill', () => {
    expect(rangeFill(25, 10, 60)).toBe('30%');
    expect(rangeFill(10, 10, 60)).toBe('0%');
});
