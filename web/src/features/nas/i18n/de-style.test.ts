import { expect, test } from 'bun:test';
import { de as ledDe } from '../../led/i18n/de';
import { de } from './de';

// Phrases that mark machine-made or stilted German; see docs/translations.md#german. A phrase that is
// legitimately needed goes into ALLOWED as 'key' with the reason.
const BANNED = [
    'dienst',
    'daemon',
    'macht sinn',
    'basierend auf',
    'erfolgt',
    'seitens',
    'bezüglich',
    'mittels',
    'bitte beachten sie',
    'realisier',
    'in kontrolle',
    'gehäuse-ansicht',
    'lüfterchip',
    'lüfter-dienst'
];
const ALLOWED: Record<string, string[]> = {};

test('German texts avoid calques and stilted phrases', () => {
    const hits: string[] = [];
    for (const table of [de, ledDe])
        for (const [key, text] of Object.entries(table))
            for (const phrase of BANNED)
                if (text.toLowerCase().includes(phrase) && !ALLOWED[key]?.includes(phrase)) hits.push(`${key}: "${phrase}" in "${text}"`);
    expect(hits).toEqual([]);
});
