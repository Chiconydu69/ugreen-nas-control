/** Colour steps of the temperature bars: [warm, hot] in °C. */
export const LIMITS = {
    cpu: [70, 85],
    disk: [45, 50],
    nvme: [60, 70],
    other: [55, 70]
} as const;

export type TempKind = keyof typeof LIMITS;
export type TempLevel = 'hot' | 'warm' | 'ok' | '';

export const deg = (value: number | null | undefined): string => (value == null ? '–' : `${value} °C`);

export function tempClass(temp: number | null | undefined, kind: TempKind): TempLevel {
    if (temp == null) return '';
    const [warm, hot] = LIMITS[kind];
    return temp >= hot ? 'hot' : temp >= warm ? 'warm' : 'ok';
}

export const chipName = (name: unknown): string =>
    String(name || '')
        .replace('IT8613', 'ITE IT8613E')
        .replace('IT5571', 'ITE IT5571');

export function fmtSize(bytes: number | null | undefined): string {
    if (!bytes) return '';
    const tb = bytes / 1e12;
    if (tb >= 1) return `${tb >= 10 ? Math.round(tb) : Math.round(tb * 10) / 10} TB`;
    return `${Math.round(bytes / 1e9)} GB`;
}

export const fmtRam = (mb: number): string => (mb >= 1024 ? `${Math.round(mb / 1024)} GB` : `${mb} MB`);

export const speedText = (mbit: number): string => (mbit >= 1000 ? `${mbit / 1000} Gbit/s` : `${mbit} Mbit/s`);

/** "12th Gen Intel(R) Core(TM) i5-1235U @ 2.50GHz" -> "Core i5-1235U" */
export const shortCpu = (name: unknown): string =>
    String(name || '')
        .replace(/\(R\)|\(TM\)|CPU|@.*$|^\d+th Gen\s+Intel\s*/gi, '')
        .replace(/\s+/g, ' ')
        .trim();

/** dmidecode gives MM/DD/YYYY. */
export const biosDate = (s: unknown): string => String(s || '').replace(/^(\d{2})\/(\d{2})\/(\d{4})$/, '$2.$1.$3');

export const friendly = (value: unknown): string =>
    String(value || '')
        .replace(/_/g, ' ')
        .replace(/\s+/g, ' ')
        .trim();

/** Always day.month.year, 24 h, whatever the UI language: "27.10.2026 23:41". */
export const berlin = (seconds: number): string =>
    new Date(seconds * 1000)
        .toLocaleString('de-DE', {
            timeZone: 'Europe/Berlin',
            hour12: false,
            day: '2-digit',
            month: '2-digit',
            year: 'numeric',
            hour: '2-digit',
            minute: '2-digit'
        })
        .replace(',', '');

export const rangeFill = (value: number, min: number, max: number): string => `${((value - min) / (max - min)) * 100}%`;

/** "2.5G", "1G", "100M": the negotiated link speed next to the LAN LED. */
export const speedShort = (mbps: number): string => (mbps >= 1000 ? `${mbps / 1000}G` : mbps > 0 ? `${mbps}M` : '');

/** "2,5 Gbit/s" (decimal comma unless `dot`), "100 Mbit/s". */
export const linkSpeed = (mbps: number, dot: boolean): string =>
    mbps >= 1000 ? `${String(mbps / 1000).replace('.', dot ? '.' : ',')} Gbit/s` : `${mbps} Mbit/s`;
