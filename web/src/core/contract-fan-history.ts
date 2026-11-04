/*
 * fan_history (web/plugin/include/fan_history.php, backend/internal/fan/history.go): what the fans did and what drove
 * them, from the fan daemon's RAM. Arrays, one entry per point (oldest first); the daemon keeps 10 s steps for the last
 * hour and 60 s averages for 24 hours, nothing on disk, and forgets all of it when it restarts.
 */
import type { ApiOk } from './contract';

export type FanHistoryRange = '1h' | '24h';

/** Modifier bits of `mods` (Mod* in backend/internal/fan/drive.go). */
export const MOD = { quiet: 1, mover: 2, parity: 4, floor: 8, shape: 16, emergency: 32, missing: 64 } as const;

export interface FanHistorySeries {
    id: string;
    /** Speed in %, null when unknown. */
    pct: (number | null)[];
    rpm: (number | null)[];
    /** °C of the input that drove the fan, null when none. */
    temp: (number | null)[];
    /** Index into `drivers`, -1 for none. */
    drv: number[];
    mods: number[];
}

/**
 * `drivers` are the sources: cpu, disk:<name> (hottest drive), disk_avg, nvme, gpu, hba, nic:<iface>, script:<name>,
 * idle (all drives asleep), missing, hold, emergency:<reason>, bios, calibration, rival, other.
 */
export interface FanHistoryResponse extends ApiOk {
    /** false: the fan daemon is not running; nothing else is set. */
    running: boolean;
    range?: FanHistoryRange;
    /** Seconds per point. */
    step?: number;
    /** Unix seconds of the answer and of the daemon start (the history begins there). */
    now?: number;
    since?: number;
    fans?: { id: string; role: 'cpu' | 'case' | string }[];
    drivers?: string[];
    t?: number[];
    cpu?: (number | null)[];
    /** The hottest drive (the case fans' input), °C. */
    case?: (number | null)[];
    series?: FanHistorySeries[];
}
