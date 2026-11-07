// Facts that README, site and the Community Applications entry state, derived once from their source. Prose imports
// these (or sits between generated markers filled from them) instead of typing a number, a version or a model list.
// docs/tools/facts-lint.ts (part of `just docs-check`) fails when prose types such a fact by hand.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { loadModels } from '../../web/models/index';
import { type Capability, type Evidence, type Model, type Overall, overallStatus } from '../../web/models/schema';
import { LANGS } from '../../web/src/core/i18n';

const root = resolve(import.meta.dir, '../..');

export const PROJECT_URL = 'https://github.com/ugreen-nas-control/ugreen-nas-control';
/** The published site: `/` is main, `/next/` is next. */
export const SITE = 'https://ugreen-nas-control.github.io/ugreen-nas-control';

/** The claim made in README, site, social card and the CA entry; the plugin runs in userspace. */
export const NO_KERNEL_MODULE = 'No kernel module';

export const models: Model[] = await loadModels();

/** `a`, `a and b`, `a, b and c`. */
export const andList = (items: string[]): string => (items.length > 1 ? `${items.slice(0, -1).join(', ')} and ${items.at(-1)}` : (items[0] ?? ''));

/** Languages of the interface: `LANGS` in web/src/core/i18n.ts. */
export const LANG_COUNT = LANGS.length;
const english = new Intl.DisplayNames('en', { type: 'language' });
export const LANG_NAMES = LANGS.map(l => english.of(l) ?? l);
export const LANGUAGES = `${LANG_COUNT} languages`;

/** Minimum Unraid version: `min` of the .plg template (plg-check keeps the CA MinVer equal). */
export const UNRAID_MIN_FULL = readFileSync(resolve(root, 'plugin/ugreen-nas-control.plg.tmpl'), 'utf8').match(/^\s*min="([^"]+)"/m)?.[1] ?? '';
if (!UNRAID_MIN_FULL) throw new Error('plugin/ugreen-nas-control.plg.tmpl: no min="…"');
/** `7.2` of `7.2.0`. */
export const UNRAID_MIN = UNRAID_MIN_FULL.replace(/\.0$/, '');
export const UNRAID_PLUS = `${UNRAID_MIN}+`;
export const UNRAID_OR_NEWER = `Unraid ${UNRAID_MIN} or newer`;

export const count = (overall: Overall): number => models.filter(m => overallStatus(m) === overall).length;
/** Names of the models whose `cap` has this evidence, in registry order. */
export const withEvidence = (cap: Capability, status: Evidence, list: Model[] = models): string[] =>
    list.filter(m => m.verified[cap].status === status).map(m => m.name);
export const withOverall = (overall: Overall): string[] => models.filter(m => overallStatus(m) === overall).map(m => m.name);

/** Keywords for the site's meta tag: the fixed terms plus every model someone has verified or tested. */
export const KEYWORD_TERMS = ['UGREEN NAS', 'Unraid plugin', 'fan control', 'LED control', 'watchdog', 'case view', 'Community Applications'];
export const modelKeywords = (): string[] => models.filter(m => ['verified', 'partial', 'community'].includes(overallStatus(m))).map(m => m.name);

/** The CA `<Overview>` (plugins/ugreen-nas-control.xml, between the overview markers). The fan sentence follows the registry. */
export function caOverview(): string {
    const confirmed = withEvidence('fans', 'verified');
    const reported = withEvidence('fans', 'community');
    const fans = [
        confirmed.length ? `verified on the ${andList(confirmed)}` : '',
        reported.length ? `reported working by users on the ${andList(reported)}` : ''
    ].filter(Boolean);
    return [
        'Front LEDs, fan control and a live view of your UGREEN NAS (DX, DXP and iDX series) in Unraid.',
        'LEDs: colours, effects, drive and network status, night mode, Unraid alerts.',
        `Fans: Quiet, Standard, Performance or your own curves, with safety limits and a history graph; ${fans.join(', ')}. Other models with a fan chip start with a guided fan detection and stay off until you confirm.`,
        'Case view with bays, ports and drives; temperatures read from Unraid, so sleeping disks stay asleep.',
        'MQTT for Home Assistant and displays.',
        `${UNRAID_OR_NEWER}, interface in ${LANGUAGES}. ${NO_KERNEL_MODULE}. Replaces UGREEN LED Control.`
    ].join(' ');
}
