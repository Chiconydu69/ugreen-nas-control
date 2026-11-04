// The model registry: one file per UGREEN model row (the rows of MODELS in web/src/features/nas/app.ts), validated by
// web/tools/check-models.ts. Adding a model = adding one file here; the harness, the screenshots, the docs and the site follow.
//
// Two kinds of facts, kept apart:
//   - what the model is (cpu, bays, m2Slots, lan), from the store page or a named review (`sources`);
//   - what is known to work with this plugin, per capability (`verified`), each with its evidence.

export type Capability = 'leds' | 'fans' | 'case' | 'sensors';
export const CAPABILITIES: Capability[] = ['leds', 'fans', 'case', 'sensors'];

/**
 * How well one capability is known to work on the model.
 *   verified    a maintainer ran it on their own device with the plugin (by, version, date)
 *   community   a user (or miskcoo's driver for the LED protocol) confirmed it on the model (by or ref)
 *   partial     checked, but not everything works or not everything is confirmed (note says what)
 *   expected    the code covers the model, nobody has confirmed it
 *   unsupported the plugin does not offer it on this model
 */
export type Evidence = 'verified' | 'community' | 'partial' | 'expected' | 'unsupported';
export const EVIDENCE: Evidence[] = ['verified', 'community', 'partial', 'expected', 'unsupported'];

export interface Check {
    status: Evidence;
    /** Who checked: "maintainer (tower)", "miskcoo", "users". */
    by?: string;
    /** Plugin release the check was done on (a heading of CHANGELOG.md); older than three releases = stale, re-test. */
    version?: string;
    /** YYYY-MM-DD. */
    date?: string;
    /** Issue, PR or forum post that holds the evidence. */
    ref?: string;
    /** What exactly was seen, or what is still open. */
    note?: string;
}

export type LedSupport = 'bays' | 'power' | 'none';
export type CaseView = 'none' | 'front-rear' | 'full';
export type Series = 'DH' | 'DX' | 'DXP' | 'iDX';
export const SERIES: Series[] = ['DH', 'DX', 'DXP', 'iDX'];

/** fixtureDmi of the models the backend feeds a watchdog on: backend/internal/fan/caps.go WatchdogModel (EC: DXP4800 GT, iDX6011 Pro; IT8613E: isIT8613Model). Keep identical. */
export const WATCHDOG_MODELS = ['DXP4800 GT', 'iDX6011 Pro', 'DXP2800', 'DXP4800', 'DXP4800 Plus', 'DXP4800 Pro', 'DXP6800 Pro', 'DXP8800 Plus'];

export type Overall = 'verified' | 'partial' | 'community' | 'expected';

export interface FanSupport {
    chip: string;
    /** fan id -> role, as wired (backend/internal/fan/daemon.go roles). */
    fans: Record<string, 'cpu' | 'case'>;
    /** How the fans are reached (backend/internal/fan/fanmodels.go FanKind): fixed wiring (default) or a probe whose map the user confirms; fan control stays off until then. */
    kind?: 'fixed' | 'probe';
    /** Lowest speed the hardware runs at (backend/internal/fan/fanmodels.go minPctFloor): the CS201x of the DXP2800 GT. Absent everywhere else. */
    minPctFloor?: number;
    /**
     * The EC watchdog the BIOS may arm and the plugin feeds (backend/internal/fan/caps.go WatchdogModel): only the DXP4800 GT and
     * the iDX6011 Pro. Evidence for the feeding itself, apart from `verified.fans`. Absent everywhere else.
     */
    watchdog?: Check;
}

export interface Model {
    id: string;
    name: string;
    series: Series;
    /** Regex source (as in MODELS of web/src/features/nas/app.ts) matching the DMI product_name of every variant. */
    dmiPattern: string;
    /** DMI product_name the harness and the screenshots use (must match dmiPattern). */
    fixtureDmi: string;
    cpu: string | null;
    bays: number | null;
    m2Slots: number | null;
    /** M.2 slots the plugin maps (backend/internal/fan/hardware.go); null when it does not map them. */
    m2: { slots: string[]; pcieSlot: boolean } | null;
    /** LAN ports, max link speed in Mbit/s each; null when not known. */
    lan: { maxMbit: (number | null)[] } | null;
    led: LedSupport;
    caseView: CaseView;
    /** Which drawing the case view uses (CASE_MODELS in web/src/features/nas/components/front.ts); 'none' without case view. */
    caseVariant: 'none' | 'rear6800' | 'rear4800' | 'rear2800' | 'rear6011' | 'rear8800';
    fan: FanSupport | null;
    verified: Record<Capability, Check>;
    sources: string[];
    note: string;
    shots: boolean;
    fixture?: { cpu?: string; chip?: string; ledBays?: number };
}

const RANK: Record<Evidence, number> = { verified: 3, community: 2, partial: 2, expected: 1, unsupported: 0 };

export const offered = (m: Model): Capability[] => CAPABILITIES.filter(c => m.verified[c].status !== 'unsupported');

/**
 * Model-level state, derived (never stored):
 *   verified   every offered capability is verified
 *   partial    at least one offered capability is verified, the rest is not
 *   community  none verified, at least one confirmed by users (or partial)
 *   expected   nothing confirmed yet
 */
export function overallStatus(m: Model): Overall {
    const ranks = offered(m).map(c => RANK[m.verified[c].status]);
    if (ranks.length > 0 && ranks.every(r => r === 3)) return 'verified';
    if (ranks.includes(3)) return 'partial';
    if (ranks.includes(2)) return 'community';
    return 'expected';
}

export function monthYear(date: string | undefined): string {
    if (!date) return '';
    const [y, mo] = date.split('-');
    const names = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
    return `${names[Number(mo) - 1] ?? mo} ${y}`;
}

export function whenText(c: Check): string {
    const parts = [c.version ? `on ${c.version}` : '', monthYear(c.date)].filter(Boolean);
    return parts.length ? parts.join(', ') : '';
}

export const CAPABILITY_LABEL: Record<Capability, string> = { leds: 'LEDs', fans: 'Fan control', case: 'Case view', sensors: 'Sensors' };

export const EVIDENCE_LABEL: Record<Evidence, string> = {
    verified: 'Verified',
    community: 'Community-tested',
    partial: 'Partly confirmed',
    expected: 'Expected',
    unsupported: 'Not offered'
};

export const EVIDENCE_HELP: Record<Evidence, string> = {
    verified: 'A maintainer ran it on their own device with the plugin; the plugin version and the date are listed.',
    community: "Users, or miskcoo's driver for the LED protocol, confirmed it on the model.",
    partial: 'Checked on a device, but not everything works or is confirmed; the note says what is open.',
    expected: 'The code covers the model; nobody has confirmed it.',
    unsupported: 'The plugin does not offer it on this model.'
};

export const OVERALL_LABEL: Record<Overall, string> = { verified: 'Verified', partial: 'Partly verified', community: 'Community-tested', expected: 'Expected' };

export const OVERALL_HELP: Record<Overall, string> = {
    verified: 'Every capability the plugin offers is verified.',
    partial: 'At least one capability is verified, the others are not yet.',
    community: 'Nothing verified by a maintainer yet; users confirmed at least one capability.',
    expected: 'Nothing confirmed yet.'
};

export function checkText(c: Check): string {
    const when = whenText(c);
    if (c.status === 'unsupported') return EVIDENCE_LABEL.unsupported;
    if (c.status === 'expected') return EVIDENCE_LABEL.expected;
    return when ? `${EVIDENCE_LABEL[c.status]} ${when}` : c.by ? `${EVIDENCE_LABEL[c.status]} by ${c.by}` : EVIDENCE_LABEL[c.status];
}

const DATE = /^\d{4}-\d{2}-\d{2}$/;

export function validate(m: Model, fileId: string): string[] {
    const bad: string[] = [];
    const str = (k: keyof Model) => (typeof m[k] === 'string' && (m[k] as string).trim() !== '') || (bad.push(`${k}: non-empty string`), false);

    for (const k of ['id', 'name', 'dmiPattern', 'fixtureDmi', 'note'] as const) str(k);
    if (m.id !== fileId) bad.push(`id "${m.id}" must equal the file name "${fileId}"`);
    if (!/^[a-z0-9]+(-[a-z0-9]+)*$/.test(m.id ?? '')) bad.push('id: lowercase letters, digits, dashes');
    if (!SERIES.includes(m.series)) bad.push('series');
    try {
        if (!new RegExp(m.dmiPattern).test(m.fixtureDmi)) bad.push(`fixtureDmi "${m.fixtureDmi}" does not match dmiPattern`);
    } catch {
        bad.push('dmiPattern: not a valid regex');
    }
    if (m.cpu !== null && typeof m.cpu !== 'string') bad.push('cpu: string or null');
    if (m.bays !== null && !(Number.isInteger(m.bays) && m.bays >= 0 && m.bays <= 8)) bad.push('bays: 0..8 or null');
    if (m.m2Slots !== null && !(Number.isInteger(m.m2Slots) && m.m2Slots >= 0 && m.m2Slots <= 8)) bad.push('m2Slots: 0..8 or null');
    if (m.m2 !== null && !(Array.isArray(m.m2?.slots) && typeof m.m2.pcieSlot === 'boolean')) bad.push('m2: {slots, pcieSlot} or null');
    if (m.lan !== null && !(Array.isArray(m.lan?.maxMbit) && m.lan.maxMbit.length >= 1)) bad.push('lan: {maxMbit: [..]} or null');
    if (!['bays', 'power', 'none'].includes(m.led)) bad.push('led');
    if (!['none', 'front-rear', 'full'].includes(m.caseView)) bad.push('caseView');
    if (!['none', 'rear6800', 'rear4800', 'rear2800', 'rear6011', 'rear8800'].includes(m.caseVariant)) bad.push('caseVariant');
    if ((m.caseView === 'none') !== (m.caseVariant === 'none')) bad.push('caseVariant must be none exactly when caseView is none');
    if (m.caseView !== 'none' && !m.bays) bad.push('a case view needs bays');
    if (m.caseView === 'full' && m.m2 === null) bad.push('caseView full needs m2');
    if (m.fan !== null && !(m.fan?.chip && Object.keys(m.fan.fans ?? {}).length > 0)) bad.push('fan: {chip, fans} or null');
    if (m.fan?.watchdog) {
        // The backend feeds it on exactly these (WatchdogModel: backend/internal/fan/caps.go); on the iDX6012 and the plain iDX6011 0x98/0x99 is a fan tachometer.
        if (!WATCHDOG_MODELS.includes(m.fixtureDmi)) bad.push(`fan.watchdog: only ${WATCHDOG_MODELS.join(', ')} have a watchdog petter`);
        if (!EVIDENCE.includes(m.fan.watchdog.status) || m.fan.watchdog.status === 'unsupported')
            bad.push('fan.watchdog.status: not unsupported (omit the field instead)');
        if (m.fan.watchdog.date !== undefined && !DATE.test(m.fan.watchdog.date)) bad.push('fan.watchdog.date: YYYY-MM-DD');
        if (m.fan.watchdog.status === 'expected' && (m.fan.watchdog.date || m.fan.watchdog.version))
            bad.push('fan.watchdog: expected has no date; nobody checked it');
    }
    if (m.fan !== null && m.caseView === 'none') bad.push('fan control needs a case view (the fan pages hang off it)');
    if (typeof m.shots !== 'boolean') bad.push('shots: boolean');
    if (m.shots && m.led === 'none') bad.push('shots: a model without LEDs is not offered by the plugin');
    if (!Array.isArray(m.sources) || m.sources.length === 0 || m.sources.some(s => !/^https:\/\//.test(s))) bad.push('sources: https URLs, at least one');

    const offeredBy: Record<Capability, boolean> = { leds: m.led !== 'none', fans: m.fan !== null, case: m.caseView !== 'none', sensors: true };
    for (const cap of CAPABILITIES) {
        const c = m.verified?.[cap];
        const at = `verified.${cap}`;
        if (!c || !EVIDENCE.includes(c.status)) {
            bad.push(`${at}: status must be one of ${EVIDENCE.join(', ')}`);
            continue;
        }
        if ((c.status === 'unsupported') === offeredBy[cap]) {
            bad.push(
                `${at}: ${offeredBy[cap] ? 'the plugin offers it, so it cannot be unsupported' : 'the plugin does not offer it, so it must be unsupported'}`
            );
        }
        if (c.date !== undefined && !DATE.test(c.date)) bad.push(`${at}.date: YYYY-MM-DD`);
        if (c.version !== undefined && !/^\d+\.\d+(\.\d+)?$/.test(c.version)) bad.push(`${at}.version: a release like 3.6`);
        if (c.ref !== undefined && !/^https:\/\//.test(c.ref)) bad.push(`${at}.ref: an https URL`);
        if ((c.version === undefined) !== (c.date === undefined)) bad.push(`${at}: version and date go together`);
        if (c.status === 'verified' && !(c.by && ((c.version && c.date) || c.ref))) bad.push(`${at}: verified needs by and (version and date, or ref)`);
        if (c.status === 'community' && !(c.by || c.ref)) bad.push(`${at}: community needs by or ref`);
        if (c.status === 'partial' && !(c.by && c.note)) bad.push(`${at}: partial needs by and a note saying what is open`);
        if (c.status === 'expected' && (c.date || c.version)) bad.push(`${at}: expected has no date; nobody checked it`);
    }
    return bad;
}
