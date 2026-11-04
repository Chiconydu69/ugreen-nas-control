export type Theme = 'light' | 'dark';

// "auto" follows Unraid: light when the page behind the plugin is light.
export function resolveTheme(setting?: string): Theme {
    if (setting === 'light' || setting === 'dark') return setting;

    for (let el: HTMLElement | null = document.body; el; el = el.parentElement) {
        const match = getComputedStyle(el).backgroundColor.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)(?:,\s*([\d.]+))?/);
        if (match && (match[4] === undefined || Number(match[4]) > 0.5)) {
            const [r, g, b] = [match[1], match[2], match[3]].map(Number) as [number, number, number];
            return luminance(r, g, b) > 140 ? 'light' : 'dark';
        }
    }
    return 'dark';
}

export const luminance = (r: number, g: number, b: number): number => 0.299 * r + 0.587 * g + 0.114 * b;
