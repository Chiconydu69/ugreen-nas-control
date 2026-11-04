import { expect, test } from 'bun:test';
import { createT, fill, LANGS, pickLang } from './i18n';

test('pickLang keeps the supported languages and falls back otherwise', () => {
    for (const lang of LANGS) expect(pickLang(lang, 'en')).toBe(lang);
    expect(pickLang('pt', 'de')).toBe('de');
    expect(pickLang(undefined, 'en')).toBe('en');
});

test('fill replaces {name} and blanks missing values', () => {
    expect(fill('Bay {n} of {total}', { n: 2, total: 6 })).toBe('Bay 2 of 6');
    expect(fill('Bay {n}{x}', { n: 2 })).toBe('Bay 2');
    expect(fill('{a}', { a: 0 })).toBe('0');
});

test('English fills gaps in other languages, German after it, then the key', () => {
    const { t, raw, find } = createT<string>({ de: { a: 'a-de', b: 'b-de', c: 'c-de' }, en: { a: 'a-en', b: 'b-en' }, es: { a: 'a-es' } }, 'es');

    expect(t('a')).toBe('a-es');
    expect(t('b')).toBe('b-en');
    expect(t('c')).toBe('c-de');
    expect(t('d')).toBe('d');
    expect(raw('b')).toBe('b-en');
    expect(find('c')).toBeUndefined();
    expect(find('b')).toBe('b-en');
});

test('without any tables every key shows itself', () => {
    expect(createT<string>(undefined, 'de').t('nav.x')).toBe('nav.x');
});
