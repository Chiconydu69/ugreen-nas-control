import { expect, test } from 'bun:test';
import { placeholderMismatches } from '../../../core/tables';
import { de } from './de';
import { en } from './en';
import { es } from './es';
import { fr } from './fr';
import { it } from './it';
import { nl } from './nl';
import { zh } from './zh';

// Missing and extra keys are compile errors (de.ts and es.ts are typed against en.ts); this checks the texts.
test('every language is filled in and uses the same {placeholders} as en', () => {
    for (const table of [de, es, fr, it, nl, zh]) expect(placeholderMismatches(en, table)).toEqual([]);
});

test('all tables have the same keys', () => {
    for (const table of [de, es, fr, it, nl, zh]) expect(Object.keys(table).sort()).toEqual(Object.keys(en).sort());
});
