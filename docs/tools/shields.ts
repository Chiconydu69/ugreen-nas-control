/** Text for a shields.io badge path segment: every `-`, `_`, space and `+` is escaped, not only the first. */
export function shieldsText(text: string): string {
    return text.replace(/[-_ +]/g, c => ({ '-': '--', _: '__', ' ': '_', '+': '%2B' })[c]!);
}
