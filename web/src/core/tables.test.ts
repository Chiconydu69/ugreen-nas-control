import { expect, test } from 'bun:test';
import { placeholderMismatches, placeholders } from './tables';

test('placeholders', () => {
    expect(placeholders('{b} and {a} and {b}')).toEqual(['a', 'b', 'b']);
    expect(placeholders('none')).toEqual([]);
});

test('placeholderMismatches finds other placeholders and empty texts', () => {
    const en = { a: 'Bay {n}', b: 'Hello', c: '{x}' };

    expect(placeholderMismatches(en, { a: 'Schacht {n}', b: 'Hallo', c: '{x}' })).toEqual([]);
    expect(placeholderMismatches(en, { a: 'Schacht {m}', b: 'Hallo', c: '{x}' })).toEqual(['a']);
    expect(placeholderMismatches(en, { a: 'Schacht {n}', b: ' ', c: '{x}' })).toEqual(['b']);
    expect(placeholderMismatches(en, { a: 'Schacht {n}', b: 'Hallo' })).toEqual(['c']);
});
