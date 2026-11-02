// Hand-written checks of the top-level shape of an API answer (no schema library): enough to notice that a
// backend field was renamed or dropped. A failed check only logs a warning, the page keeps working.
const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null && !Array.isArray(value);

const has = (value: Record<string, unknown>, ...keys: string[]): boolean => keys.every(key => key in value);

export const isStatusResponse = (value: unknown): boolean =>
    isObject(value) &&
    has(value, 'ok', 'info', 'sensors', 'presets', 'calibration', 'running', 'led', 'settings') &&
    isObject(value.settings) &&
    has(value.settings, 'mode', 'min_pct') &&
    (!('fan_caps' in value) || (isObject(value.fan_caps) && has(value.fan_caps, 'kind', 'channels', 'watchdog', 'backlight'))) &&
    (!isObject(value.watchdog) ||
        (has(value.watchdog, 'armed', 'state') &&
            ['off', 'armed', 'action_needed', 'yielded', 'not_counting', 'not_effective'].includes(String(value.watchdog.state)))) &&
    (!('fan_config' in value.settings) || (isObject(value.settings.fan_config) && has(value.settings.fan_config, 'cpu', 'case', 'quiet')));

export const isHardwareResponse = (value: unknown): boolean => isObject(value) && has(value, 'ok', 'model', 'memory', 'nvme', 'bays', 'net');

export const isLedStatusResponse = (value: unknown): boolean => isObject(value) && has(value, 'ok', 'mode', 'bays', 'leds', 'fx', 'night');

export const isLedAllResponse = (value: unknown): boolean => isObject(value) && isObject(value.leds);

// The page was rendered by one plugin version, the backend answering it is another: an update replaced the files while
// this tab stayed open. A page or backend without a version never counts (old backend, harness).
export const isStaleBundle = (pageVersion: string | undefined, backendVersion: unknown): boolean =>
    Boolean(pageVersion) && typeof backendVersion === 'string' && backendVersion !== '' && backendVersion !== pageVersion;
