// The plugin's own front-of-NAS drawing and its colours, for the Pages site: the markup comes from the same module the
// plugin page uses (web/src/features/nas/components/front.ts) fed with the sample data of the models registry; the CSS
// is cut out of web/plugin/css/app.css, so site and plugin look the same and cannot drift apart.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { Hardware } from '../../web/src/core/contract';
import { frontHtml } from '../../web/src/features/nas/components/front';
import { en } from '../../web/src/features/nas/i18n/en';
import { hardware } from '../../web/models/fixture';
import type { Model } from '../../web/models/schema';

const root = resolve(import.meta.dir, '../..');

/** The drawing of one model's front with its sample drives; '' for a model without a case view. */
export function liveFront(m: Model): string {
    if (m.caseView === 'none') return '';
    // The component reads its texts from window.UNC_I18N like in the plugin page; only exists during this call.
    const g = globalThis as unknown as { window?: unknown };
    const had = g.window;
    g.window = { UNC_I18N: { en }, UGREEN_NAS_LANG: 'en' };
    try {
        return frontHtml(hardware(m) as unknown as Hardware);
    } finally {
        if (had === undefined) delete g.window;
        else g.window = had;
    }
}

/** Top-level rules of a stylesheet as [selector, whole text]; @-rules keep their inner text in the second item. */
function rules(css: string): [string, string][] {
    const text = css.replace(/\/\*[\s\S]*?\*\//g, '');
    const out: [string, string][] = [];
    let depth = 0;
    let start = 0;
    let open = 0;
    for (let i = 0; i < text.length; i++) {
        if (text[i] === '{') {
            if (depth++ === 0) open = i;
        } else if (text[i] === '}' && --depth === 0) {
            out.push([text.slice(start, open).trim(), text.slice(start, i + 1).trim()]);
            start = i + 1;
        }
    }
    return out;
}

const PICTURE =
    /^(\.unc\[data-theme="light"\] |\.unc-hero )?\.(unc-nas(-bays|-front|-model)?|unc-nbay|unc-nscreen|unc-socket|unc-pled|unc-led|unc-pbtn|unc-front-ports)\b/;

/**
 * The plugin's colour tokens (--unc-*, dark and light) retargeted to :root, and the rules that draw the front.
 * `.unc[data-theme="light"]` becomes `:root[data-theme="light"]`, the site's own theme switch.
 */
export function pluginCss(): string {
    const all = rules(readFileSync(resolve(root, 'web/plugin/css/app.css'), 'utf8'));
    const tokens = (selector: string, target: string): string => {
        const rule = all.find(([s]) => s === selector);
        if (!rule) throw new Error(`app.css: no rule for ${selector}`);
        const vars = rule[1]
            .slice(rule[1].indexOf('{') + 1, -1)
            .split(';')
            .map(d => d.trim())
            .filter(d => /^--unc-[\w-]+\s*:/.test(d));
        return `${target}{${vars.join(';')}}`;
    };
    const pictures = all
        .filter(([s]) => s.split(',').every(p => PICTURE.test(p.trim())))
        .map(([, body]) => body.replaceAll('.unc[data-theme="light"]', ':root[data-theme="light"]'));
    if (pictures.length < 40) throw new Error(`app.css: only ${pictures.length} drawing rules found; the selectors in docs/site/live.ts need an update`);
    return [tokens('.unc', ':root'), tokens('.unc[data-theme="light"]', ':root[data-theme="light"]'), ...pictures].join('\n');
}
