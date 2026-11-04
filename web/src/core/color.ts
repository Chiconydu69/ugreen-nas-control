/** "255 128 0" (the LED controller's colour) -> "#ff8000"; anything that is not three numbers is white. */
export function rgbToHex(value: unknown): string {
    const parts = String(value).trim().split(/\s+/).map(Number);

    if (parts.length < 3 || parts.some(Number.isNaN)) return '#ffffff';

    return `#${parts
        .slice(0, 3)
        .map(n => Math.max(0, Math.min(255, n)).toString(16).padStart(2, '0'))
        .join('')}`;
}

export function hexToRgb(hex: string): { r: number; g: number; b: number } {
    const value = hex.replace('#', '');

    return {
        r: Number.parseInt(value.slice(0, 2), 16),
        g: Number.parseInt(value.slice(2, 4), 16),
        b: Number.parseInt(value.slice(4, 6), 16)
    };
}

/** "#FF8000" or "ff8000" -> "#ff8000"; null when it is not a six digit hex colour. */
export function normalizeHex(value: unknown): string | null {
    const match = String(value)
        .trim()
        .match(/^#?([0-9a-f]{6})$/i);
    return match ? `#${(match[1] as string).toLowerCase()}` : null;
}

export const levelFromPercent = (percent: number): number => Math.round(percent * 2.55);
