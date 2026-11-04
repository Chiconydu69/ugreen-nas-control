export const placeholders = (text: string): string[] => [...text.matchAll(/\{(\w+)\}/g)].map(m => m[1] as string).sort();

/** Keys whose translation has other placeholders than the English text, or no text at all. */
export function placeholderMismatches(en: Record<string, string>, other: Record<string, string>): string[] {
    return Object.keys(en).filter(key => {
        const text = other[key];
        return !text?.trim() || placeholders(text).join() !== placeholders(en[key] as string).join();
    });
}
