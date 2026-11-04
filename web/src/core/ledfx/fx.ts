/*
 * One LED renderer for every view that draws a UGREEN LED (LED page, NAS Overview, case view,
 * Unraid Dashboard tile). Client side only: it draws from the per-LED state the pages already
 * fetch ({ color:"R G B", brightness, effect, on_ms, off_ms, sent_ms, now_ms }) and animates with
 * the Web Animations API, so nothing is polled, read or written on the NAS for it.
 *
 * - static: colour at the mapped brightness.
 * - blink:  square wave, cycle = on_ms + off_ms, lit for on_ms (controller semantics).
 * - breathe: cycle = on_ms + off_ms. The real curve is firmware-defined and undocumented; this is
 *   the exp(sin) shape (wide trough, narrow peak), sampled at BREATH_STOPS keyframes, in PWM
 *   domain before the brightness mapping.
 * - rainbow: software effect of the daemon, mirrored step for step (10 degrees per step period,
 *   per-LED offset 360/n, as led/i2c/effects.go).
 * - temperature: the daemon writes a plain colour, so the reported colour is shown as is.
 * Phase: when the backend reports sent_ms/now_ms (time of the last blink/breath command, same
 * clock) the animation starts in step with the LED; otherwise it starts at an arbitrary phase.
 * prefers-reduced-motion: no animation, static colour, data-fx names the effect for a CSS label.
 */
import type { LedState, LedStatus } from '../contract';

const GAMMA = 2.2; // display gamma: opacity = linear light ^ (1/GAMMA)
const FLOOR = 0.18; // darkest visible lit LED (brightness 1/255 must not look dead)
const DARK = 0.08; // an LED in the dark part of a blink/breath
const BREATH_STOPS = 16;
const HUE_STEP = 10; // degrees per rainbow step (effects.go hueStep)
const RAINBOW_STEP_MS = [600, 400, 250, 150, 80]; // effects.go rainbowPeriods
const MIN_BLINK_CYCLE = 20;
const BUSY_CYCLE = 334; // blinks faster than this (> 3 Hz, WCAG 2.3.1) are drawn as a 3 Hz soft flicker
const BUSY_LOW = 0.6; // flicker depth: the dim half is this fraction of the lit alpha

type Rgb = number[];

interface StaticSpec {
    kind: 'static';
    rgb: Rgb;
    peak: number;
}
interface RainbowSpec {
    kind: 'rainbow';
    rgb: Rgb;
    peak: number;
    step: number;
    offset: number;
}
interface BlinkSpec {
    kind: 'blink';
    rgb: Rgb;
    peak: number;
    lit: number;
    cycle: number;
}
interface BreathSpec {
    kind: 'breath';
    rgb: Rgb;
    peak: number;
    lit: number;
    cycle: number;
}
interface BusySpec {
    kind: 'busy';
    rgb: Rgb;
    peak: number;
    cycle: number;
}
export type LedSpec = { kind: 'off' } | StaticSpec | RainbowSpec | BlinkSpec | BreathSpec | BusySpec;

type Face = { backgroundColor: string; boxShadow: string };

export interface LedElement extends HTMLElement {
    _ledfx?: { key: string; animation?: Animation } | null;
}

const clamp01 = (x: number): number => Math.max(0, Math.min(1, x));
const reduced = (): boolean => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;

// Brightness 0-255 -> opacity of a lit LED. The one mapping all views use.
export function alpha(brightness: unknown): number {
    const x = clamp01((Number(brightness) || 0) / 255);
    return x === 0 ? 0 : FLOOR + (1 - FLOOR) * x ** (1 / GAMMA);
}

// Breathe curve, phase 0..1: 0 at the trough, 1 at the peak (Voisen's exp(sin)).
export function breath(phase: number): number {
    return (Math.exp(Math.sin(2 * Math.PI * phase - Math.PI / 2)) - 1 / Math.E) / (Math.E - 1 / Math.E);
}

// HSV hue (degrees) at full saturation and value, as hsv() in effects.go.
export function hsv(hueIn: number): number[] {
    const hue = ((hueIn % 360) + 360) % 360;
    const x = 1 - Math.abs(((hue / 60) % 2) - 1);
    const table = [
        [1, x, 0],
        [x, 1, 0],
        [0, 1, x],
        [0, x, 1],
        [x, 0, 1],
        [1, 0, x]
    ];
    const [r, g, b] = table[Math.floor(hue / 60) % 6] ?? [0, 0, 0];
    return [r, g, b].map(v => Math.round((v ?? 0) * 255));
}

const face = (rgb: Rgb, a: number): Face => ({
    backgroundColor: `rgba(${rgb[0]}, ${rgb[1]}, ${rgb[2]}, ${a.toFixed(3)})`,
    boxShadow: `0 0 ${Math.round(4 + 8 * a)}px rgba(${rgb[0]}, ${rgb[1]}, ${rgb[2]}, ${(0.7 * a).toFixed(3)})`
});

const OFF: Face = { backgroundColor: 'var(--led-off, #2c3138)', boxShadow: 'none' };

export function describe(led: LedState | null | undefined, name: string | undefined, status: LedStatus | null | undefined): LedSpec {
    const rgb = String(led?.color || '0 0 0')
        .trim()
        .split(/\s+/)
        .map(Number);
    const brightness = Number(led?.brightness) || 0;
    const effect = led?.effect;
    const on = Boolean(led) && brightness > 0 && effect !== 'off' && rgb.length === 3 && !rgb.some(Number.isNaN);

    if (!led || !on) return { kind: 'off' };

    const peak = alpha(brightness);
    const lit = Number(led.on_ms) || 0;
    const dark = Number(led.off_ms) || 0;

    if ((effect === 'blink' || effect === 'breath') && lit + dark >= MIN_BLINK_CYCLE && lit > 0) {
        // Activity blink (100/100 ms): the eye reads it as lit with flicker, not as flashing.
        if (effect === 'blink' && lit + dark < BUSY_CYCLE) return { kind: 'busy', rgb, peak, cycle: BUSY_CYCLE };
        return { kind: effect, rgb, peak, lit, cycle: lit + dark };
    }

    const leds = String(status?.fx_leds || '')
        .split(',')
        .filter(Boolean);
    const index = name === undefined ? -1 : leds.indexOf(name);
    if (status?.fx === 'rainbow' && index >= 0 && effect !== 'blink' && effect !== 'breath') {
        const step = RAINBOW_STEP_MS[status.fx_speed ?? 2] || RAINBOW_STEP_MS[2] || 250;
        const offset = ((status.fx_reverse ? -1 : 1) * index * 360) / leds.length;
        return { kind: 'rainbow', rgb, peak, step, offset };
    }

    return { kind: 'static', rgb, peak };
}

// ms since the controller got the last blink/breath command: the backend's own clock (now_ms - sent_ms)
// plus the time since the page received it (stamp(), else 0). null when the backend did not say.
function phaseOf(led: LedState | null | undefined): number | null {
    const sent = Number(led?.sent_ms);
    const now = Number(led?.now_ms);

    return sent > 0 && now >= sent ? now - sent + (led?.rx === undefined ? 0 : performance.now() - led.rx) : null;
}

// Call when a response arrives: marks the moment so the phase can be advanced to the moment of drawing.
// rx: performance.now() when the response reached the page; default the last led response UNCPoll handed out.
export function stamp<T extends Record<string, LedState> | null | undefined>(leds: T, rx: number = window.UNCPoll?.lastPhaseRx ?? performance.now()): T {
    Object.values((leds || {}) as Record<string, LedState>).forEach(led => {
        if (led && typeof led === 'object') led.rx = rx;
    });
    return leds;
}

type Frame = Face & { offset: number; easing?: string };

function frames(spec: Exclude<LedSpec, { kind: 'off' | 'static' }>): Frame[] {
    const full = face(spec.rgb, spec.peak);
    const low = face(spec.rgb, DARK);

    if (spec.kind === 'blink') {
        const on = spec.lit / spec.cycle;
        return [
            { ...full, offset: 0, easing: 'steps(1, end)' },
            { ...(on < 1 ? low : full), offset: on },
            { ...low, offset: 1 }
        ];
    }

    if (spec.kind === 'busy') {
        const dim = face(spec.rgb, DARK + (spec.peak - DARK) * BUSY_LOW);
        return [
            { ...full, offset: 0 },
            { ...dim, offset: 0.5 },
            { ...full, offset: 1 }
        ];
    }

    if (spec.kind === 'breath') {
        const out: Frame[] = [];
        for (let k = 0; k <= BREATH_STOPS; k++) {
            // PWM domain curve through the display gamma, down to the dark level.
            out.push({ ...face(spec.rgb, DARK + (spec.peak - DARK) * breath(k / BREATH_STOPS) ** (1 / GAMMA)), offset: k / BREATH_STOPS });
        }
        return out;
    }

    // rainbow: one full turn in 360/HUE_STEP steps; the colour holds for a step.
    const steps = 360 / HUE_STEP;
    return Array.from({ length: steps + 1 }, (_, k) => ({
        ...face(hsv(k * HUE_STEP + spec.offset), spec.peak),
        offset: k / steps,
        easing: 'steps(1, end)'
    }));
}

function stop(element: LedElement): void {
    element._ledfx?.animation?.cancel();
    element._ledfx = null;
}

export function paint(
    elementIn: HTMLElement | null | undefined,
    led: LedState | null | undefined,
    name: string | undefined,
    status: LedStatus | null | undefined
): void {
    const element = elementIn as LedElement | null | undefined;
    if (!element) return;

    const spec = describe(led, name || element.dataset.led, status);
    const still = reduced();
    const key = JSON.stringify([spec, still, phaseOf(led) === null ? null : led?.sent_ms]);

    if (element._ledfx?.key === key) return;
    stop(element);

    element.classList.toggle('off', spec.kind === 'off');
    element.style.setProperty('--c', spec.kind === 'off' ? '' : `rgb(${spec.rgb.join(',')})`);
    if (spec.kind === 'off') {
        Object.assign(element.style, OFF);
        delete element.dataset.fx;
        element._ledfx = { key };
        return;
    }
    if (spec.kind === 'static' || still) {
        Object.assign(element.style, face(spec.rgb, spec.peak));
    }
    if (spec.kind === 'static') {
        delete element.dataset.fx;
        element._ledfx = { key };
        return;
    }

    element.dataset.fx = spec.kind === 'busy' ? 'blink' : spec.kind;
    if (still || !element.animate) {
        element._ledfx = { key };
        return;
    }

    const duration = spec.kind === 'rainbow' ? (spec.step * 360) / HUE_STEP : spec.cycle;
    const animation = element.animate(frames(spec), { duration, iterations: Number.POSITIVE_INFINITY, easing: 'linear' });
    // Phase from the backend when it knows it, else one shared wall-clock phase.
    animation.currentTime = (spec.kind === 'busy' ? Date.now() : (phaseOf(led) ?? Date.now())) % duration;
    element._ledfx = { key, animation };
}
