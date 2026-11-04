export const LANGS = ['de', 'en', 'es', 'fr', 'it', 'nl', 'zh'] as const;
export type Lang = (typeof LANGS)[number];
export type Vars = Record<string, string | number | null | undefined>;
type Tables = Partial<Record<Lang, Record<string, string>>>;

export const pickLang = (value: unknown, fallback: Lang): Lang => LANGS.find(lang => lang === value) ?? fallback;

export const fill = (text: string, vars: Vars = {}): string => text.replace(/\{(\w+)\}/g, (_, name: string) => String(vars[name] ?? ''));

/**
 * The text of `key` in `lang`. English fills gaps in other languages, German after it, then the key itself shows.
 */
export function createT<K extends string>(tables: Tables | undefined, lang: Lang) {
    const texts = tables ?? { de: {}, en: {} };
    const warned = new Set<string>();
    const lookup = (key: string): string => String(texts[lang]?.[key] ?? texts.en?.[key] ?? texts.de?.[key] ?? key);

    return {
        t: (key: K, vars: Vars = {}): string => fill(lookup(key), vars),
        /** Text by a key that is only known at run time (an API error message, a value from the backend). */
        raw: (key: string, vars: Vars = {}): string => fill(lookup(key), vars),
        /** Text for a backend enum value (key built as `prefix.${value}`); an unknown value shows as itself (never a key) and is logged once. */
        enumText: (key: string, value: string, vars: Vars = {}): string => {
            const text = texts[lang]?.[key] ?? texts.en?.[key] ?? texts.de?.[key];
            if (text !== undefined) return fill(String(text), vars);
            if (!warned.has(key)) {
                warned.add(key);
                console.warn(`[UGREEN NAS] no text for backend value "${value}" (${key})`);
            }
            return value;
        },
        find: (key: string): string | undefined => texts[lang]?.[key] ?? texts.en?.[key]
    };
}
