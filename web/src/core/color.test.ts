import { expect, test } from 'bun:test';
import { hexToRgb, levelFromPercent, normalizeHex, rgbToHex } from './color';

test('rgbToHex reads the controller colour', () => {
    expect(rgbToHex('255 128 0')).toBe('#ff8000');
    expect(rgbToHex(' 0  0 255 ')).toBe('#0000ff');
    expect(rgbToHex('300 -5 0')).toBe('#ff0000');
    expect(rgbToHex('1 2')).toBe('#ffffff');
    expect(rgbToHex('a b c')).toBe('#ffffff');
});

test('hexToRgb', () => {
    expect(hexToRgb('#ff8000')).toEqual({ r: 255, g: 128, b: 0 });
});

test('normalizeHex accepts six digits with or without #', () => {
    expect(normalizeHex('#FF8000')).toBe('#ff8000');
    expect(normalizeHex('ff8000')).toBe('#ff8000');
    expect(normalizeHex(' #00c853 ')).toBe('#00c853');
    expect(normalizeHex('#fff')).toBeNull();
    expect(normalizeHex('#gg0000')).toBeNull();
});

test('levelFromPercent maps 0-100 to 0-255', () => {
    expect(levelFromPercent(0)).toBe(0);
    expect(levelFromPercent(100)).toBe(255);
    expect(levelFromPercent(80)).toBe(204);
});
