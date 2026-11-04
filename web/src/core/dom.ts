import { rangeFill } from './format';

/** Sets --fill on a range input: the share of the track left of the thumb, for the CSS gradient. */
export function setRangeFill(input: HTMLInputElement): void {
    const min = Number(input.min) || 0;
    const max = Number(input.max) || 100;
    input.style.setProperty('--fill', rangeFill(Number(input.value), min, max));
}

export const hit = (event: Event): Element | null => (event.target instanceof Element ? event.target : null);
