import { escapeHtml } from './html';

// Fan icons turn by script, not by CSS animation: a CSS duration change or a re-render
// would restart the angle. The angle lives per fan id; only the speed follows the rpm.
const fanSpin = new Map<string, { angle: number; rpm: number }>();
const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
let fanLast = 0;
let fanFrame = 0;

function fanTick(now: number): void {
    const dt = Math.min(0.1, (now - fanLast) / 1000);
    let moving = false;

    fanLast = now;
    fanSpin.forEach(fan => {
        if (fan.rpm > 0) {
            fan.angle = (fan.angle + (dt * 360) / Math.max(0.3, 2400 / fan.rpm)) % 360;
            moving = true;
        }
    });
    document.querySelectorAll<HTMLElement>('.unc-fan-icon[data-fan]').forEach(el => {
        el.style.transform = `rotate(${fanSpin.get(el.dataset.fan ?? '')?.angle || 0}deg)`;
    });
    fanFrame = moving ? requestAnimationFrame(fanTick) : 0;
}

export function fanIcon(fan: { id: string; rpm: number }, svg: string): string {
    const state = fanSpin.get(fan.id) || { angle: 0, rpm: 0 };

    state.rpm = reduceMotion.matches ? 0 : fan.rpm;
    fanSpin.set(fan.id, state);

    if (state.rpm > 0 && !fanFrame) {
        fanLast = performance.now();
        fanFrame = requestAnimationFrame(fanTick);
    }
    return `<span class="unc-fan-icon" data-fan="${escapeHtml(fan.id)}" style="transform:rotate(${state.angle}deg)">${svg}</span>`;
}
