// Plugin version scheme. Unraid compares plugin versions with strcmp (webgui
// dynamix.plugin.manager/scripts/plugin: install, update, updateall), so every version must sort right as text.
//   stable  X.Y.Z   one digit each ("5.0.10" would sort below "5.0.9"); always three parts
//   next    X.Y.0-next.NNNNNN   X.Y.0 = the minor after the newest heading, N = CI run, zero-padded
//   local   X.Y.Z.tYYYYMMDDHHMM   Z always written, so it sorts above its release and below the next patch
// Policy: docs/releasing.md, section Versions.

// Headings up to 4.x are two-part (4.2 = 4.2.0); they stay valid as the base of a step, a new release is always X.Y.Z.
export const STABLE_RE = /^[1-9]\.\d\.\d$/;
export const BASE_RE = /^[1-9]\.\d(\.\d)?$/;

export type Bump = 'patch' | 'minor' | 'major';
export type FragmentType = 'feat' | 'fix' | 'model' | 'docs' | 'chore';

/** The release size a fragment type asks for; null: no size of its own. */
export function bumpOf(type: FragmentType): Bump | null {
    if (type === 'fix') return 'patch';
    if (type === 'feat' || type === 'model') return 'minor';
    return null;
}

const parts = (v: string) => {
    const [x, y, z] = v.split('.').map(Number);
    return { x, y, z: z ?? 0 };
};

/** The stable release after `base` for a patch, minor or major step; null if the scheme has no such step (digit cap). */
export function successor(base: string, bump: Bump): string | null {
    const { x, y, z } = parts(base);
    if (bump === 'patch') return z >= 9 ? null : `${x}.${y}.${z + 1}`;
    if (bump === 'minor') return y >= 9 ? null : `${x}.${y + 1}.0`;
    return x >= 9 ? null : `${x + 1}.0.0`;
}

/** Which step takes `base` to `version`, or null when it is none of the allowed steps. */
export function bumpBetween(base: string, version: string): Bump | null {
    for (const b of ['patch', 'minor', 'major'] as const) if (successor(base, b) === version) return b;
    return null;
}

/** The minor release the next channel builds towards (after X.9.Z comes (X+1).0.0). */
export function nextAfter(base: string): string {
    const { x, y } = parts(base);
    return y >= 9 ? `${x + 1}.0.0` : `${x}.${y + 1}.0`;
}

export const nextVersion = (base: string, run: number | string) => `${nextAfter(base)}-next.${String(run).padStart(6, '0')}`;
export const localVersion = (base: string, stamp: string) => {
    const { x, y, z } = parts(base);
    return `${x}.${y}.${z}.t${stamp}`;
};

/** Why `version` may not follow `newest` in CHANGELOG.md, or null. */
export function releaseProblem(newest: string | undefined, version: string, fragments: FragmentType[]): string | null {
    if (!STABLE_RE.test(version)) return `${version}: stable versions are X.Y.Z with one digit each, all three parts written (Unraid compares plugin versions as text)`;
    if (!newest) return null;
    if (!(version > newest)) return `${version} does not sort above ${newest} as text; Unraid would not offer it as an update`;
    const bump = bumpBetween(newest, version);
    if (!bump) {
        const ok = (['patch', 'minor', 'major'] as const).map(b => successor(newest, b)).filter(Boolean);
        return `${version} does not follow ${newest}; allowed: ${ok.join(', ')}`;
    }
    if (bump === 'patch' && fragments.some(t => bumpOf(t) === 'minor'))
        return `${version} is a patch release but the fragments hold a feat or model entry; features ship as a minor (${successor(newest, 'minor') ?? successor(newest, 'major')})`;
    return null;
}

/** Sort-order problems of the three version kinds around `base` (the newest stable); empty when sane. */
export function orderProblems(base: string): string[] {
    const out: string[] = [];
    const chain = [
        base,
        localVersion(base, '202601010000'),
        successor(base, 'patch'),
        nextVersion(base, 9),
        nextVersion(base, 10),
        nextVersion(base, 100),
        nextAfter(nextAfter(base))
    ].filter((v): v is string => !!v);
    for (let i = 1; i < chain.length; i++) if (!(chain[i] > chain[i - 1])) out.push(`${chain[i]} must sort above ${chain[i - 1]} as text`);
    const patch = successor(base, 'patch');
    if (patch && !(localVersion(base, '999912312359') < patch)) out.push(`local build of ${base} must sort below ${patch}`);
    return out;
}
