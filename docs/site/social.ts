// The 1200x630 social preview as an SVG, rasterised by build.ts with rsvg-convert (no browser). Fonts: rsvg-convert resolves them through fontconfig, build.ts points it at Archivo.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { SOCIAL_HEAD, SOCIAL_SUB } from './content';

const root = resolve(import.meta.dir, '../..');

/** Greedy word wrap to at most `max` characters per line. */
function wrap(text: string, max: number): string[] {
    const lines: string[] = [];
    for (const w of text.split(' ')) {
        const last = lines.at(-1);
        if (last !== undefined && last.length + 1 + w.length <= max) lines[lines.length - 1] = `${last} ${w}`;
        else lines.push(w);
    }
    return lines;
}

const esc = (s: string): string => s.replace(/&/g, '&amp;').replace(/</g, '&lt;');

export function socialSvg(): string {
    const icon = `data:image/svg+xml;base64,${readFileSync(resolve(root, 'web/assets/icon.svg')).toString('base64')}`;
    // One line per sentence; three lines get a smaller size so the card keeps its margins.
    const head = SOCIAL_HEAD.split(/(?<=\.) /);
    const [size, step, top] = head.length > 2 ? [62, 74, 230] : [76, 92, 266];
    const sub = wrap(SOCIAL_SUB, 40);
    const font = `font-family="Archivo, Helvetica, Arial, sans-serif"`;
    return `<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="630" viewBox="0 0 1200 630">
<defs>
<pattern id="dots" width="22" height="22" patternUnits="userSpaceOnUse"><circle cx="1" cy="1" r="1.2" fill="#ffffff" fill-opacity="0.08"/></pattern>
<radialGradient id="glow" cx="50%" cy="50%" r="50%"><stop offset="0" stop-color="#7fe3ff" stop-opacity="0.22"/><stop offset="1" stop-color="#7fe3ff" stop-opacity="0"/></radialGradient>
</defs>
<rect width="1200" height="630" fill="#23272b"/>
<rect width="1200" height="630" fill="url(#dots)"/>
<circle cx="975" cy="315" r="330" fill="url(#glow)"/>
<image href="${icon}" x="835" y="175" width="280" height="280"/>
<text x="64" y="104" ${font} font-size="30" font-weight="700" fill="#b4bac0">UGREEN NAS Control</text>
<text ${font} font-size="${size}" font-weight="800" fill="#eef0f1" letter-spacing="-2">${head.map((l, i) => `<tspan x="64" y="${top + i * step}">${esc(l)}</tspan>`).join('')}</text>
<text ${font} font-size="32" font-weight="400" fill="#b4bac0">${sub.map((l, i) => `<tspan x="64" y="${top + (head.length - 1) * step + 66}" dy="${i * 44}">${esc(l)}</tspan>`).join('')}</text>
<g>
<rect x="64" y="550" width="44" height="8" rx="4" fill="#ff3b30"/><rect x="118" y="550" width="44" height="8" rx="4" fill="#ffd60a"/>
<rect x="172" y="550" width="44" height="8" rx="4" fill="#34c759"/><rect x="226" y="550" width="44" height="8" rx="4" fill="#0a84ff"/>
</g>
</svg>
`;
}

/** Kept for web/tools/shots.ts, which still screenshots the card in a browser; the site build uses socialSvg. */
export const socialHtml = (): string => `<!doctype html><html><body style="margin:0;width:1200px;height:630px">${socialSvg()}</body></html>`;
